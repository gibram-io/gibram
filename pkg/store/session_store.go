// Package store provides session-based storage for GibRAM with data isolation
package store

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gibram-io/gibram/pkg/types"
	"github.com/gibram-io/gibram/pkg/vector"
)

// =============================================================================
// SessionStore - Partitioned storage per session
// =============================================================================

// SessionStore holds all data for a single session with isolated indexes
type SessionStore struct {
	mu sync.RWMutex

	// Session metadata
	session *types.Session

	// ID Generator (per-session)
	idGen *types.IDGenerator

	// Data stores
	documents     map[uint64]*types.Document
	docByExtID    map[string]uint64
	docByFilename map[string]uint64

	textUnits map[uint64]*types.TextUnit
	tuByExtID map[string]uint64
	tuByDocID map[uint64][]uint64

	entities   map[uint64]*types.Entity
	entByExtID map[string]uint64
	entByTitle map[string]uint64

	relationships     map[uint64]*types.Relationship
	relByExtID        map[string]uint64
	relBySourceTarget map[string]uint64
	outEdges          map[uint64][]uint64
	inEdges           map[uint64][]uint64

	communities map[uint64]*types.Community
	commByExtID map[string]uint64
	commByLevel map[int][]uint64

	// Vector indices (per-session, lazy initialized)
	textUnitIndex  vector.Index
	entityIndex    vector.Index
	communityIndex vector.Index
	vectorDim      int
}

// NewSessionStore creates a new session store
func NewSessionStore(sessionID string, vectorDim int) *SessionStore {
	return &SessionStore{
		session:   types.NewSession(sessionID),
		idGen:     types.NewIDGenerator(),
		vectorDim: vectorDim,

		// Documents
		documents:     make(map[uint64]*types.Document),
		docByExtID:    make(map[string]uint64),
		docByFilename: make(map[string]uint64),

		// TextUnits
		textUnits: make(map[uint64]*types.TextUnit),
		tuByExtID: make(map[string]uint64),
		tuByDocID: make(map[uint64][]uint64),

		// Entities
		entities:   make(map[uint64]*types.Entity),
		entByExtID: make(map[string]uint64),
		entByTitle: make(map[string]uint64),

		// Relationships
		relationships:     make(map[uint64]*types.Relationship),
		relByExtID:        make(map[string]uint64),
		relBySourceTarget: make(map[string]uint64),
		outEdges:          make(map[uint64][]uint64),
		inEdges:           make(map[uint64][]uint64),

		// Communities
		communities: make(map[uint64]*types.Community),
		commByExtID: make(map[string]uint64),
		commByLevel: make(map[int][]uint64),
	}
}

// =============================================================================
// Session Management
// =============================================================================

// GetSession returns session metadata
func (s *SessionStore) GetSession() *types.Session {
	return s.session
}

// GetSessionID returns the session ID
func (s *SessionStore) GetSessionID() string {
	return s.session.ID
}

// Touch updates session last access time
func (s *SessionStore) Touch() {
	s.session.Touch()
}

// IsExpired checks if session has expired
func (s *SessionStore) IsExpired() bool {
	return s.session.IsExpired()
}

// SetTTL sets session absolute TTL
func (s *SessionStore) SetTTL(ttl int64) {
	s.session.SetTTL(ttl)
}

// SetIdleTTL sets session idle TTL
func (s *SessionStore) SetIdleTTL(idleTTL int64) {
	s.session.SetIdleTTL(idleTTL)
}

// GetInfo returns session info with counts
func (s *SessionStore) GetInfo() types.SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info := s.session.GetInfo()
	info.DocumentCount = len(s.documents)
	info.TextUnitCount = len(s.textUnits)
	info.EntityCount = len(s.entities)
	info.RelationshipCount = len(s.relationships)
	info.CommunityCount = len(s.communities)
	return info
}

// GetIDGenerator returns the ID generator
func (s *SessionStore) GetIDGenerator() *types.IDGenerator {
	return s.idGen
}

// =============================================================================
// Vector Index Management (lazy initialization)
// =============================================================================

func (s *SessionStore) getTextUnitIndex() vector.Index {
	if s.textUnitIndex == nil {
		s.textUnitIndex = vector.NewHNSWIndex(s.vectorDim, vector.DefaultHNSWConfig())
	}
	return s.textUnitIndex
}

