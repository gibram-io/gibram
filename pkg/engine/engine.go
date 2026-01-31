// Package engine provides the session-based query engine for GibRAM
package engine

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gibram-io/gibram/pkg/graph"
	"github.com/gibram-io/gibram/pkg/store"
	"github.com/gibram-io/gibram/pkg/types"
	"github.com/gibram-io/gibram/pkg/version"
)

// =============================================================================
// Errors
// =============================================================================

var (
	ErrSessionRequired = errors.New("session_id is required")
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

// =============================================================================
// LRU Cache for Query Logs
// =============================================================================

const (
	MaxQueryLogEntries = 10000
	MaxSessions        = 10000 // Maximum concurrent sessions (DoS protection)
)

type queryLogLRU struct {
	mu       sync.RWMutex
	capacity int
	items    map[uint64]*list.Element
	order    *list.List // front = most recent, back = least recent
}

type queryLogEntry struct {
	id  uint64
	log *queryLog
}

func newQueryLogLRU(capacity int) *queryLogLRU {
	return &queryLogLRU{
		capacity: capacity,
		items:    make(map[uint64]*list.Element),
		order:    list.New(),
	}
}

func (c *queryLogLRU) Set(id uint64, log *queryLog) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create a deep copy to avoid concurrent modification
	logCopy := &queryLog{
		sessionID: log.sessionID,
		spec:      log.spec,
		seeds:     make([]types.SeedInfo, len(log.seeds)),
		traversal: make([]types.TraversalStep, len(log.traversal)),
	}
	copy(logCopy.seeds, log.seeds)
	copy(logCopy.traversal, log.traversal)

	// If already exists, move to front
	if elem, ok := c.items[id]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*queryLogEntry).log = logCopy
		return
	}

	// Evict if at capacity
	for c.order.Len() >= c.capacity {
		back := c.order.Back()
		if back != nil {
			entry := back.Value.(*queryLogEntry)
			delete(c.items, entry.id)
			c.order.Remove(back)
		}
	}

	// Add new entry
	entry := &queryLogEntry{id: id, log: logCopy}
	elem := c.order.PushFront(entry)
	c.items[id] = elem
}

func (c *queryLogLRU) Get(id uint64) (*queryLog, bool) {
	c.mu.RLock()
	elem, ok := c.items[id]
	if !ok {
		c.mu.RUnlock()
		return nil, false
	}

	// Create a deep copy before returning to avoid external modification
	log := elem.Value.(*queryLogEntry).log
	logCopy := &queryLog{
		sessionID: log.sessionID,
		spec:      log.spec,
		seeds:     make([]types.SeedInfo, len(log.seeds)),
		traversal: make([]types.TraversalStep, len(log.traversal)),
	}
	copy(logCopy.seeds, log.seeds)
	copy(logCopy.traversal, log.traversal)
	c.mu.RUnlock()

	return logCopy, true
}

func (c *queryLogLRU) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// =============================================================================
// Engine - Session-Based GibRAM Engine
// =============================================================================

type Engine struct {
	mu sync.RWMutex

	// Session stores (partitioned by session_id)
	sessions map[string]*store.SessionStore

	// Global query ID generator
	queryIDGen uint64

	// Query logs for explain (LRU cache)
	queryLogs *queryLogLRU

	// Config
	vectorDim int

	// Session cleanup
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupWg       sync.WaitGroup
}

type queryLog struct {
	sessionID string
	spec      types.QuerySpec
	seeds     []types.SeedInfo
	traversal []types.TraversalStep
}

// NewEngine creates a new session-based GibRAM engine
func NewEngine(vectorDim int) *Engine {
	e := &Engine{
		sessions:        make(map[string]*store.SessionStore),
		queryLogs:       newQueryLogLRU(MaxQueryLogEntries),
		vectorDim:       vectorDim,
		cleanupInterval: 60 * time.Second,
		stopCleanup:     make(chan struct{}),
	}

	return e
}

// =============================================================================
// Session Management
// =============================================================================

