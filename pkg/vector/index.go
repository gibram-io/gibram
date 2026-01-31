// Package vector provides vector index implementations for GibRAM
package vector

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sort"
	"sync"

	"github.com/gibram-io/gibram/pkg/simd"
)

// =============================================================================
// Index Interface
// =============================================================================

type SearchResult struct {
	ID         uint64
	Similarity float32
}

type Index interface {
	Add(id uint64, vector []float32) error
	Remove(id uint64) bool
	Search(query []float32, k int) []SearchResult
	Count() int
	Dimension() int
	Save(w io.Writer) error
	Load(r io.Reader) error

	// Rebuild functionality
	GetAllVectors() map[uint64][]float32 // Get raw vectors for rebuild
	Rebuild() error                      // Rebuild index from scratch
	ValidateIntegrity() error            // Check if index is corrupted
}

// =============================================================================
// HNSW Index - Hierarchical Navigable Small World
// =============================================================================

type HNSWConfig struct {
	M              int     // max connections per node
	EfConstruction int     // size of dynamic candidate list during construction
	EfSearch       int     // size of dynamic candidate list during search
	MaxLevel       int     // max layer
	ML             float64 // level multiplier (1/ln(M))
}

func DefaultHNSWConfig() HNSWConfig {
	return HNSWConfig{
		M:              16,
		EfConstruction: 200,
		EfSearch:       50,
		MaxLevel:       16,
		ML:             1.0 / math.Log(16),
	}
}

type hnswNode struct {
	id      uint64
	vector  []float32
	level   int
	friends [][]uint64 // friends[level] = list of connected node IDs
}

type HNSWIndex struct {
	mu        sync.RWMutex
	config    HNSWConfig
	dimension int
	nodes     map[uint64]*hnswNode
	entryID   uint64
	maxLevel  int
}

func NewHNSWIndex(dimension int, config HNSWConfig) *HNSWIndex {
	return &HNSWIndex{
		config:    config,
		dimension: dimension,
		nodes:     make(map[uint64]*hnswNode),
		entryID:   0,
		maxLevel:  -1,
	}
}

func (h *HNSWIndex) Dimension() int {
	return h.dimension
}

func (h *HNSWIndex) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// randomLevel generates a random level for a new node
func (h *HNSWIndex) randomLevel() int {
	level := 0
	for rand.Float64() < h.config.ML && level < h.config.MaxLevel {
		level++
	}
	return level
}

// cosineSimilarity calculates cosine similarity between two vectors
// Uses SIMD-optimized implementation when available
func cosineSimilarity(a, b []float32) float32 {
	return simd.CosineSimilarity(a, b)
}