func (s *SessionStore) getEntityIndex() vector.Index {
	if s.entityIndex == nil {
		s.entityIndex = vector.NewHNSWIndex(s.vectorDim, vector.DefaultHNSWConfig())
	}
	return s.entityIndex
}

func (s *SessionStore) getCommunityIndex() vector.Index {
	if s.communityIndex == nil {
		s.communityIndex = vector.NewHNSWIndex(s.vectorDim, vector.DefaultHNSWConfig())
	}
	return s.communityIndex
}

// GetTextUnitIndex returns the text unit vector index
func (s *SessionStore) GetTextUnitIndex() vector.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getTextUnitIndex()
}

// GetEntityIndex returns the entity vector index
func (s *SessionStore) GetEntityIndex() vector.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getEntityIndex()
}

// GetCommunityIndex returns the community vector index
func (s *SessionStore) GetCommunityIndex() vector.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCommunityIndex()
}

// =============================================================================
// Document Operations
// =============================================================================

// AddDocument adds a document to the session
func (s *SessionStore) AddDocument(extID, filename string) (*types.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.docByExtID[extID]; exists {
		return nil, fmt.Errorf("document with external_id %s already exists", extID)
	}

	doc := types.NewDocument(s.idGen.NextDocumentID(), extID, filename)
	s.documents[doc.ID] = doc
	s.docByExtID[extID] = doc.ID
	if filename != "" {
		s.docByFilename[filename] = doc.ID
	}

	s.session.Touch()
	return doc, nil
}

// GetDocument retrieves a document by ID
func (s *SessionStore) GetDocument(id uint64) (*types.Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.documents[id]
	if ok {
		s.session.Touch()
	}
	return doc, ok
}

// GetDocumentByExternalID retrieves a document by external ID
func (s *SessionStore) GetDocumentByExternalID(extID string) (*types.Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.docByExtID[extID]
	if !ok {
		return nil, false
	}
	return s.documents[id], true
}

// DeleteDocument removes a document
func (s *SessionStore) DeleteDocument(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.documents[id]
	if !ok {
		return false
	}

	delete(s.docByExtID, doc.ExternalID)
	delete(s.docByFilename, doc.Filename)
	delete(s.documents, id)

	s.session.Touch()
	return true
}

// GetAllDocuments returns all documents
func (s *SessionStore) GetAllDocuments() []*types.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.Document, 0, len(s.documents))
	for _, doc := range s.documents {
		result = append(result, doc)
	}
	return result
}

// DocumentCount returns the number of documents
func (s *SessionStore) DocumentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.documents)
}

// =============================================================================
// TextUnit Operations
// =============================================================================

// AddTextUnit adds a text unit to the session
func (s *SessionStore) AddTextUnit(extID string, docID uint64, content string, embedding []float32, tokenCount int) (*types.TextUnit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tuByExtID[extID]; exists {
		return nil, fmt.Errorf("textunit with external_id %s already exists", extID)
	}

	tu := types.NewTextUnit(s.idGen.NextTextUnitID(), extID, docID, content, tokenCount)
	s.textUnits[tu.ID] = tu
	s.tuByExtID[extID] = tu.ID
	s.tuByDocID[docID] = append(s.tuByDocID[docID], tu.ID)

	// Add to vector index
	if len(embedding) > 0 {
		if err := s.getTextUnitIndex().Add(tu.ID, embedding); err != nil {
			delete(s.textUnits, tu.ID)
			delete(s.tuByExtID, extID)
			return nil, err
		}
	}

	s.session.Touch()
	return tu, nil
}

// GetTextUnit retrieves a text unit by ID
func (s *SessionStore) GetTextUnit(id uint64) (*types.TextUnit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tu, ok := s.textUnits[id]
	if ok {
		s.session.Touch()
	}
	return tu, ok
}

// GetTextUnitsByDocumentID retrieves all text units for a document
func (s *SessionStore) GetTextUnitsByDocumentID(docID uint64) []*types.TextUnit {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.tuByDocID[docID]
	result := make([]*types.TextUnit, 0, len(ids))
	for _, id := range ids {
		if tu, ok := s.textUnits[id]; ok {
			result = append(result, tu)
		}
	}
	return result
}