// getOrCreateSession gets or creates a session store
func (e *Engine) getOrCreateSession(sessionID string) (*store.SessionStore, error) {
	if sessionID == "" {
		return nil, ErrSessionRequired
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if session exists
	if sess, ok := e.sessions[sessionID]; ok {
		if sess.IsExpired() {
			delete(e.sessions, sessionID)
			return nil, ErrSessionExpired
		}
		sess.Touch()
		return sess, nil
	}

	// Enforce max session limit (DoS protection)
	if len(e.sessions) >= MaxSessions {
		return nil, fmt.Errorf("max sessions limit reached (%d)", MaxSessions)
	}

	// Create new session (auto-create on first write)
	sess := store.NewSessionStore(sessionID, e.vectorDim)
	e.sessions[sessionID] = sess
	return sess, nil
}

// getSession gets an existing session (does not auto-create)
func (e *Engine) getSession(sessionID string) (*store.SessionStore, error) {
	if sessionID == "" {
		return nil, ErrSessionRequired
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	sess, ok := e.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if sess.IsExpired() {
		return nil, ErrSessionExpired
	}

	sess.Touch()
	return sess, nil
}

// ListSessions returns all active sessions
func (e *Engine) ListSessions() []types.SessionInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]types.SessionInfo, 0, len(e.sessions))
	for _, sess := range e.sessions {
		if !sess.IsExpired() {
			result = append(result, sess.GetInfo())
		}
	}
	return result
}

// DeleteSession deletes a session and all its data
func (e *Engine) DeleteSession(sessionID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.sessions[sessionID]; !ok {
		return false
	}

	delete(e.sessions, sessionID)
	return true
}

// GetSessionInfo returns info for a specific session
func (e *Engine) GetSessionInfo(sessionID string) (types.SessionInfo, error) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return types.SessionInfo{}, err
	}
	return sess.GetInfo(), nil
}

// SetSessionTTL sets TTL for a session
func (e *Engine) SetSessionTTL(sessionID string, ttl, idleTTL int64) error {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return err
	}
	if ttl > 0 {
		sess.SetTTL(ttl)
	}
	if idleTTL > 0 {
		sess.SetIdleTTL(idleTTL)
	}
	return nil
}

// TouchSession updates session last access time
func (e *Engine) TouchSession(sessionID string) error {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return err
	}
	sess.Touch()
	return nil
}

// SessionCount returns the number of active sessions
func (e *Engine) SessionCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.sessions)
}

// =============================================================================
// Session Cleanup (background task)
// =============================================================================

// StartSessionCleanup starts the background session cleanup task
func (e *Engine) StartSessionCleanup(interval time.Duration) {
	if interval > 0 {
		e.cleanupInterval = interval
	}

	e.cleanupWg.Add(1)
	go func() {
		defer e.cleanupWg.Done()
		ticker := time.NewTicker(e.cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				e.cleanupExpiredSessions()
			case <-e.stopCleanup:
				return
			}
		}
	}()
}

// StopSessionCleanup stops the background cleanup task
func (e *Engine) StopSessionCleanup() {
	close(e.stopCleanup)
	e.cleanupWg.Wait()
}

func (e *Engine) cleanupExpiredSessions() {
	// Use RLock first to collect expired sessions (avoids long write lock)
	e.mu.RLock()
	var expired []string
	for id, sess := range e.sessions {
		if sess.IsExpired() {
			expired = append(expired, id)
		}
	}
	e.mu.RUnlock()

	// Only acquire write lock if there are sessions to delete
	if len(expired) > 0 {
		e.mu.Lock()
		for _, id := range expired {
			// Re-check expiry in case session was touched between locks
			if sess, ok := e.sessions[id]; ok && sess.IsExpired() {
				delete(e.sessions, id)
			}
		}
		e.mu.Unlock()
	}
}

// =============================================================================
// Document Operations
// =============================================================================

func (e *Engine) AddDocument(sessionID, extID, filename string) (*types.Document, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sess.AddDocument(extID, filename)
}

func (e *Engine) GetDocument(sessionID string, id uint64) (*types.Document, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetDocument(id)
}

func (e *Engine) DeleteDocument(sessionID string, id uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.DeleteDocument(id)
}

func (e *Engine) UpdateDocumentStatus(sessionID string, id uint64, status types.DocumentStatus) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	doc, ok := sess.GetDocument(id)
	if !ok {
		return false
	}
	doc.Status = status
	return true
}

// =============================================================================
// TextUnit Operations
// =============================================================================