// Add inserts a vector into the index
func (h *HNSWIndex) Add(id uint64, vector []float32) error {
	if len(vector) != h.dimension {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", h.dimension, len(vector))
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already exists
	if _, exists := h.nodes[id]; exists {
		return fmt.Errorf("vector with id %d already exists", id)
	}

	// Create new node
	level := h.randomLevel()
	node := &hnswNode{
		id:      id,
		vector:  make([]float32, len(vector)),
		level:   level,
		friends: make([][]uint64, level+1),
	}
	copy(node.vector, vector)

	for i := range node.friends {
		node.friends[i] = make([]uint64, 0, h.config.M)
	}

	// First node
	if len(h.nodes) == 0 {
		h.nodes[id] = node
		h.entryID = id
		h.maxLevel = level
		return nil
	}

	// Find entry point and search down
	currID := h.entryID

	// Traverse from top level to node's level + 1
	for l := h.maxLevel; l > level; l-- {
		currID = h.searchLayerClosest(vector, currID, l)
	}

	// Insert at each level from level to 0
	for l := min(level, h.maxLevel); l >= 0; l-- {
		neighbors := h.searchLayer(vector, currID, h.config.EfConstruction, l)

		// Select M best neighbors
		selectedNeighbors := h.selectNeighbors(vector, neighbors, h.config.M)

		// Connect node to neighbors
		node.friends[l] = selectedNeighbors

		// Connect neighbors back to node
		for _, neighborID := range selectedNeighbors {
			neighbor := h.nodes[neighborID]
			if neighbor != nil && l < len(neighbor.friends) {
				neighbor.friends[l] = append(neighbor.friends[l], id)

				// Prune if too many connections
				if len(neighbor.friends[l]) > h.config.M*2 {
					neighbor.friends[l] = h.selectNeighbors(neighbor.vector, neighbor.friends[l], h.config.M)
				}
			}
		}

		if len(selectedNeighbors) > 0 {
			currID = selectedNeighbors[0]
		}
	}

	h.nodes[id] = node

	// Update entry point if necessary
	if level > h.maxLevel {
		h.entryID = id
		h.maxLevel = level
	}

	return nil
}

// searchLayerClosest finds the closest node to query in a single layer
func (h *HNSWIndex) searchLayerClosest(query []float32, entryID uint64, level int) uint64 {
	currID := entryID
	currDist := cosineSimilarity(query, h.nodes[currID].vector)

	changed := true
	for changed {
		changed = false
		currNode := h.nodes[currID]
		if currNode == nil || level >= len(currNode.friends) {
			break
		}

		for _, friendID := range currNode.friends[level] {
			friend := h.nodes[friendID]
			if friend == nil {
				continue
			}
			dist := cosineSimilarity(query, friend.vector)
			if dist > currDist {
				currID = friendID
				currDist = dist
				changed = true
			}
		}
	}

	return currID
}

// searchLayer finds ef closest nodes to query starting from entry
func (h *HNSWIndex) searchLayer(query []float32, entryID uint64, ef int, level int) []uint64 {
	visited := make(map[uint64]bool)
	candidates := &priorityQueue{}
	result := &priorityQueue{}

	entry := h.nodes[entryID]
	if entry == nil {
		return nil
	}

	dist := cosineSimilarity(query, entry.vector)
	visited[entryID] = true

	candidates.Push(pqItem{id: entryID, priority: dist})
	result.Push(pqItem{id: entryID, priority: dist})

	for candidates.Len() > 0 {
		curr := candidates.Pop()
		currNode := h.nodes[curr.id]
		if currNode == nil {
			continue
		}

		// Get worst result
		worst := result.Peek()

		// If current is farther than worst result and we have enough, stop
		if curr.priority < worst.priority && result.Len() >= ef {
			break
		}

		// Explore neighbors
		if level < len(currNode.friends) {
			for _, neighborID := range currNode.friends[level] {
				if visited[neighborID] {
					continue
				}
				visited[neighborID] = true

				neighbor := h.nodes[neighborID]
				if neighbor == nil {
					continue
				}

				neighborDist := cosineSimilarity(query, neighbor.vector)
				worst = result.Peek()

				if result.Len() < ef || neighborDist > worst.priority {
					candidates.Push(pqItem{id: neighborID, priority: neighborDist})
					result.Push(pqItem{id: neighborID, priority: neighborDist})

					if result.Len() > ef {
						result.PopWorst()
					}
				}
			}
		}
	}

	// Extract IDs from result
	ids := make([]uint64, 0, result.Len())
	for result.Len() > 0 {
		item := result.Pop()
		ids = append(ids, item.id)
	}

	return ids
}

// selectNeighbors selects the M best neighbors
func (h *HNSWIndex) selectNeighbors(query []float32, candidates []uint64, M int) []uint64 {
	if len(candidates) <= M {
		return candidates
	}

	// Score and sort candidates
	type scored struct {
		id    uint64
		score float32
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, id := range candidates {
		node := h.nodes[id]
		if node != nil {
			scoredCandidates = append(scoredCandidates, scored{id: id, score: cosineSimilarity(query, node.vector)})
		}
	}

	sort.Slice(scoredCandidates, func(i, j int) bool {
		return scoredCandidates[i].score > scoredCandidates[j].score
	})

	result := make([]uint64, 0, M)
	for i := 0; i < M && i < len(scoredCandidates); i++ {
		result = append(result, scoredCandidates[i].id)
	}

	return result
}

// Search finds the k most similar vectors to query
func (h *HNSWIndex) Search(query []float32, k int) []SearchResult {
	if len(query) != h.dimension {
		return nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.nodes) == 0 {
		return nil
	}

	// Start from entry point and traverse down
	currID := h.entryID

	for l := h.maxLevel; l > 0; l-- {
		currID = h.searchLayerClosest(query, currID, l)
	}

	// Search at level 0 with ef neighbors
	ef := max(h.config.EfSearch, k)
	neighborIDs := h.searchLayer(query, currID, ef, 0)

	// Score all neighbors
	type scored struct {
		id    uint64
		score float32
	}
	scoredNeighbors := make([]scored, 0, len(neighborIDs))
	for _, id := range neighborIDs {
		node := h.nodes[id]
		if node != nil {
			scoredNeighbors = append(scoredNeighbors, scored{id: id, score: cosineSimilarity(query, node.vector)})
		}
	}

	// Sort by score descending
	sort.Slice(scoredNeighbors, func(i, j int) bool {
		return scoredNeighbors[i].score > scoredNeighbors[j].score
	})

	// Return top k
	results := make([]SearchResult, 0, k)
	for i := 0; i < k && i < len(scoredNeighbors); i++ {
		results = append(results, SearchResult{
			ID:         scoredNeighbors[i].id,
			Similarity: scoredNeighbors[i].score,
		})
	}

	return results
}

// Remove deletes a vector from the index with proper neighbor reconnection
func (h *HNSWIndex) Remove(id uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodes[id]
	if !exists {
		return false
	}

	// For each level, remove connections and reconnect orphaned neighbors
	for level, friends := range node.friends {
		// Collect all neighbors that were connected to this node
		affectedNeighbors := make(map[uint64]bool)

		for _, friendID := range friends {
			friend := h.nodes[friendID]
			if friend == nil || level >= len(friend.friends) {
				continue
			}

			// Remove id from friend's connections
			newFriends := make([]uint64, 0, len(friend.friends[level]))
			for _, fid := range friend.friends[level] {
				if fid != id {
					newFriends = append(newFriends, fid)
				}
			}
			friend.friends[level] = newFriends
			affectedNeighbors[friendID] = true
		}

		// Reconnect affected neighbors to maintain graph connectivity
		// This prevents graph fragmentation after deletion
		h.reconnectNeighbors(level, affectedNeighbors, id)
	}

	delete(h.nodes, id)

	// Update entry point if necessary
	if h.entryID == id {
		if len(h.nodes) == 0 {
			h.entryID = 0
			h.maxLevel = -1
		} else {
			// Find new entry point (node with highest level)
			var newEntry uint64
			newMaxLevel := -1
			for nid, n := range h.nodes {
				if n.level > newMaxLevel {
					newMaxLevel = n.level
					newEntry = nid
				}
			}
			h.entryID = newEntry
			h.maxLevel = newMaxLevel
		}
	}

	return true
}

// reconnectNeighbors ensures affected neighbors maintain connectivity after node removal
func (h *HNSWIndex) reconnectNeighbors(level int, affected map[uint64]bool, deletedID uint64) {
	if len(affected) < 2 {
		return // No need to reconnect if only one neighbor
	}

	// For each affected neighbor, try to connect to other affected neighbors
	// This maintains the local graph structure
	for neighborID := range affected {
		neighbor := h.nodes[neighborID]
		if neighbor == nil || level >= len(neighbor.friends) {
			continue
		}

		currentFriendCount := len(neighbor.friends[level])
		maxFriends := h.config.M
		if level == 0 {
			maxFriends = h.config.M * 2
		}

		// If neighbor lost a connection and has room, find new connections
		if currentFriendCount < maxFriends {
			// Find best candidates from other affected neighbors
			candidates := make([]uint64, 0, len(affected))
			for candidateID := range affected {
				if candidateID == neighborID {
					continue
				}
				// Check if already connected
				alreadyConnected := false
				for _, fid := range neighbor.friends[level] {
					if fid == candidateID {
						alreadyConnected = true
						break
					}
				}
				if !alreadyConnected {
					candidates = append(candidates, candidateID)
				}
			}

			if len(candidates) == 0 {
				continue
			}

			// Select best candidates based on similarity
			selected := h.selectNeighborsForReconnect(neighbor.vector, candidates, maxFriends-currentFriendCount)

			// Add bidirectional connections
			for _, selectedID := range selected {
				selectedNode := h.nodes[selectedID]
				if selectedNode == nil || level >= len(selectedNode.friends) {
					continue
				}

				// Add connection neighbor -> selected
				neighbor.friends[level] = append(neighbor.friends[level], selectedID)

				// Add connection selected -> neighbor (if has room)
				selectedFriendCount := len(selectedNode.friends[level])
				selectedMaxFriends := h.config.M
				if level == 0 {
					selectedMaxFriends = h.config.M * 2
				}

				if selectedFriendCount < selectedMaxFriends {
					selectedNode.friends[level] = append(selectedNode.friends[level], neighborID)
				}
			}
		}
	}
}

// selectNeighborsForReconnect selects best neighbors during reconnection (no pruning, just selection)
func (h *HNSWIndex) selectNeighborsForReconnect(query []float32, candidates []uint64, maxCount int) []uint64 {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) <= maxCount {
		return candidates
	}

	// Score and sort candidates
	type scored struct {
		id    uint64
		score float32
	}
	scoredCandidates := make([]scored, 0, len(candidates))
	for _, id := range candidates {
		node := h.nodes[id]
		if node != nil {
			scoredCandidates = append(scoredCandidates, scored{id: id, score: cosineSimilarity(query, node.vector)})
		}
	}

	sort.Slice(scoredCandidates, func(i, j int) bool {
		return scoredCandidates[i].score > scoredCandidates[j].score
	})

	result := make([]uint64, 0, maxCount)
	for i := 0; i < maxCount && i < len(scoredCandidates); i++ {
		result = append(result, scoredCandidates[i].id)
	}

	return result
}

// Save serializes the index to a writer
func (h *HNSWIndex) Save(w io.Writer) error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Write header
	header := struct {
		Dimension int32
		Count     int32
		EntryID   uint64
		MaxLevel  int32
	}{
		Dimension: int32(h.dimension),
		Count:     int32(len(h.nodes)),
		EntryID:   h.entryID,
		MaxLevel:  int32(h.maxLevel),
	}

	if err := binary.Write(w, binary.LittleEndian, &header); err != nil {
		return err
	}

	// Write each node
	for _, node := range h.nodes {
		// Write node header
		nodeHeader := struct {
			ID    uint64
			Level int32
		}{
			ID:    node.id,
			Level: int32(node.level),
		}
		if err := binary.Write(w, binary.LittleEndian, &nodeHeader); err != nil {
			return err
		}

		// Write vector
		if err := binary.Write(w, binary.LittleEndian, node.vector); err != nil {
			return err
		}

		// Write friends for each level
		for l := 0; l <= node.level; l++ {
			friendCount := int32(len(node.friends[l]))
			if err := binary.Write(w, binary.LittleEndian, friendCount); err != nil {
				return err
			}
			if err := binary.Write(w, binary.LittleEndian, node.friends[l]); err != nil {
				return err
			}
		}
	}

	return nil
}