// DeleteTextUnit removes a text unit
func (s *SessionStore) DeleteTextUnit(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tu, ok := s.textUnits[id]
	if !ok {
		return false
	}

	delete(s.tuByExtID, tu.ExternalID)

	// Remove from byDocID
	docIDs := s.tuByDocID[tu.DocumentID]
	for i, tid := range docIDs {
		if tid == id {
			s.tuByDocID[tu.DocumentID] = append(docIDs[:i], docIDs[i+1:]...)
			break
		}
	}

	delete(s.textUnits, id)

	if s.textUnitIndex != nil {
		s.textUnitIndex.Remove(id)
	}

	s.session.Touch()
	return true
}

// LinkTextUnitToEntity links a text unit to an entity
func (s *SessionStore) LinkTextUnitToEntity(tuID, entityID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tu, ok := s.textUnits[tuID]
	if !ok {
		return false
	}

	ent, ok := s.entities[entityID]
	if !ok {
		return false
	}

	tu.AddEntityID(entityID)
	ent.AddTextUnitID(tuID)

	s.session.Touch()
	return true
}

// GetAllTextUnits returns all text units
func (s *SessionStore) GetAllTextUnits() []*types.TextUnit {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.TextUnit, 0, len(s.textUnits))
	for _, tu := range s.textUnits {
		result = append(result, tu)
	}
	return result
}

// TextUnitCount returns the number of text units
func (s *SessionStore) TextUnitCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.textUnits)
}

// =============================================================================
// Entity Operations
// =============================================================================

// AddEntity adds an entity to the session
func (s *SessionStore) AddEntity(extID, title, entType, description string, embedding []float32) (*types.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalizedTitle := strings.ToUpper(strings.TrimSpace(title))

	if _, exists := s.entByTitle[normalizedTitle]; exists {
		return nil, fmt.Errorf("entity with title %s already exists", title)
	}
	if extID != "" {
		if _, exists := s.entByExtID[extID]; exists {
			return nil, fmt.Errorf("entity with external_id %s already exists", extID)
		}
	}

	ent := types.NewEntity(s.idGen.NextEntityID(), extID, normalizedTitle, entType, description)
	s.entities[ent.ID] = ent
	s.entByTitle[normalizedTitle] = ent.ID
	if extID != "" {
		s.entByExtID[extID] = ent.ID
	}

	// Add to vector index
	if len(embedding) > 0 {
		if err := s.getEntityIndex().Add(ent.ID, embedding); err != nil {
			delete(s.entities, ent.ID)
			delete(s.entByTitle, normalizedTitle)
			delete(s.entByExtID, extID)
			return nil, err
		}
	}

	s.session.Touch()
	return ent, nil
}

// GetEntity retrieves an entity by ID
func (s *SessionStore) GetEntity(id uint64) (*types.Entity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ent, ok := s.entities[id]
	if ok {
		s.session.Touch()
	}
	return ent, ok
}

// GetEntityByTitle retrieves an entity by title
func (s *SessionStore) GetEntityByTitle(title string) (*types.Entity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalizedTitle := strings.ToUpper(strings.TrimSpace(title))
	id, ok := s.entByTitle[normalizedTitle]
	if !ok {
		return nil, false
	}
	return s.entities[id], true
}

// UpdateEntityDescription updates an entity's description
func (s *SessionStore) UpdateEntityDescription(id uint64, description string, embedding []float32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	ent, ok := s.entities[id]
	if !ok {
		return false
	}

	ent.Description = description

	// Update vector index
	if len(embedding) > 0 && s.entityIndex != nil {
		s.entityIndex.Remove(id)
		if err := s.entityIndex.Add(id, embedding); err != nil {
			return false
		}
	}

	s.session.Touch()
	return true
}

// DeleteEntity removes an entity
func (s *SessionStore) DeleteEntity(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	ent, ok := s.entities[id]
	if !ok {
		return false
	}

	delete(s.entByTitle, ent.Title)
	delete(s.entByExtID, ent.ExternalID)
	delete(s.entities, id)

	if s.entityIndex != nil {
		s.entityIndex.Remove(id)
	}

	s.session.Touch()
	return true
}