func (e *Engine) AddTextUnit(sessionID, extID string, docID uint64, content string, embedding []float32, tokenCount int) (*types.TextUnit, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sess.AddTextUnit(extID, docID, content, embedding, tokenCount)
}

func (e *Engine) GetTextUnit(sessionID string, id uint64) (*types.TextUnit, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetTextUnit(id)
}

func (e *Engine) DeleteTextUnit(sessionID string, id uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.DeleteTextUnit(id)
}

func (e *Engine) LinkTextUnitToEntity(sessionID string, tuID, entityID uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.LinkTextUnitToEntity(tuID, entityID)
}

// =============================================================================
// Entity Operations
// =============================================================================

func (e *Engine) AddEntity(sessionID, extID, title, entType, description string, embedding []float32) (*types.Entity, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sess.AddEntity(extID, title, entType, description, embedding)
}

func (e *Engine) GetEntity(sessionID string, id uint64) (*types.Entity, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetEntity(id)
}

func (e *Engine) GetEntityByTitle(sessionID, title string) (*types.Entity, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetEntityByTitle(title)
}

func (e *Engine) UpdateEntityDescription(sessionID string, id uint64, description string, embedding []float32) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.UpdateEntityDescription(id, description, embedding)
}

func (e *Engine) DeleteEntity(sessionID string, id uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.DeleteEntity(id)
}

// =============================================================================
// Relationship Operations
// =============================================================================

func (e *Engine) AddRelationship(sessionID, extID string, sourceID, targetID uint64, relType, description string, weight float32) (*types.Relationship, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sess.AddRelationship(extID, sourceID, targetID, relType, description, weight)
}

func (e *Engine) GetRelationship(sessionID string, id uint64) (*types.Relationship, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetRelationship(id)
}

func (e *Engine) GetRelationshipByEntities(sessionID string, sourceID, targetID uint64) (*types.Relationship, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetRelationshipBySourceTarget(sourceID, targetID)
}

func (e *Engine) DeleteRelationship(sessionID string, id uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.DeleteRelationship(id)
}

// =============================================================================
// Community Operations
// =============================================================================

func (e *Engine) AddCommunity(sessionID, extID, title, summary, fullContent string, level int, entityIDs, relIDs []uint64, embedding []float32) (*types.Community, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sess.AddCommunity(extID, title, summary, fullContent, level, entityIDs, relIDs, embedding)
}

func (e *Engine) GetCommunity(sessionID string, id uint64) (*types.Community, bool) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, false
	}
	return sess.GetCommunity(id)
}

func (e *Engine) DeleteCommunity(sessionID string, id uint64) bool {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return false
	}
	return sess.DeleteCommunity(id)
}

// ComputeCommunities runs Leiden clustering and creates communities
func (e *Engine) ComputeCommunities(sessionID string, config graph.LeidenConfig) ([]*types.Community, error) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Create adapter for Leiden algorithm
	entities := sess.GetAllEntities()
	relationships := sess.GetAllRelationships()
	idGen := sess.GetIDGenerator()

	// Build entity and relationship stores for Leiden
	entStore := &entityStoreAdapter{entities: entities}
	relStore := &relationshipStoreAdapter{
		relationships: relationships,
		outEdges:      make(map[uint64][]*types.Relationship),
		inEdges:       make(map[uint64][]*types.Relationship),
	}
	for _, rel := range relationships {
		relStore.outEdges[rel.SourceID] = append(relStore.outEdges[rel.SourceID], rel)
		relStore.inEdges[rel.TargetID] = append(relStore.inEdges[rel.TargetID], rel)
	}

	leiden := graph.NewLeiden(entStore, relStore, config)
	clusters := leiden.ComputeCommunities()

	// Clear existing communities
	sess.ClearCommunities()

	// Build community objects
	communities := graph.BuildCommunities(clusters, entStore, relStore, idGen, 0)

	for _, comm := range communities {
		sess.AddCommunity(comm.ExternalID, comm.Title, comm.Summary, comm.FullContent, comm.Level, comm.EntityIDs, comm.RelationshipIDs, nil)
	}

	return communities, nil
}