// Load deserializes the index from a reader
func (h *HNSWIndex) Load(r io.Reader) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Read and validate header
	var header struct {
		Dimension int32
		Count     int32
		EntryID   uint64
		MaxLevel  int32
	}

	if err := binary.Read(r, binary.LittleEndian, &header); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Validate header fields before using them
	if header.Dimension <= 0 || header.Dimension > 10000 {
		return fmt.Errorf("invalid dimension in header: %d", header.Dimension)
	}
	if header.Count < 0 || header.Count > 100000000 {
		return fmt.Errorf("invalid node count in header: %d", header.Count)
	}
	if header.MaxLevel < -1 || header.MaxLevel > 20 {
		return fmt.Errorf("invalid max level in header: %d", header.MaxLevel)
	}

	h.dimension = int(header.Dimension)
	h.entryID = header.EntryID
	h.maxLevel = int(header.MaxLevel)
	h.nodes = make(map[uint64]*hnswNode, header.Count)

	// Read each node
	for i := 0; i < int(header.Count); i++ {
		var nodeHeader struct {
			ID    uint64
			Level int32
		}
		if err := binary.Read(r, binary.LittleEndian, &nodeHeader); err != nil {
			return fmt.Errorf("failed to read node %d header: %w", i, err)
		}

		// Validate node header
		if nodeHeader.Level < 0 || nodeHeader.Level > 20 {
			return fmt.Errorf("invalid node level: %d", nodeHeader.Level)
		}

		node := &hnswNode{
			id:      nodeHeader.ID,
			level:   int(nodeHeader.Level),
			vector:  make([]float32, h.dimension),
			friends: make([][]uint64, nodeHeader.Level+1),
		}

		// Read vector
		if err := binary.Read(r, binary.LittleEndian, node.vector); err != nil {
			return fmt.Errorf("failed to read node %d vector: %w", i, err)
		}

		// Read friends for each level
		for l := 0; l <= node.level; l++ {
			var friendCount int32
			if err := binary.Read(r, binary.LittleEndian, &friendCount); err != nil {
				return fmt.Errorf("failed to read friend count at level %d: %w", l, err)
			}

			// Validate friend count to prevent allocation attacks
			if friendCount < 0 || friendCount > 10000 {
				return fmt.Errorf("invalid friend count: %d", friendCount)
			}

			node.friends[l] = make([]uint64, friendCount)
			if friendCount > 0 {
				if err := binary.Read(r, binary.LittleEndian, node.friends[l]); err != nil {
					return fmt.Errorf("failed to read friends at level %d: %w", l, err)
				}
			}
		}

		h.nodes[node.id] = node
	}

	return nil
}