// GetAllEntities returns all entities
func (s *SessionStore) GetAllEntities() []*types.Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.Entity, 0, len(s.entities))
	for _, ent := range s.entities {
		result = append(result, ent)
	}
	return result
}

// ListEntities returns entities after the given cursor, up to limit, in ID order.
func (s *SessionStore) ListEntities(afterID uint64, limit int) ([]*types.Entity, uint64) {
	if limit <= 0 {
		limit = 1000
	}

	s.mu.RLock()
	ids := make([]uint64, 0, len(s.entities))
	for id := range s.entities {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > afterID })

	results := make([]*types.Entity, 0, limit)
	i := start
	var lastID uint64

	s.mu.RLock()
	for ; i < len(ids) && len(results) < limit; i++ {
		lastID = ids[i]
		if ent, ok := s.entities[lastID]; ok {
			results = append(results, ent)
		}
	}
	s.mu.RUnlock()

	s.session.Touch()

	if i < len(ids) {
		return results, lastID
	}
	return results, 0
}

// EntityCount returns the number of entities
func (s *SessionStore) EntityCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entities)
}

// =============================================================================
// Relationship Operations
// =============================================================================

func (s *SessionStore) makeRelKey(sourceID, targetID uint64) string {
	return fmt.Sprintf("%d|%d", sourceID, targetID)
}

// AddRelationship adds a relationship to the session
func (s *SessionStore) AddRelationship(extID string, sourceID, targetID uint64, relType, description string, weight float32) (*types.Relationship, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeRelKey(sourceID, targetID)
	if _, exists := s.relBySourceTarget[key]; exists {
		return nil, fmt.Errorf("relationship from %d to %d already exists", sourceID, targetID)
	}
	if extID != "" {
		if _, exists := s.relByExtID[extID]; exists {
			return nil, fmt.Errorf("relationship with external_id %s already exists", extID)
		}
	}

	if weight == 0 {
		weight = 1.0
	}

	rel := types.NewRelationship(s.idGen.NextRelationshipID(), extID, sourceID, targetID, relType, description, weight)
	s.relationships[rel.ID] = rel
	s.relBySourceTarget[key] = rel.ID
	if extID != "" {
		s.relByExtID[extID] = rel.ID
	}
	s.outEdges[sourceID] = append(s.outEdges[sourceID], rel.ID)
	s.inEdges[targetID] = append(s.inEdges[targetID], rel.ID)

	s.session.Touch()
	return rel, nil
}

// GetRelationship retrieves a relationship by ID
func (s *SessionStore) GetRelationship(id uint64) (*types.Relationship, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rel, ok := s.relationships[id]
	if ok {
		s.session.Touch()
	}
	return rel, ok
}

// GetRelationshipBySourceTarget retrieves a relationship by source and target
func (s *SessionStore) GetRelationshipBySourceTarget(sourceID, targetID uint64) (*types.Relationship, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.makeRelKey(sourceID, targetID)
	id, ok := s.relBySourceTarget[key]
	if !ok {
		return nil, false
	}
	return s.relationships[id], true
}

// GetOutgoingRelationships retrieves outgoing relationships for an entity
func (s *SessionStore) GetOutgoingRelationships(entityID uint64) []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.outEdges[entityID]
	result := make([]*types.Relationship, 0, len(ids))
	for _, id := range ids {
		if rel, ok := s.relationships[id]; ok {
			result = append(result, rel)
		}
	}
	return result
}

// GetIncomingRelationships retrieves incoming relationships for an entity
func (s *SessionStore) GetIncomingRelationships(entityID uint64) []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.inEdges[entityID]
	result := make([]*types.Relationship, 0, len(ids))
	for _, id := range ids {
		if rel, ok := s.relationships[id]; ok {
			result = append(result, rel)
		}
	}
	return result
}

// GetNeighbors returns all neighboring entity IDs
func (s *SessionStore) GetNeighbors(entityID uint64) []uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	neighborSet := make(map[uint64]struct{})

	for _, relID := range s.outEdges[entityID] {
		if rel, ok := s.relationships[relID]; ok {
			neighborSet[rel.TargetID] = struct{}{}
		}
	}
	for _, relID := range s.inEdges[entityID] {
		if rel, ok := s.relationships[relID]; ok {
			neighborSet[rel.SourceID] = struct{}{}
		}
	}

	result := make([]uint64, 0, len(neighborSet))
	for id := range neighborSet {
		result = append(result, id)
	}
	return result
}