// ComputeHierarchicalCommunities runs hierarchical Leiden clustering
func (e *Engine) ComputeHierarchicalCommunities(sessionID string, config graph.LeidenConfig) ([]*types.Community, error) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Enforce max 5 levels
	if config.MaxLevels > 5 {
		config.MaxLevels = 5
	}
	if config.MaxLevels < 1 {
		config.MaxLevels = 1
	}
	if config.MinCommunitySize < 2 {
		config.MinCommunitySize = 2
	}
	if config.LevelResolution <= 0 || config.LevelResolution >= 1 {
		config.LevelResolution = 0.7
	}

	entities := sess.GetAllEntities()
	relationships := sess.GetAllRelationships()
	idGen := sess.GetIDGenerator()

	entStore := &entityStoreAdapter{entities: entities}
	relStore := &relationshipStoreAdapter{
		relationships: relationships,
		outEdges:      make(map[uint64][]*types.Relationship),
		inEdges:       make(map[uint64][]*types.Relationship),
	}
	for _, rel := range relationships {
		relStore.outEdges[rel.SourceID] = append(relStore.outEdges[rel.SourceID], rel)
		relStore.inEdges[rel.TargetID] = append(relStore.inEdges[rel.TargetID], rel)
	}

	leiden := graph.NewLeiden(entStore, relStore, config)
	hierarchical := leiden.ComputeHierarchicalCommunities()

	// Clear existing communities
	sess.ClearCommunities()

	// Build community objects from hierarchical results
	communities := graph.BuildHierarchicalCommunities(hierarchical, entStore, relStore, idGen)

	for _, comm := range communities {
		sess.AddCommunity(comm.ExternalID, comm.Title, comm.Summary, comm.FullContent, comm.Level, comm.EntityIDs, comm.RelationshipIDs, nil)
	}

	return communities, nil
}

// =============================================================================
// Query - Main Query Pipeline
// =============================================================================