// GetAllVectors returns all vectors in the index (for rebuild)
func (h *HNSWIndex) GetAllVectors() map[uint64][]float32 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[uint64][]float32, len(h.nodes))
	for id, node := range h.nodes {
		copied := make([]float32, len(node.vector))
		copy(copied, node.vector)
		result[id] = copied
	}
	return result
}

// Rebuild rebuilds the HNSW graph from scratch using existing vectors
// This is useful when the graph structure becomes corrupted after many deletions
func (h *HNSWIndex) Rebuild() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.nodes) == 0 {
		return nil
	}

	// Extract all vectors (snapshot for transaction safety)
	vectors := make(map[uint64][]float32, len(h.nodes))
	for id, node := range h.nodes {
		// Deep copy vector to prevent external modification during rebuild
		vectorCopy := make([]float32, len(node.vector))
		copy(vectorCopy, node.vector)
		vectors[id] = vectorCopy
	}

	// Create backup of current graph state for rollback
	backup := &struct {
		nodes    map[uint64]*hnswNode
		entryID  uint64
		maxLevel int
	}{
		nodes:    h.nodes,
		entryID:  h.entryID,
		maxLevel: h.maxLevel,
	}

	// Initialize new graph structure
	h.nodes = make(map[uint64]*hnswNode)
	h.entryID = 0
	h.maxLevel = -1

	// Re-add all vectors with error handling
	for id, vector := range vectors {
		// Create new node with random level
		level := h.randomLevel()
		node := &hnswNode{
			id:      id,
			vector:  vector,
			level:   level,
			friends: make([][]uint64, level+1),
		}
		for i := range node.friends {
			node.friends[i] = make([]uint64, 0, h.config.M)
		}

		// First node
		if len(h.nodes) == 0 {
			h.nodes[id] = node
			h.entryID = id
			h.maxLevel = level
			continue
		}

		// Find entry point and search down
		currID := h.entryID

		// Traverse from top level to node's level + 1
		for l := h.maxLevel; l > level; l-- {
			currID = h.searchLayerClosest(vector, currID, l)
		}

		// Insert at each level from level to 0
		for l := min(level, h.maxLevel); l >= 0; l-- {
			neighbors := h.searchLayer(vector, currID, h.config.EfConstruction, l)
			selectedNeighbors := h.selectNeighbors(vector, neighbors, h.config.M)

			node.friends[l] = selectedNeighbors

			for _, neighborID := range selectedNeighbors {
				neighbor := h.nodes[neighborID]
				if neighbor != nil && l < len(neighbor.friends) {
					neighbor.friends[l] = append(neighbor.friends[l], id)
					if len(neighbor.friends[l]) > h.config.M*2 {
						neighbor.friends[l] = h.selectNeighbors(neighbor.vector, neighbor.friends[l], h.config.M)
					}
				}
			}

			if len(selectedNeighbors) > 0 {
				currID = selectedNeighbors[0]
			}
		}

		h.nodes[id] = node

		if level > h.maxLevel {
			h.entryID = id
			h.maxLevel = level
		}
	}

	// Validate the rebuilt index
	if err := h.validateIntegrityLocked(); err != nil {
		// Rollback on validation failure
		h.nodes = backup.nodes
		h.entryID = backup.entryID
		h.maxLevel = backup.maxLevel
		return fmt.Errorf("rebuild validation failed, rolled back: %w", err)
	}

	return nil
}