// DeleteRelationship removes a relationship
func (s *SessionStore) DeleteRelationship(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	rel, ok := s.relationships[id]
	if !ok {
		return false
	}

	key := s.makeRelKey(rel.SourceID, rel.TargetID)
	delete(s.relBySourceTarget, key)
	delete(s.relByExtID, rel.ExternalID)

	// Remove from outEdges
	outIDs := s.outEdges[rel.SourceID]
	for i, rid := range outIDs {
		if rid == id {
			s.outEdges[rel.SourceID] = append(outIDs[:i], outIDs[i+1:]...)
			break
		}
	}

	// Remove from inEdges
	inIDs := s.inEdges[rel.TargetID]
	for i, rid := range inIDs {
		if rid == id {
			s.inEdges[rel.TargetID] = append(inIDs[:i], inIDs[i+1:]...)
			break
		}
	}

	delete(s.relationships, id)

	s.session.Touch()
	return true
}

// GetAllRelationships returns all relationships
func (s *SessionStore) GetAllRelationships() []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.Relationship, 0, len(s.relationships))
	for _, rel := range s.relationships {
		result = append(result, rel)
	}
	return result
}

// ListRelationships returns relationships after the given cursor, up to limit, in ID order.
func (s *SessionStore) ListRelationships(afterID uint64, limit int) ([]*types.Relationship, uint64) {
	if limit <= 0 {
		limit = 1000
	}

	s.mu.RLock()
	ids := make([]uint64, 0, len(s.relationships))
	for id := range s.relationships {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > afterID })

	results := make([]*types.Relationship, 0, limit)
	i := start
	var lastID uint64

	s.mu.RLock()
	for ; i < len(ids) && len(results) < limit; i++ {
		lastID = ids[i]
		if rel, ok := s.relationships[lastID]; ok {
			results = append(results, rel)
		}
	}
	s.mu.RUnlock()

	s.session.Touch()

	if i < len(ids) {
		return results, lastID
	}
	return results, 0
}

// RelationshipCount returns the number of relationships
func (s *SessionStore) RelationshipCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.relationships)
}

// =============================================================================
// Community Operations
// =============================================================================

// AddCommunity adds a community to the session
func (s *SessionStore) AddCommunity(extID, title, summary, fullContent string, level int, entityIDs, relIDs []uint64, embedding []float32) (*types.Community, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if extID != "" {
		if _, exists := s.commByExtID[extID]; exists {
			return nil, fmt.Errorf("community with external_id %s already exists", extID)
		}
	}

	comm := types.NewCommunity(s.idGen.NextCommunityID(), extID, title, summary, fullContent, level, entityIDs, relIDs)
	s.communities[comm.ID] = comm
	if extID != "" {
		s.commByExtID[extID] = comm.ID
	}
	s.commByLevel[level] = append(s.commByLevel[level], comm.ID)

	// Add to vector index
	if len(embedding) > 0 {
		if err := s.getCommunityIndex().Add(comm.ID, embedding); err != nil {
			delete(s.communities, comm.ID)
			delete(s.commByExtID, extID)
			return nil, err
		}
	}

	s.session.Touch()
	return comm, nil
}

// GetCommunity retrieves a community by ID
func (s *SessionStore) GetCommunity(id uint64) (*types.Community, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	comm, ok := s.communities[id]
	if ok {
		s.session.Touch()
	}
	return comm, ok
}

// GetCommunitiesByLevel retrieves communities at a level
func (s *SessionStore) GetCommunitiesByLevel(level int) []*types.Community {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := s.commByLevel[level]
	result := make([]*types.Community, 0, len(ids))
	for _, id := range ids {
		if comm, ok := s.communities[id]; ok {
			result = append(result, comm)
		}
	}
	return result
}