func (e *Engine) Query(sessionID string, spec types.QuerySpec) (*types.ContextPack, error) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, err
	}

	startTime := time.Now()

	// Atomically increment query ID without global lock
	queryID := atomic.AddUint64(&e.queryIDGen, 1)

	// Initialize query log
	qlog := &queryLog{
		sessionID: sessionID,
		spec:      spec,
		seeds:     make([]types.SeedInfo, 0),
	}

	// Results containers
	textUnitResults := make(map[uint64]*types.TextUnitResult)
	entityResults := make(map[uint64]*types.EntityResult)
	communityResults := make(map[uint64]*types.CommunityResult)

	stats := types.QueryStats{}

	// Get indexes
	textUnitIndex := sess.GetTextUnitIndex()
	entityIndex := sess.GetEntityIndex()
	communityIndex := sess.GetCommunityIndex()

	// Phase 1: Vector search on selected indices
	for _, searchType := range spec.SearchTypes {
		switch searchType {
		case types.SearchTypeTextUnit:
			if textUnitIndex != nil {
				results := textUnitIndex.Search(spec.QueryVector, spec.TopK)
				stats.TextUnitsSearched = textUnitIndex.Count()

				for _, r := range results {
					if tu, ok := sess.GetTextUnit(r.ID); ok {
						textUnitResults[r.ID] = &types.TextUnitResult{
							TextUnit:   tu,
							Score:      r.Similarity,
							Similarity: r.Similarity,
							Hop:        0,
						}

						qlog.seeds = append(qlog.seeds, types.SeedInfo{
							Type:       types.SearchTypeTextUnit,
							ID:         r.ID,
							ExternalID: tu.ExternalID,
							Similarity: r.Similarity,
							LinkedIDs:  tu.EntityIDs,
						})
					}
				}
			}

		case types.SearchTypeEntity:
			if entityIndex != nil {
				results := entityIndex.Search(spec.QueryVector, spec.TopK)
				stats.EntitiesSearched = entityIndex.Count()

				for _, r := range results {
					if ent, ok := sess.GetEntity(r.ID); ok {
						entityResults[r.ID] = &types.EntityResult{
							Entity:     ent,
							Score:      r.Similarity,
							Similarity: r.Similarity,
							Hop:        0,
						}

						qlog.seeds = append(qlog.seeds, types.SeedInfo{
							Type:       types.SearchTypeEntity,
							ID:         r.ID,
							ExternalID: ent.ExternalID,
							Similarity: r.Similarity,
							LinkedIDs:  ent.TextUnitIDs,
						})
					}
				}
			}

		case types.SearchTypeCommunity:
			if communityIndex != nil {
				results := communityIndex.Search(spec.QueryVector, spec.TopK)
				stats.CommunitiesSearched = communityIndex.Count()

				for _, r := range results {
					if comm, ok := sess.GetCommunity(r.ID); ok {
						communityResults[r.ID] = &types.CommunityResult{
							Community:  comm,
							Score:      r.Similarity,
							Similarity: r.Similarity,
						}

						qlog.seeds = append(qlog.seeds, types.SeedInfo{
							Type:       types.SearchTypeCommunity,
							ID:         r.ID,
							ExternalID: comm.ExternalID,
							Similarity: r.Similarity,
							LinkedIDs:  comm.EntityIDs,
						})
					}
				}
			}
		}
	}

	// Phase 2: Graph expansion from entity seeds
	if spec.KHops > 0 {
		// Collect seed entity IDs
		seedEntityIDs := make([]uint64, 0)

		// From direct entity search
		for eid := range entityResults {
			seedEntityIDs = append(seedEntityIDs, eid)
		}

		// From text unit links
		for _, tur := range textUnitResults {
			seedEntityIDs = append(seedEntityIDs, tur.TextUnit.EntityIDs...)
		}

		// From community members
		for _, cr := range communityResults {
			seedEntityIDs = append(seedEntityIDs, cr.Community.EntityIDs...)
		}

		// BFS traversal using session's relationship store
		relAdapter := &sessionRelAdapter{sess: sess}
		visitedIDs, hopMap, traversal := graph.BFSTraversal(
			seedEntityIDs,
			relAdapter,
			spec.KHops,
			spec.MaxEntities,
		)

		stats.EdgesScanned = len(traversal)
		qlog.traversal = traversal

		// Add discovered entities
		for _, eid := range visitedIDs {
			if _, exists := entityResults[eid]; !exists {
				if ent, ok := sess.GetEntity(eid); ok {
					hop := hopMap[eid]
					score := float32(1.0 / float64(1+hop))

					entityResults[eid] = &types.EntityResult{
						Entity:     ent,
						Score:      score,
						Similarity: 0,
						Hop:        hop,
					}
				}
			}
		}

		// Collect text units from discovered entities
		for _, er := range entityResults {
			for _, tuID := range er.Entity.TextUnitIDs {
				if _, exists := textUnitResults[tuID]; !exists {
					if tu, ok := sess.GetTextUnit(tuID); ok {
						hop := er.Hop + 1
						score := float32(1.0 / float64(1+hop))

						textUnitResults[tuID] = &types.TextUnitResult{
							TextUnit:   tu,
							Score:      score,
							Similarity: 0,
							Hop:        hop,
						}
					}
				}
			}
		}
	}

	// Phase 3: Collect relationships between found entities
	relationshipResults := make([]types.RelationshipResult, 0)
	entitySet := make(map[uint64]bool)
	for eid := range entityResults {
		entitySet[eid] = true
	}

	for eid := range entityResults {
		rels := sess.GetOutgoingRelationships(eid)
		for _, rel := range rels {
			if entitySet[rel.TargetID] {
				sourceEnt, _ := sess.GetEntity(rel.SourceID)
				targetEnt, _ := sess.GetEntity(rel.TargetID)

				sourceTitle := ""
				targetTitle := ""
				if sourceEnt != nil {
					sourceTitle = sourceEnt.Title
				}
				if targetEnt != nil {
					targetTitle = targetEnt.Title
				}

				relationshipResults = append(relationshipResults, types.RelationshipResult{
					Relationship: rel,
					SourceTitle:  sourceTitle,
					TargetTitle:  targetTitle,
				})
			}
		}
	}

	// Phase 4: Sort and limit results
	textUnitList := make([]types.TextUnitResult, 0, len(textUnitResults))
	for _, tur := range textUnitResults {
		textUnitList = append(textUnitList, *tur)
	}
	sort.Slice(textUnitList, func(i, j int) bool {
		return textUnitList[i].Score > textUnitList[j].Score
	})
	if len(textUnitList) > spec.MaxTextUnits {
		textUnitList = textUnitList[:spec.MaxTextUnits]
	}

	entityList := make([]types.EntityResult, 0, len(entityResults))
	for _, er := range entityResults {
		entityList = append(entityList, *er)
	}
	sort.Slice(entityList, func(i, j int) bool {
		return entityList[i].Score > entityList[j].Score
	})
	if len(entityList) > spec.MaxEntities {
		entityList = entityList[:spec.MaxEntities]
	}

	communityList := make([]types.CommunityResult, 0, len(communityResults))
	for _, cr := range communityResults {
		communityList = append(communityList, *cr)
	}
	sort.Slice(communityList, func(i, j int) bool {
		return communityList[i].Score > communityList[j].Score
	})
	if len(communityList) > spec.MaxCommunities {
		communityList = communityList[:spec.MaxCommunities]
	}

	stats.DurationMicros = time.Since(startTime).Microseconds()

	// Save query log
	e.queryLogs.Set(queryID, qlog)

	return &types.ContextPack{
		QueryID:       queryID,
		TextUnits:     textUnitList,
		Entities:      entityList,
		Communities:   communityList,
		Relationships: relationshipResults,
		Stats:         stats,
	}, nil
}