// ValidateIntegrity checks if the HNSW graph structure is valid
// Returns an error describing the corruption if found
func (h *HNSWIndex) ValidateIntegrity() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.validateIntegrityLocked()
}

// validateIntegrityLocked assumes the caller already holds a lock.
func (h *HNSWIndex) validateIntegrityLocked() error {
	if len(h.nodes) == 0 {
		return nil
	}

	// Check entry point exists
	if _, exists := h.nodes[h.entryID]; !exists && len(h.nodes) > 0 {
		return fmt.Errorf("entry point %d does not exist", h.entryID)
	}

	// Check each node
	orphanCount := 0
	danglingRefCount := 0

	for id, node := range h.nodes {
		// Check vector dimension
		if len(node.vector) != h.dimension {
			return fmt.Errorf("node %d has wrong dimension: expected %d, got %d", id, h.dimension, len(node.vector))
		}

		// Check level consistency
		if node.level < 0 || node.level > h.config.MaxLevel {
			return fmt.Errorf("node %d has invalid level: %d", id, node.level)
		}

		if len(node.friends) != node.level+1 {
			return fmt.Errorf("node %d friends array length mismatch: expected %d, got %d", id, node.level+1, len(node.friends))
		}

		// Check connections at each level
		for level, friends := range node.friends {
			for _, friendID := range friends {
				if _, exists := h.nodes[friendID]; !exists {
					danglingRefCount++
				}
			}

			// Check for orphans at level 0 (no connections)
			if level == 0 && len(friends) == 0 && len(h.nodes) > 1 {
				orphanCount++
			}
		}
	}

	// Report issues but allow small numbers (may be transient)
	// Stricter threshold: 1% for production safety
	if danglingRefCount > len(h.nodes)/100 {
		return fmt.Errorf("high number of dangling references: %d (>1%% of nodes)", danglingRefCount)
	}

	// 5% threshold for orphan nodes (more lenient as they're less critical)
	if orphanCount > len(h.nodes)/20 {
		return fmt.Errorf("high number of orphan nodes: %d (>5%% of nodes)", orphanCount)
	}

	return nil
}