// DeleteCommunity removes a community
func (s *SessionStore) DeleteCommunity(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	comm, ok := s.communities[id]
	if !ok {
		return false
	}

	delete(s.commByExtID, comm.ExternalID)

	// Remove from byLevel
	levelIDs := s.commByLevel[comm.Level]
	for i, cid := range levelIDs {
		if cid == id {
			s.commByLevel[comm.Level] = append(levelIDs[:i], levelIDs[i+1:]...)
			break
		}
	}

	delete(s.communities, id)

	if s.communityIndex != nil {
		s.communityIndex.Remove(id)
	}

	s.session.Touch()
	return true
}

// ClearCommunities removes all communities (useful before re-computing)
func (s *SessionStore) ClearCommunities() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.communities = make(map[uint64]*types.Community)
	s.commByExtID = make(map[string]uint64)
	s.commByLevel = make(map[int][]uint64)

	if s.communityIndex != nil {
		s.communityIndex = vector.NewHNSWIndex(s.vectorDim, vector.DefaultHNSWConfig())
	}
}

// GetAllCommunities returns all communities
func (s *SessionStore) GetAllCommunities() []*types.Community {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*types.Community, 0, len(s.communities))
	for _, comm := range s.communities {
		result = append(result, comm)
	}
	return result
}

// CommunityCount returns the number of communities
func (s *SessionStore) CommunityCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.communities)
}

// =============================================================================
// Bulk Operations
// =============================================================================

// Clear removes all data from the session store
func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.documents = make(map[uint64]*types.Document)
	s.docByExtID = make(map[string]uint64)
	s.docByFilename = make(map[string]uint64)

	s.textUnits = make(map[uint64]*types.TextUnit)
	s.tuByExtID = make(map[string]uint64)
	s.tuByDocID = make(map[uint64][]uint64)

	s.entities = make(map[uint64]*types.Entity)
	s.entByExtID = make(map[string]uint64)
	s.entByTitle = make(map[string]uint64)

	s.relationships = make(map[uint64]*types.Relationship)
	s.relByExtID = make(map[string]uint64)
	s.relBySourceTarget = make(map[string]uint64)
	s.outEdges = make(map[uint64][]uint64)
	s.inEdges = make(map[uint64][]uint64)

	s.communities = make(map[uint64]*types.Community)
	s.commByExtID = make(map[string]uint64)
	s.commByLevel = make(map[int][]uint64)

	// Reset vector indices
	s.textUnitIndex = nil
	s.entityIndex = nil
	s.communityIndex = nil

	// Reset ID generator
	s.idGen = types.NewIDGenerator()
}

// =============================================================================
// Snapshot/Restore Support
// =============================================================================

// SessionSnapshot contains all session state for serialization
type SessionSnapshot struct {
	SessionID        string                `json:"session_id"`
	Session          *types.Session        `json:"session"`
	Documents        []*types.Document     `json:"documents"`
	TextUnits        []*types.TextUnit     `json:"text_units"`
	Entities         []*types.Entity       `json:"entities"`
	Relationships    []*types.Relationship `json:"relationships"`
	Communities      []*types.Community    `json:"communities"`
	IDGeneratorState map[string]uint64     `json:"id_generator_state"`
	TextUnitVectors  map[uint64][]float32  `json:"text_unit_vectors"`
	EntityVectors    map[uint64][]float32  `json:"entity_vectors"`
	CommunityVectors map[uint64][]float32  `json:"community_vectors"`
}

// Snapshot creates a snapshot of the session
func (s *SessionStore) Snapshot() *SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := &SessionSnapshot{
		SessionID:        s.session.ID,
		Session:          s.session,
		Documents:        s.GetAllDocuments(),
		TextUnits:        s.GetAllTextUnits(),
		Entities:         s.GetAllEntities(),
		Relationships:    s.GetAllRelationships(),
		Communities:      s.GetAllCommunities(),
		IDGeneratorState: make(map[string]uint64),
	}

	// Save ID generator state
	doc, tu, ent, rel, comm, _ := s.idGen.GetCounters()
	snapshot.IDGeneratorState["document"] = doc
	snapshot.IDGeneratorState["textunit"] = tu
	snapshot.IDGeneratorState["entity"] = ent
	snapshot.IDGeneratorState["relationship"] = rel
	snapshot.IDGeneratorState["community"] = comm

	// Save vector indices
	if s.textUnitIndex != nil {
		snapshot.TextUnitVectors = s.textUnitIndex.GetAllVectors()
	}
	if s.entityIndex != nil {
		snapshot.EntityVectors = s.entityIndex.GetAllVectors()
	}
	if s.communityIndex != nil {
		snapshot.CommunityVectors = s.communityIndex.GetAllVectors()
	}

	return snapshot
}