// =============================================================================
// Explain - Query Explanation
// =============================================================================

func (e *Engine) Explain(queryID uint64) (*types.ExplainPack, bool) {
	qlog, ok := e.queryLogs.Get(queryID)
	if !ok {
		return nil, false
	}

	return &types.ExplainPack{
		QueryID:   queryID,
		Seeds:     qlog.seeds,
		Traversal: qlog.traversal,
	}, true
}

// =============================================================================
// Server Info
// =============================================================================

func (e *Engine) Info() types.ServerInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var docCount, tuCount, entCount, relCount, commCount int
	for _, sess := range e.sessions {
		if !sess.IsExpired() {
			docCount += sess.DocumentCount()
			tuCount += sess.TextUnitCount()
			entCount += sess.EntityCount()
			relCount += sess.RelationshipCount()
			commCount += sess.CommunityCount()
		}
	}

	return types.ServerInfo{
		Version:           version.Version,
		DocumentCount:     docCount,
		TextUnitCount:     tuCount,
		EntityCount:       entCount,
		RelationshipCount: relCount,
		CommunityCount:    commCount,
		VectorDim:         e.vectorDim,
		SessionCount:      len(e.sessions),
	}
}

// InfoForSession returns info for a specific session
func (e *Engine) InfoForSession(sessionID string) (types.ServerInfo, error) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return types.ServerInfo{}, err
	}

	return types.ServerInfo{
		Version:           version.Version,
		DocumentCount:     sess.DocumentCount(),
		TextUnitCount:     sess.TextUnitCount(),
		EntityCount:       sess.EntityCount(),
		RelationshipCount: sess.RelationshipCount(),
		CommunityCount:    sess.CommunityCount(),
		VectorDim:         e.vectorDim,
		SessionCount:      1,
	}, nil
}

// =============================================================================
// Index Operations
// =============================================================================

// RebuildVectorIndices rebuilds all vector indices for a session
func (e *Engine) RebuildVectorIndices(sessionID string) error {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return err
	}

	// Get vectors from current indices before recreating
	tuIdx := sess.GetTextUnitIndex()
	entIdx := sess.GetEntityIndex()
	commIdx := sess.GetCommunityIndex()

	textUnitVectors := tuIdx.GetAllVectors()
	entityVectors := entIdx.GetAllVectors()
	communityVectors := commIdx.GetAllVectors()

	// Clear and rebuild
	sess.Clear()

	// Re-add vectors
	newTuIdx := sess.GetTextUnitIndex()
	newEntIdx := sess.GetEntityIndex()
	newCommIdx := sess.GetCommunityIndex()

	for id, vec := range textUnitVectors {
		newTuIdx.Add(id, vec)
	}
	for id, vec := range entityVectors {
		newEntIdx.Add(id, vec)
	}
	for id, vec := range communityVectors {
		newCommIdx.Add(id, vec)
	}

	return nil
}

// =============================================================================
// Bulk Operations
// =============================================================================