// TryLoadWithRebuild loads the index, validates it, and rebuilds if corrupted
func (h *HNSWIndex) TryLoadWithRebuild(r io.Reader) error {
	if err := h.Load(r); err != nil {
		return fmt.Errorf("load failed: %w", err)
	}

	if err := h.ValidateIntegrity(); err != nil {
		// Index is corrupted, rebuild
		fmt.Printf("Index corruption detected (%v), rebuilding...\n", err)
		if err := h.Rebuild(); err != nil {
			return fmt.Errorf("rebuild failed: %w", err)
		}
		fmt.Println("Index rebuilt successfully")
	}

	return nil
}

// =============================================================================
// Priority Queue for HNSW
// =============================================================================

type pqItem struct {
	id       uint64
	priority float32
}

type priorityQueue struct {
	items []pqItem
}

func (pq *priorityQueue) Len() int {
	return len(pq.items)
}

func (pq *priorityQueue) Push(item pqItem) {
	pq.items = append(pq.items, item)
	pq.bubbleUp(len(pq.items) - 1)
}

func (pq *priorityQueue) Pop() pqItem {
	if len(pq.items) == 0 {
		return pqItem{}
	}
	item := pq.items[0]
	n := len(pq.items) - 1
	pq.items[0] = pq.items[n]
	pq.items = pq.items[:n]
	if n > 0 {
		pq.bubbleDown(0)
	}
	return item
}

func (pq *priorityQueue) Peek() pqItem {
	if len(pq.items) == 0 {
		return pqItem{}
	}
	return pq.items[0]
}

func (pq *priorityQueue) PopWorst() pqItem {
	if len(pq.items) == 0 {
		return pqItem{}
	}
	// Find minimum (worst similarity)
	minIdx := 0
	for i := 1; i < len(pq.items); i++ {
		if pq.items[i].priority < pq.items[minIdx].priority {
			minIdx = i
		}
	}
	item := pq.items[minIdx]
	pq.items = append(pq.items[:minIdx], pq.items[minIdx+1:]...)
	return item
}

func (pq *priorityQueue) bubbleUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if pq.items[i].priority <= pq.items[parent].priority {
			break
		}
		pq.items[i], pq.items[parent] = pq.items[parent], pq.items[i]
		i = parent
	}
}