// RestoreFromSnapshot restores a session from a snapshot
func (s *SessionStore) RestoreFromSnapshot(snapshot *SessionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Restore session metadata
	s.session = snapshot.Session

	// Clear and restore documents
	s.documents = make(map[uint64]*types.Document)
	s.docByExtID = make(map[string]uint64)
	s.docByFilename = make(map[string]uint64)
	for _, doc := range snapshot.Documents {
		s.documents[doc.ID] = doc
		s.docByExtID[doc.ExternalID] = doc.ID
		if doc.Filename != "" {
			s.docByFilename[doc.Filename] = doc.ID
		}
	}

	// Clear and restore text units
	s.textUnits = make(map[uint64]*types.TextUnit)
	s.tuByExtID = make(map[string]uint64)
	s.tuByDocID = make(map[uint64][]uint64)
	for _, tu := range snapshot.TextUnits {
		s.textUnits[tu.ID] = tu
		s.tuByExtID[tu.ExternalID] = tu.ID
		s.tuByDocID[tu.DocumentID] = append(s.tuByDocID[tu.DocumentID], tu.ID)
	}

	// Clear and restore entities
	s.entities = make(map[uint64]*types.Entity)
	s.entByExtID = make(map[string]uint64)
	s.entByTitle = make(map[string]uint64)
	for _, ent := range snapshot.Entities {
		s.entities[ent.ID] = ent
		s.entByTitle[ent.Title] = ent.ID
		if ent.ExternalID != "" {
			s.entByExtID[ent.ExternalID] = ent.ID
		}
	}

	// Clear and restore relationships
	s.relationships = make(map[uint64]*types.Relationship)
	s.relByExtID = make(map[string]uint64)
	s.relBySourceTarget = make(map[string]uint64)
	s.outEdges = make(map[uint64][]uint64)
	s.inEdges = make(map[uint64][]uint64)
	for _, rel := range snapshot.Relationships {
		s.relationships[rel.ID] = rel
		key := s.makeRelKey(rel.SourceID, rel.TargetID)
		s.relBySourceTarget[key] = rel.ID
		if rel.ExternalID != "" {
			s.relByExtID[rel.ExternalID] = rel.ID
		}
		s.outEdges[rel.SourceID] = append(s.outEdges[rel.SourceID], rel.ID)
		s.inEdges[rel.TargetID] = append(s.inEdges[rel.TargetID], rel.ID)
	}

	// Clear and restore communities
	s.communities = make(map[uint64]*types.Community)
	s.commByExtID = make(map[string]uint64)
	s.commByLevel = make(map[int][]uint64)
	for _, comm := range snapshot.Communities {
		s.communities[comm.ID] = comm
		if comm.ExternalID != "" {
			s.commByExtID[comm.ExternalID] = comm.ID
		}
		s.commByLevel[comm.Level] = append(s.commByLevel[comm.Level], comm.ID)
	}

	// Restore ID generator
	if snapshot.IDGeneratorState != nil {
		s.idGen.RestoreState(snapshot.IDGeneratorState)
	}

	// Restore vector indices
	s.textUnitIndex = nil
	s.entityIndex = nil
	s.communityIndex = nil

	if len(snapshot.TextUnitVectors) > 0 {
		idx := s.getTextUnitIndex()
		for id, vec := range snapshot.TextUnitVectors {
			if err := idx.Add(id, vec); err != nil {
				return err
			}
		}
	}
	if len(snapshot.EntityVectors) > 0 {
		idx := s.getEntityIndex()
		for id, vec := range snapshot.EntityVectors {
			if err := idx.Add(id, vec); err != nil {
				return err
			}
		}
	}
	if len(snapshot.CommunityVectors) > 0 {
		idx := s.getCommunityIndex()
		for id, vec := range snapshot.CommunityVectors {
			if err := idx.Add(id, vec); err != nil {
				return err
			}
		}
	}

	return nil
}