// MSetDocuments adds multiple documents
func (e *Engine) MSetDocuments(sessionID string, inputs []types.BulkDocumentInput) ([]uint64, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(inputs))
	for _, input := range inputs {
		doc, err := sess.AddDocument(input.ExternalID, input.Filename)
		if err != nil {
			continue
		}
		ids = append(ids, doc.ID)
	}
	return ids, nil
}

// MGetDocuments gets multiple documents
func (e *Engine) MGetDocuments(sessionID string, ids []uint64) []*types.Document {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil
	}

	result := make([]*types.Document, 0, len(ids))
	for _, id := range ids {
		if doc, ok := sess.GetDocument(id); ok {
			result = append(result, doc)
		}
	}
	return result
}

// MSetTextUnits adds multiple text units
func (e *Engine) MSetTextUnits(sessionID string, inputs []types.BulkTextUnitInput) ([]uint64, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(inputs))
	for _, input := range inputs {
		tu, err := sess.AddTextUnit(input.ExternalID, input.DocumentID, input.Content, input.Embedding, input.TokenCount)
		if err != nil {
			continue
		}
		ids = append(ids, tu.ID)
	}
	return ids, nil
}

// MGetTextUnits gets multiple text units
func (e *Engine) MGetTextUnits(sessionID string, ids []uint64) []*types.TextUnit {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil
	}

	result := make([]*types.TextUnit, 0, len(ids))
	for _, id := range ids {
		if tu, ok := sess.GetTextUnit(id); ok {
			result = append(result, tu)
		}
	}
	return result
}

// MSetEntities adds multiple entities
func (e *Engine) MSetEntities(sessionID string, inputs []types.BulkEntityInput) ([]uint64, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(inputs))
	for _, input := range inputs {
		ent, err := sess.AddEntity(input.ExternalID, input.Title, input.Type, input.Description, input.Embedding)
		if err != nil {
			continue
		}
		ids = append(ids, ent.ID)
	}
	return ids, nil
}

// MGetEntities gets multiple entities
func (e *Engine) MGetEntities(sessionID string, ids []uint64) []*types.Entity {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil
	}

	result := make([]*types.Entity, 0, len(ids))
	for _, id := range ids {
		if ent, ok := sess.GetEntity(id); ok {
			result = append(result, ent)
		}
	}
	return result
}

// ListEntities returns entities after the given cursor, up to limit, in ID order.
func (e *Engine) ListEntities(sessionID string, cursor uint64, limit int) ([]*types.Entity, uint64) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, 0
	}
	return sess.ListEntities(cursor, limit)
}

// MSetRelationships adds multiple relationships
func (e *Engine) MSetRelationships(sessionID string, inputs []types.BulkRelationshipInput) ([]uint64, error) {
	sess, err := e.getOrCreateSession(sessionID)
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(inputs))
	for _, input := range inputs {
		rel, err := sess.AddRelationship(input.ExternalID, input.SourceID, input.TargetID, input.Type, input.Description, input.Weight)
		if err != nil {
			continue
		}
		ids = append(ids, rel.ID)
	}
	return ids, nil
}

// MGetRelationships gets multiple relationships
func (e *Engine) MGetRelationships(sessionID string, ids []uint64) []*types.Relationship {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil
	}

	result := make([]*types.Relationship, 0, len(ids))
	for _, id := range ids {
		if rel, ok := sess.GetRelationship(id); ok {
			result = append(result, rel)
		}
	}
	return result
}

// ListRelationships returns relationships after the given cursor, up to limit, in ID order.
func (e *Engine) ListRelationships(sessionID string, cursor uint64, limit int) ([]*types.Relationship, uint64) {
	sess, err := e.getSession(sessionID)
	if err != nil {
		return nil, 0
	}
	return sess.ListRelationships(cursor, limit)
}

// =============================================================================
// Snapshot/Restore
// =============================================================================

// EngineSnapshot contains all engine state for serialization
type EngineSnapshot struct {
	Version   string                            `json:"version"`
	VectorDim int                               `json:"vector_dim"`
	Sessions  map[string]*store.SessionSnapshot `json:"sessions"`
}

// Snapshot serializes the entire engine state to a writer
func (e *Engine) Snapshot(w io.Writer) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := EngineSnapshot{
		Version:   version.Version,
		VectorDim: e.vectorDim,
		Sessions:  make(map[string]*store.SessionSnapshot),
	}

	for id, sess := range e.sessions {
		if !sess.IsExpired() {
			snapshot.Sessions[id] = sess.Snapshot()
		}
	}

	encoder := json.NewEncoder(w)
	return encoder.Encode(snapshot)
}