func (pq *priorityQueue) bubbleDown(i int) {
	n := len(pq.items)
	for {
		largest := i
		left := 2*i + 1
		right := 2*i + 2

		if left < n && pq.items[left].priority > pq.items[largest].priority {
			largest = left
		}
		if right < n && pq.items[right].priority > pq.items[largest].priority {
			largest = right
		}

		if largest == i {
			break
		}

		pq.items[i], pq.items[largest] = pq.items[largest], pq.items[i]
		i = largest
	}
}

// =============================================================================
// Brute Force Index (for small datasets or testing)
// =============================================================================

type BruteForceIndex struct {
	mu        sync.RWMutex
	dimension int
	vectors   map[uint64][]float32
}

func NewBruteForceIndex(dimension int) *BruteForceIndex {
	return &BruteForceIndex{
		dimension: dimension,
		vectors:   make(map[uint64][]float32),
	}
}

func (b *BruteForceIndex) Dimension() int {
	return b.dimension
}

func (b *BruteForceIndex) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.vectors)
}

func (b *BruteForceIndex) Add(id uint64, vector []float32) error {
	if len(vector) != b.dimension {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", b.dimension, len(vector))
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.vectors[id]; exists {
		return fmt.Errorf("vector with id %d already exists", id)
	}

	v := make([]float32, len(vector))
	copy(v, vector)
	b.vectors[id] = v
	return nil
}

func (b *BruteForceIndex) Remove(id uint64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.vectors[id]; !exists {
		return false
	}
	delete(b.vectors, id)
	return true
}

func (b *BruteForceIndex) Search(query []float32, k int) []SearchResult {
	if len(query) != b.dimension {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	type scored struct {
		id    uint64
		score float32
	}

	scoredVectors := make([]scored, 0, len(b.vectors))
	for id, vec := range b.vectors {
		scoredVectors = append(scoredVectors, scored{id: id, score: cosineSimilarity(query, vec)})
	}

	sort.Slice(scoredVectors, func(i, j int) bool {
		return scoredVectors[i].score > scoredVectors[j].score
	})

	results := make([]SearchResult, 0, k)
	for i := 0; i < k && i < len(scoredVectors); i++ {
		results = append(results, SearchResult{
			ID:         scoredVectors[i].id,
			Similarity: scoredVectors[i].score,
		})
	}

	return results
}

func (b *BruteForceIndex) Save(w io.Writer) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Write header
	header := struct {
		Dimension int32
		Count     int32
	}{
		Dimension: int32(b.dimension),
		Count:     int32(len(b.vectors)),
	}

	if err := binary.Write(w, binary.LittleEndian, &header); err != nil {
		return err
	}

	// Write each vector
	for id, vec := range b.vectors {
		if err := binary.Write(w, binary.LittleEndian, id); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, vec); err != nil {
			return err
		}
	}

	return nil
}

func (b *BruteForceIndex) Load(r io.Reader) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var header struct {
		Dimension int32
		Count     int32
	}

	if err := binary.Read(r, binary.LittleEndian, &header); err != nil {
		return err
	}

	b.dimension = int(header.Dimension)
	b.vectors = make(map[uint64][]float32, header.Count)

	for i := 0; i < int(header.Count); i++ {
		var id uint64
		if err := binary.Read(r, binary.LittleEndian, &id); err != nil {
			return err
		}

		vec := make([]float32, b.dimension)
		if err := binary.Read(r, binary.LittleEndian, vec); err != nil {
			return err
		}

		b.vectors[id] = vec
	}

	return nil
}

// GetAllVectors returns all vectors in the index
func (b *BruteForceIndex) GetAllVectors() map[uint64][]float32 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make(map[uint64][]float32, len(b.vectors))
	for id, vec := range b.vectors {
		copied := make([]float32, len(vec))
		copy(copied, vec)
		result[id] = copied
	}
	return result
}

// Rebuild rebuilds the index (no-op for brute force)
func (b *BruteForceIndex) Rebuild() error {
	return nil // Brute force doesn't need rebuild
}

// ValidateIntegrity checks if the index is valid
func (b *BruteForceIndex) ValidateIntegrity() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for id, vec := range b.vectors {
		if len(vec) != b.dimension {
			return fmt.Errorf("vector %d has wrong dimension: expected %d, got %d", id, b.dimension, len(vec))
		}
	}
	return nil
}