// Restore deserializes engine state from a reader
func (e *Engine) Restore(r io.Reader) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var snapshot EngineSnapshot
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	// Validate snapshot
	if snapshot.VectorDim != e.vectorDim {
		return fmt.Errorf("vector dimension mismatch: snapshot=%d, engine=%d", snapshot.VectorDim, e.vectorDim)
	}

	// Clear current state
	e.sessions = make(map[string]*store.SessionStore)

	// Restore sessions
	for id, sessSnapshot := range snapshot.Sessions {
		sess := store.NewSessionStore(id, e.vectorDim)
		if err := sess.RestoreFromSnapshot(sessSnapshot); err != nil {
			return fmt.Errorf("restore session %s: %w", id, err)
		}
		e.sessions[id] = sess
	}

	return nil
}

// Clear clears all data in the engine
func (e *Engine) Clear() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.sessions = make(map[string]*store.SessionStore)
	e.queryIDGen = 0

	return nil
}

// =============================================================================
// Getters for backward compatibility
// =============================================================================

// GetSession returns a session store (for handlers)
func (e *Engine) GetSession(sessionID string) (*store.SessionStore, error) {
	return e.getSession(sessionID)
}

// GetOrCreateSession returns or creates a session store (for handlers)
func (e *Engine) GetOrCreateSession(sessionID string) (*store.SessionStore, error) {
	return e.getOrCreateSession(sessionID)
}

// =============================================================================
// Adapters for Leiden algorithm
// =============================================================================

type entityStoreAdapter struct {
	entities []*types.Entity
}

func (a *entityStoreAdapter) GetAll() []*types.Entity {
	return a.entities
}

func (a *entityStoreAdapter) Get(id uint64) (*types.Entity, bool) {
	for _, e := range a.entities {
		if e.ID == id {
			return e, true
		}
	}
	return nil, false
}

type relationshipStoreAdapter struct {
	relationships []*types.Relationship
	outEdges      map[uint64][]*types.Relationship
	inEdges       map[uint64][]*types.Relationship
}

func (a *relationshipStoreAdapter) GetAll() []*types.Relationship {
	return a.relationships
}

func (a *relationshipStoreAdapter) Get(id uint64) (*types.Relationship, bool) {
	for _, r := range a.relationships {
		if r.ID == id {
			return r, true
		}
	}
	return nil, false
}

func (a *relationshipStoreAdapter) GetOutgoing(entityID uint64) []*types.Relationship {
	return a.outEdges[entityID]
}

func (a *relationshipStoreAdapter) GetIncoming(entityID uint64) []*types.Relationship {
	return a.inEdges[entityID]
}

func (a *relationshipStoreAdapter) GetNeighbors(entityID uint64) []*types.Relationship {
	// Return both outgoing and incoming
	result := make([]*types.Relationship, 0, len(a.outEdges[entityID])+len(a.inEdges[entityID]))
	result = append(result, a.outEdges[entityID]...)
	result = append(result, a.inEdges[entityID]...)
	return result
}

// sessionRelAdapter adapts SessionStore for graph traversal
type sessionRelAdapter struct {
	sess *store.SessionStore
}

func (a *sessionRelAdapter) GetAll() []*types.Relationship {
	return a.sess.GetAllRelationships()
}

func (a *sessionRelAdapter) Get(id uint64) (*types.Relationship, bool) {
	return a.sess.GetRelationship(id)
}

func (a *sessionRelAdapter) GetOutgoing(entityID uint64) []*types.Relationship {
	return a.sess.GetOutgoingRelationships(entityID)
}

func (a *sessionRelAdapter) GetIncoming(entityID uint64) []*types.Relationship {
	return a.sess.GetIncomingRelationships(entityID)
}

func (a *sessionRelAdapter) GetNeighbors(entityID uint64) []*types.Relationship {
	// Return both outgoing and incoming
	result := make([]*types.Relationship, 0)
	result = append(result, a.sess.GetOutgoingRelationships(entityID)...)
	result = append(result, a.sess.GetIncomingRelationships(entityID)...)
	return result
}
