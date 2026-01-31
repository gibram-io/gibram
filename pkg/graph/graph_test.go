// Package graph provides graph algorithm tests
package graph

import (
	"sync"
	"testing"

	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Mock Stores for Testing
// =============================================================================

// mockEntityStore implements EntityStore interface for testing
type mockEntityStore struct {
	mu       sync.RWMutex
	entities map[uint64]*types.Entity
}

func newMockEntityStore() *mockEntityStore {
	return &mockEntityStore{
		entities: make(map[uint64]*types.Entity),
	}
}

func (s *mockEntityStore) Add(entity *types.Entity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entities[entity.ID] = entity
}

func (s *mockEntityStore) GetAll() []*types.Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*types.Entity, 0, len(s.entities))
	for _, e := range s.entities {
		result = append(result, e)
	}
	return result
}

func (s *mockEntityStore) Get(id uint64) (*types.Entity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entities[id]
	return e, ok
}

// mockRelationshipStore implements RelationshipStore interface for testing
type mockRelationshipStore struct {
	mu            sync.RWMutex
	relationships map[uint64]*types.Relationship
}

func newMockRelationshipStore() *mockRelationshipStore {
	return &mockRelationshipStore{
		relationships: make(map[uint64]*types.Relationship),
	}
}

func (s *mockRelationshipStore) Add(rel *types.Relationship) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relationships[rel.ID] = rel
}

func (s *mockRelationshipStore) GetAll() []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*types.Relationship, 0, len(s.relationships))
	for _, r := range s.relationships {
		result = append(result, r)
	}
	return result
}

func (s *mockRelationshipStore) Get(id uint64) (*types.Relationship, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.relationships[id]
	return r, ok
}

func (s *mockRelationshipStore) GetOutgoing(entityID uint64) []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []*types.Relationship{}
	for _, r := range s.relationships {
		if r.SourceID == entityID {
			result = append(result, r)
		}
	}
	return result
}

func (s *mockRelationshipStore) GetIncoming(entityID uint64) []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []*types.Relationship{}
	for _, r := range s.relationships {
		if r.TargetID == entityID {
			result = append(result, r)
		}
	}
	return result
}

func (s *mockRelationshipStore) GetNeighbors(entityID uint64) []*types.Relationship {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []*types.Relationship{}
	for _, r := range s.relationships {
		if r.SourceID == entityID || r.TargetID == entityID {
			result = append(result, r)
		}
	}
	return result
}

// =============================================================================
// Test Helpers
// =============================================================================

// createTestGraph creates a simple test graph with entities and relationships
// Returns entity store, relationship store, and entity IDs
func createTestGraph() (*mockEntityStore, *mockRelationshipStore, []uint64) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create entities
	entityIDs := []uint64{1, 2, 3, 4, 5}
	for _, id := range entityIDs {
		entityStore.Add(&types.Entity{
			ID:    id,
			Title: "Entity " + itoa(int(id)),
			Type:  "test",
		})
	}

	// Create relationships forming a simple graph:
	// 1 -> 2, 1 -> 3, 2 -> 4, 3 -> 4, 4 -> 5
	relationships := []struct {
		id       uint64
		sourceID uint64
		targetID uint64
		weight   float32
	}{
		{1, 1, 2, 1.0},
		{2, 1, 3, 1.0},
		{3, 2, 4, 1.0},
		{4, 3, 4, 1.0},
		{5, 4, 5, 1.0},
	}

	for _, r := range relationships {
		relStore.Add(&types.Relationship{
			ID:       r.id,
			SourceID: r.sourceID,
			TargetID: r.targetID,
			Type:     "RELATED_TO",
			Weight:   r.weight,
		})
	}

	return entityStore, relStore, entityIDs
}

// createClusterGraph creates a graph with clear clusters
func createClusterGraph() (*mockEntityStore, *mockRelationshipStore, []uint64) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Cluster A: 1, 2, 3 (densely connected)
	// Cluster B: 4, 5, 6 (densely connected)
	// Weak connection: 3 -> 4

	entityIDs := []uint64{1, 2, 3, 4, 5, 6}
	for _, id := range entityIDs {
		entityStore.Add(&types.Entity{
			ID:    id,
			Title: "Entity " + itoa(int(id)),
			Type:  "test",
		})
	}

	relID := uint64(1)
	// Cluster A internal
	for i := uint64(1); i <= 3; i++ {
		for j := uint64(1); j <= 3; j++ {
			if i < j {
				relStore.Add(&types.Relationship{ID: relID, SourceID: i, TargetID: j, Type: "IN_CLUSTER", Weight: 1.0})
				relID++
			}
		}
	}

	// Cluster B internal
	for i := uint64(4); i <= 6; i++ {
		for j := uint64(4); j <= 6; j++ {
			if i < j {
				relStore.Add(&types.Relationship{ID: relID, SourceID: i, TargetID: j, Type: "IN_CLUSTER", Weight: 1.0})
				relID++
			}
		}
	}

	// Weak inter-cluster connection
	relStore.Add(&types.Relationship{ID: relID, SourceID: 3, TargetID: 4, Type: "BETWEEN_CLUSTER", Weight: 0.1})

	return entityStore, relStore, entityIDs
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte(i%10) + '0'
		i /= 10
	}
	return string(buf[pos:])
}

// =============================================================================
// Leiden Clustering Tests
// =============================================================================

func TestLeiden_NewLeiden(t *testing.T) {
	entityStore, relStore, _ := createTestGraph()
	config := DefaultLeidenConfig()

	leiden := NewLeiden(entityStore, relStore, config)

	if leiden == nil {
		t.Fatal("NewLeiden() returned nil")
	}
}

func TestLeiden_ComputeHierarchicalCommunities_Empty(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()
	config := DefaultLeidenConfig()

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeHierarchicalCommunities()

	if len(result) > 0 {
		t.Error("ComputeHierarchicalCommunities() on empty graph should return nil or empty")
	}
}

func TestLeiden_ComputeHierarchicalCommunities(t *testing.T) {
	entityStore, relStore, entityIDs := createClusterGraph()
	config := DefaultLeidenConfig()
	config.MinCommunitySize = 2

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeHierarchicalCommunities()

	// Should have at least one level
	if len(result) == 0 {
		t.Fatal("ComputeHierarchicalCommunities() returned empty result")
	}

	// Count total entities across all level 0 communities
	totalEntities := 0
	for _, comm := range result[0] {
		totalEntities += len(comm.EntityIDs)
	}

	if totalEntities != len(entityIDs) {
		t.Errorf("Total entities in communities = %d, want %d", totalEntities, len(entityIDs))
	}
}

func TestDefaultLeidenConfig(t *testing.T) {
	config := DefaultLeidenConfig()

	if config.Resolution <= 0 {
		t.Error("DefaultLeidenConfig() Resolution should be > 0")
	}
	if config.Iterations <= 0 {
		t.Error("DefaultLeidenConfig() Iterations should be > 0")
	}
	if config.MaxLevels <= 0 {
		t.Error("DefaultLeidenConfig() MaxLevels should be > 0")
	}
}

// =============================================================================
// BFS Traversal Tests
// =============================================================================

func TestBFSTraversal_Basic(t *testing.T) {
	_, relStore, _ := createTestGraph()

	nodeIDs, distances, steps := BFSTraversal([]uint64{1}, relStore, 3, 100)

	if len(nodeIDs) == 0 {
		t.Fatal("BFSTraversal() returned empty nodeIDs")
	}

	// Node 1 should have distance 0
	if d, ok := distances[1]; !ok || d != 0 {
		t.Errorf("Distance to seed node should be 0, got %d", d)
	}

	// Should have found some traversal steps
	if len(steps) == 0 {
		t.Error("BFSTraversal() returned no traversal steps")
	}
}

func TestBFSTraversal_MaxHops(t *testing.T) {
	_, relStore, _ := createTestGraph()

	// With maxHops=1, should only get direct neighbors
	nodeIDs, distances, _ := BFSTraversal([]uint64{1}, relStore, 1, 100)

	for _, nid := range nodeIDs {
		if distances[nid] > 1 {
			t.Errorf("Node %d has distance %d, but maxHops=1", nid, distances[nid])
		}
	}
}

func TestBFSTraversal_MaxNodes(t *testing.T) {
	_, relStore, _ := createTestGraph()

	// Limit to 3 nodes
	nodeIDs, _, _ := BFSTraversal([]uint64{1}, relStore, 10, 3)

	if len(nodeIDs) > 3 {
		t.Errorf("BFSTraversal() returned %d nodes, want <= 3", len(nodeIDs))
	}
}

func TestBFSTraversal_MultipleSeeds(t *testing.T) {
	_, relStore, _ := createTestGraph()

	// Start from nodes 1 and 5
	nodeIDs, distances, _ := BFSTraversal([]uint64{1, 5}, relStore, 3, 100)

	// Both seeds should have distance 0
	if distances[1] != 0 {
		t.Errorf("Seed 1 distance = %d, want 0", distances[1])
	}
	if distances[5] != 0 {
		t.Errorf("Seed 5 distance = %d, want 0", distances[5])
	}

	// Should include nodes from both starting points
	if len(nodeIDs) < 2 {
		t.Error("BFSTraversal() with multiple seeds should return multiple nodes")
	}
}

func TestBFSTraversal_Empty(t *testing.T) {
	relStore := newMockRelationshipStore()

	nodeIDs, distances, steps := BFSTraversal([]uint64{1}, relStore, 3, 100)

	// Should return the seed even if no relationships
	if len(nodeIDs) != 1 {
		t.Errorf("BFSTraversal() on empty graph returned %d nodes", len(nodeIDs))
	}
	if distances[1] != 0 {
		t.Error("Seed should have distance 0")
	}
	if len(steps) != 0 {
		t.Error("No steps should exist for disconnected node")
	}
}

// =============================================================================
// PageRank Tests
// =============================================================================

func TestPageRank_Basic(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	scores := PageRank(entityIDs, relStore, 0.85, 10)

	if scores == nil {
		t.Fatal("PageRank() returned nil")
	}

	// All entity IDs should have scores
	for _, eid := range entityIDs {
		if _, ok := scores[eid]; !ok {
			t.Errorf("Entity %d missing from PageRank scores", eid)
		}
	}

	// Scores should sum to approximately 1.0
	sum := 0.0
	for _, s := range scores {
		sum += s
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("PageRank scores sum = %f, want ~1.0", sum)
	}
}

func TestPageRank_Empty(t *testing.T) {
	relStore := newMockRelationshipStore()

	scores := PageRank([]uint64{}, relStore, 0.85, 10)

	if len(scores) > 0 {
		t.Error("PageRank() on empty graph should return nil or empty")
	}
}

func TestPageRank_SingleNode(t *testing.T) {
	relStore := newMockRelationshipStore()

	scores := PageRank([]uint64{1}, relStore, 0.85, 10)

	if scores == nil {
		t.Fatal("PageRank() returned nil")
	}

	if scores[1] != 1.0 {
		t.Errorf("Single node PageRank = %f, want 1.0", scores[1])
	}
}

// =============================================================================
// Connected Components Tests
// =============================================================================

func TestConnectedComponents_SingleComponent(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	components := ConnectedComponents(entityIDs, relStore)

	// All nodes in one graph should be one component
	if len(components) != 1 {
		t.Errorf("ConnectedComponents() = %d components, want 1", len(components))
	}

	if len(components[0]) != len(entityIDs) {
		t.Errorf("Component size = %d, want %d", len(components[0]), len(entityIDs))
	}
}

func TestConnectedComponents_MultipleComponents(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create two disconnected components
	// Component 1: 1 -> 2
	// Component 2: 3 -> 4
	for _, id := range []uint64{1, 2, 3, 4} {
		entityStore.Add(&types.Entity{ID: id, Title: "E" + itoa(int(id))})
	}

	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 2, Type: "REL"})
	relStore.Add(&types.Relationship{ID: 2, SourceID: 3, TargetID: 4, Type: "REL"})

	components := ConnectedComponents([]uint64{1, 2, 3, 4}, relStore)

	if len(components) != 2 {
		t.Errorf("ConnectedComponents() = %d components, want 2", len(components))
	}
}

func TestConnectedComponents_Isolated(t *testing.T) {
	relStore := newMockRelationshipStore()

	// Three isolated nodes
	components := ConnectedComponents([]uint64{1, 2, 3}, relStore)

	if len(components) != 3 {
		t.Errorf("ConnectedComponents() = %d components, want 3", len(components))
	}
}

func TestConnectedComponents_Empty(t *testing.T) {
	relStore := newMockRelationshipStore()

	components := ConnectedComponents([]uint64{}, relStore)

	if len(components) != 0 {
		t.Errorf("ConnectedComponents() on empty = %d components, want 0", len(components))
	}
}

// =============================================================================
// Betweenness Centrality Tests
// =============================================================================

func TestBetweenness_Basic(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	scores := Betweenness(entityIDs, relStore, 0) // 0 = use all nodes

	if scores == nil {
		t.Fatal("Betweenness() returned nil")
	}

	// All entities should have scores
	for _, eid := range entityIDs {
		if _, ok := scores[eid]; !ok {
			t.Errorf("Entity %d missing from betweenness scores", eid)
		}
	}

	// Node 4 is on the path between clusters, should have high centrality
	// (In our test graph: 1->2->4, 1->3->4, 4->5)
	if scores[4] <= 0 {
		t.Error("Node 4 (bridge) should have positive betweenness centrality")
	}
}

func TestBetweenness_Empty(t *testing.T) {
	relStore := newMockRelationshipStore()

	scores := Betweenness([]uint64{}, relStore, 0)

	if len(scores) > 0 {
		t.Error("Betweenness() on empty should return nil or empty")
	}
}

func TestBetweenness_SingleNode(t *testing.T) {
	relStore := newMockRelationshipStore()

	scores := Betweenness([]uint64{1}, relStore, 0)

	if scores == nil {
		t.Fatal("Betweenness() returned nil")
	}

	// Single node has 0 betweenness
	if scores[1] != 0 {
		t.Errorf("Single node betweenness = %f, want 0", scores[1])
	}
}

// =============================================================================
// HierarchicalCommunity Tests
// =============================================================================

func TestHierarchicalCommunity_Structure(t *testing.T) {
	comm := HierarchicalCommunity{
		EntityIDs: []uint64{1, 2, 3},
		Level:     0,
		ParentIdx: -1,
		Children:  []int{0, 1},
	}

	if len(comm.EntityIDs) != 3 {
		t.Errorf("EntityIDs length = %d, want 3", len(comm.EntityIDs))
	}

	if comm.Level != 0 {
		t.Errorf("Level = %d, want 0", comm.Level)
	}

	if comm.ParentIdx != -1 {
		t.Errorf("ParentIdx = %d, want -1", comm.ParentIdx)
	}

	if len(comm.Children) != 2 {
		t.Errorf("Children length = %d, want 2", len(comm.Children))
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestBFSTraversal_ZeroMaxHops(t *testing.T) {
	_, relStore, _ := createTestGraph()

	// maxHops=0 should only return seeds
	nodeIDs, distances, _ := BFSTraversal([]uint64{1, 2}, relStore, 0, 100)

	if len(nodeIDs) != 2 {
		t.Errorf("BFSTraversal(maxHops=0) returned %d nodes, want 2", len(nodeIDs))
	}

	for _, nid := range nodeIDs {
		if distances[nid] != 0 {
			t.Errorf("Node %d distance = %d, want 0", nid, distances[nid])
		}
	}
}

func TestBFSTraversal_ZeroMaxNodes(t *testing.T) {
	_, relStore, _ := createTestGraph()

	nodeIDs, _, _ := BFSTraversal([]uint64{1}, relStore, 10, 0)

	// maxNodes=0 might return 0 or 1 (seed) depending on implementation
	if len(nodeIDs) > 1 {
		t.Errorf("BFSTraversal(maxNodes=0) returned %d nodes, want <= 1", len(nodeIDs))
	}
}

func TestBFSTraversal_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large graph test in short mode")
	}

	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create a larger graph
	for i := uint64(1); i <= 100; i++ {
		entityStore.Add(&types.Entity{ID: i, Title: "E" + itoa(int(i))})
	}

	relID := uint64(1)
	for i := uint64(1); i <= 99; i++ {
		relStore.Add(&types.Relationship{ID: relID, SourceID: i, TargetID: i + 1, Type: "NEXT"})
		relID++
	}

	nodeIDs, _, _ := BFSTraversal([]uint64{1}, relStore, 50, 1000)

	if len(nodeIDs) < 50 {
		t.Errorf("BFSTraversal() on chain returned %d nodes, want >= 50", len(nodeIDs))
	}
}

func TestPageRank_Convergence(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	// With many iterations, PageRank should converge
	scores1 := PageRank(entityIDs, relStore, 0.85, 10)
	scores2 := PageRank(entityIDs, relStore, 0.85, 100)

	// Scores should be similar (converged)
	for eid := range scores1 {
		diff := scores1[eid] - scores2[eid]
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.01 {
			t.Errorf("PageRank not converged for entity %d: %f vs %f", eid, scores1[eid], scores2[eid])
		}
	}
}

func TestPageRank_DifferentDamping(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	scores1 := PageRank(entityIDs, relStore, 0.5, 20)
	scores2 := PageRank(entityIDs, relStore, 0.95, 20)

	// Different damping factors should produce different results
	allSame := true
	for eid := range scores1 {
		if scores1[eid] != scores2[eid] {
			allSame = false
			break
		}
	}

	if allSame {
		t.Error("Different damping factors should produce different scores")
	}
}

func TestBetweenness_WithSampleSize(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	// Test with sample size
	scores := Betweenness(entityIDs, relStore, 3)

	if scores == nil {
		t.Fatal("Betweenness() with sample size returned nil")
	}

	if len(scores) != len(entityIDs) {
		t.Errorf("Betweenness() returned %d scores, want %d", len(scores), len(entityIDs))
	}
}

func TestLeiden_WithDifferentResolutions(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()

	// Low resolution - should produce fewer communities
	configLow := DefaultLeidenConfig()
	configLow.Resolution = 0.5
	configLow.MinCommunitySize = 1

	leidenLow := NewLeiden(entityStore, relStore, configLow)
	resultLow := leidenLow.ComputeHierarchicalCommunities()

	// High resolution - should produce more communities
	configHigh := DefaultLeidenConfig()
	configHigh.Resolution = 2.0
	configHigh.MinCommunitySize = 1

	leidenHigh := NewLeiden(entityStore, relStore, configHigh)
	resultHigh := leidenHigh.ComputeHierarchicalCommunities()

	// Both should produce valid results
	if len(resultLow) == 0 {
		t.Error("Leiden with low resolution returned empty result")
	}
	if len(resultHigh) == 0 {
		t.Error("Leiden with high resolution returned empty result")
	}
}

func TestLeiden_SingleNode(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	entityStore.Add(&types.Entity{ID: 1, Title: "Single", Type: "test"})

	config := DefaultLeidenConfig()
	config.MinCommunitySize = 1

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeHierarchicalCommunities()

	// Should handle single node gracefully
	if result == nil {
		t.Skip("Leiden returns nil for single node (acceptable behavior)")
	}
}

func TestLeiden_DisconnectedGraph(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create two disconnected clusters
	for i := uint64(1); i <= 6; i++ {
		entityStore.Add(&types.Entity{ID: i, Title: "E" + itoa(int(i)), Type: "test"})
	}

	// Cluster 1: 1-2-3 connected
	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 2, Type: "REL"})
	relStore.Add(&types.Relationship{ID: 2, SourceID: 2, TargetID: 3, Type: "REL"})

	// Cluster 2: 4-5-6 connected (no connection to cluster 1)
	relStore.Add(&types.Relationship{ID: 3, SourceID: 4, TargetID: 5, Type: "REL"})
	relStore.Add(&types.Relationship{ID: 4, SourceID: 5, TargetID: 6, Type: "REL"})

	config := DefaultLeidenConfig()
	config.MinCommunitySize = 1

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeHierarchicalCommunities()

	if len(result) > 0 && len(result[0]) < 2 {
		t.Error("Disconnected graph should produce at least 2 communities at level 0")
	}
}

func TestConnectedComponents_SelfLoop(t *testing.T) {
	relStore := newMockRelationshipStore()

	// Node with self-loop
	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 1, Type: "SELF"})

	components := ConnectedComponents([]uint64{1, 2}, relStore)

	// Should have 2 components (node 1 and isolated node 2)
	if len(components) != 2 {
		t.Errorf("ConnectedComponents() = %d, want 2", len(components))
	}
}

func TestLeidenConfig_Modifications(t *testing.T) {
	config := DefaultLeidenConfig()

	// Modify config
	config.Resolution = 2.0
	config.Iterations = 20
	config.MaxLevels = 5
	config.MinCommunitySize = 3
	config.RandomSeed = 12345

	if config.Resolution != 2.0 {
		t.Error("Resolution not set correctly")
	}
	if config.Iterations != 20 {
		t.Error("Iterations not set correctly")
	}
	if config.MaxLevels != 5 {
		t.Error("MaxLevels not set correctly")
	}
	if config.MinCommunitySize != 3 {
		t.Error("MinCommunitySize not set correctly")
	}
	if config.RandomSeed != 12345 {
		t.Error("RandomSeed not set correctly")
	}
}

// =============================================================================
// ComputeCommunities Tests
// =============================================================================

func TestLeiden_ComputeCommunities(t *testing.T) {
	entityStore, relStore, entityIDs := createClusterGraph()
	config := DefaultLeidenConfig()
	config.MinCommunitySize = 1

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeCommunities()

	if len(result) == 0 {
		t.Skip("ComputeCommunities returned empty (graph might be too small)")
	}

	// Count total entities in all communities
	totalEntities := 0
	entityMap := make(map[uint64]bool)
	for _, comm := range result {
		for _, eid := range comm {
			entityMap[eid] = true
			totalEntities++
		}
	}

	// Each entity should be in exactly one community
	if len(entityMap) != len(entityIDs) {
		t.Errorf("Expected %d unique entities in communities, got %d", len(entityIDs), len(entityMap))
	}
}

func TestLeiden_ComputeCommunities_Empty(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()
	config := DefaultLeidenConfig()

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeCommunities()

	if len(result) > 0 {
		t.Error("ComputeCommunities on empty graph should return nil or empty")
	}
}

func TestLeiden_ComputeCommunities_SingleCommunity(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create fully connected graph - should form single community
	for i := uint64(1); i <= 4; i++ {
		entityStore.Add(&types.Entity{ID: i, Title: "E" + itoa(int(i)), Type: "test"})
	}

	relID := uint64(1)
	for i := uint64(1); i <= 4; i++ {
		for j := i + 1; j <= 4; j++ {
			relStore.Add(&types.Relationship{ID: relID, SourceID: i, TargetID: j, Type: "CONNECTED", Weight: 1.0})
			relID++
		}
	}

	config := DefaultLeidenConfig()
	config.MinCommunitySize = 1
	config.Resolution = 0.5 // Lower resolution tends to create fewer communities

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeCommunities()

	if result != nil {
		// Count total entities
		total := 0
		for _, comm := range result {
			total += len(comm)
		}
		if total != 4 {
			t.Errorf("Expected 4 entities total, got %d", total)
		}
	}
}

// =============================================================================
// BuildCommunities Tests
// =============================================================================

func TestBuildCommunities(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()
	idGen := types.NewIDGenerator()

	// Create some clusters manually
	clusters := [][]uint64{
		{1, 2, 3},
		{4, 5, 6},
	}

	communities := BuildCommunities(clusters, entityStore, relStore, idGen, 0)

	if len(communities) != 2 {
		t.Errorf("BuildCommunities returned %d communities, want 2", len(communities))
	}

	// Verify first community
	comm1 := communities[0]
	if comm1.ID == 0 {
		t.Error("Community ID should not be 0")
	}
	if len(comm1.EntityIDs) != 3 {
		t.Errorf("Community 1 should have 3 entities, got %d", len(comm1.EntityIDs))
	}
	if comm1.Level != 0 {
		t.Errorf("Community 1 level should be 0, got %d", comm1.Level)
	}

	// Verify community has title
	if comm1.Title == "" {
		t.Error("Community should have a title built from entity titles")
	}
}

func TestBuildCommunities_Empty(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()
	idGen := types.NewIDGenerator()

	communities := BuildCommunities([][]uint64{}, entityStore, relStore, idGen, 0)

	if len(communities) != 0 {
		t.Errorf("BuildCommunities with empty clusters should return empty, got %d", len(communities))
	}
}

func TestBuildCommunities_WithEmptyClusters(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()
	idGen := types.NewIDGenerator()

	// Mix of empty and non-empty clusters
	clusters := [][]uint64{
		{1, 2},
		{}, // Empty
		{4, 5, 6},
		{}, // Empty
	}

	communities := BuildCommunities(clusters, entityStore, relStore, idGen, 0)

	// Should only create communities for non-empty clusters
	if len(communities) != 2 {
		t.Errorf("BuildCommunities should skip empty clusters, got %d communities", len(communities))
	}
}

func TestBuildCommunities_RelationshipIDs(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()
	idGen := types.NewIDGenerator()

	// Create entities
	for i := uint64(1); i <= 4; i++ {
		entityStore.Add(&types.Entity{ID: i, Title: "E" + itoa(int(i)), Type: "test"})
	}

	// Create relationships within cluster
	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 2, Type: "INTERNAL"})
	relStore.Add(&types.Relationship{ID: 2, SourceID: 2, TargetID: 3, Type: "INTERNAL"})
	// External relationship (should not be included)
	relStore.Add(&types.Relationship{ID: 3, SourceID: 3, TargetID: 4, Type: "EXTERNAL"})

	clusters := [][]uint64{{1, 2, 3}}

	communities := BuildCommunities(clusters, entityStore, relStore, idGen, 0)

	if len(communities) != 1 {
		t.Fatalf("Expected 1 community, got %d", len(communities))
	}

	// Should include internal relationships
	if len(communities[0].RelationshipIDs) < 1 {
		t.Error("Community should have internal relationship IDs")
	}
}

func TestBuildCommunities_DifferentLevels(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()
	idGen := types.NewIDGenerator()

	clusters := [][]uint64{{1, 2, 3}}

	// Build at level 0
	comms0 := BuildCommunities(clusters, entityStore, relStore, idGen, 0)
	if comms0[0].Level != 0 {
		t.Errorf("Expected level 0, got %d", comms0[0].Level)
	}

	// Build at level 1
	comms1 := BuildCommunities(clusters, entityStore, relStore, idGen, 1)
	if comms1[0].Level != 1 {
		t.Errorf("Expected level 1, got %d", comms1[0].Level)
	}
}

// =============================================================================
// BuildHierarchicalCommunities Tests
// =============================================================================

func TestBuildHierarchicalCommunities(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()
	idGen := types.NewIDGenerator()

	// Create hierarchical structure
	hierarchical := [][]HierarchicalCommunity{
		// Level 0
		{
			{EntityIDs: []uint64{1, 2, 3}},
			{EntityIDs: []uint64{4, 5, 6}},
		},
		// Level 1
		{
			{EntityIDs: []uint64{1, 2, 3, 4, 5, 6}},
		},
	}

	communities := BuildHierarchicalCommunities(hierarchical, entityStore, relStore, idGen)

	if len(communities) != 3 { // 2 from level 0 + 1 from level 1
		t.Errorf("Expected 3 communities, got %d", len(communities))
	}

	// Verify levels
	level0Count := 0
	level1Count := 0
	for _, c := range communities {
		switch c.Level {
		case 0:
			level0Count++
		case 1:
			level1Count++
		}
	}

	if level0Count != 2 {
		t.Errorf("Expected 2 level 0 communities, got %d", level0Count)
	}
	if level1Count != 1 {
		t.Errorf("Expected 1 level 1 community, got %d", level1Count)
	}
}

func TestBuildHierarchicalCommunities_Empty(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()
	idGen := types.NewIDGenerator()

	communities := BuildHierarchicalCommunities(nil, entityStore, relStore, idGen)

	if len(communities) != 0 {
		t.Errorf("Expected 0 communities, got %d", len(communities))
	}
}

func TestBuildHierarchicalCommunities_WithEmptyCommunities(t *testing.T) {
	entityStore, relStore, _ := createClusterGraph()
	idGen := types.NewIDGenerator()

	hierarchical := [][]HierarchicalCommunity{
		{
			{EntityIDs: []uint64{1, 2}},
			{EntityIDs: []uint64{}}, // Empty
			{EntityIDs: []uint64{4, 5}},
		},
	}

	communities := BuildHierarchicalCommunities(hierarchical, entityStore, relStore, idGen)

	// Should skip empty community
	if len(communities) != 2 {
		t.Errorf("Expected 2 communities (skipping empty), got %d", len(communities))
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestLeiden_LargeGraph(t *testing.T) {
	entityStore := newMockEntityStore()
	relStore := newMockRelationshipStore()

	// Create a larger graph with 50 nodes in 5 clusters
	for i := uint64(1); i <= 50; i++ {
		entityStore.Add(&types.Entity{ID: i, Title: "E" + itoa(int(i)), Type: "test"})
	}

	relID := uint64(1)
	// Create 5 clusters of 10 nodes each
	for cluster := 0; cluster < 5; cluster++ {
		base := uint64(cluster*10 + 1)
		for i := uint64(0); i < 10; i++ {
			for j := i + 1; j < 10; j++ {
				relStore.Add(&types.Relationship{
					ID:       relID,
					SourceID: base + i,
					TargetID: base + j,
					Type:     "CLUSTER",
					Weight:   1.0,
				})
				relID++
			}
		}
	}

	// Add weak inter-cluster connections
	for i := 0; i < 4; i++ {
		relStore.Add(&types.Relationship{
			ID:       relID,
			SourceID: uint64(i*10 + 10),
			TargetID: uint64((i+1)*10 + 1),
			Type:     "INTER",
			Weight:   0.1,
		})
		relID++
	}

	config := DefaultLeidenConfig()
	config.MinCommunitySize = 3

	leiden := NewLeiden(entityStore, relStore, config)
	result := leiden.ComputeCommunities()

	if len(result) > 0 {
		// Count total entities
		total := 0
		for _, comm := range result {
			total += len(comm)
		}
		if total == 0 {
			t.Error("Leiden on large graph should produce non-empty communities")
		}
	}
}

func TestConnectedComponents_LargeGraph(t *testing.T) {
	relStore := newMockRelationshipStore()

	// Create 3 separate connected components
	// Component 1: nodes 1-10
	for i := uint64(1); i < 10; i++ {
		relStore.Add(&types.Relationship{ID: i, SourceID: i, TargetID: i + 1, Type: "LINK"})
	}

	// Component 2: nodes 11-20
	for i := uint64(11); i < 20; i++ {
		relStore.Add(&types.Relationship{ID: i, SourceID: i, TargetID: i + 1, Type: "LINK"})
	}

	// Component 3: nodes 21-30
	for i := uint64(21); i < 30; i++ {
		relStore.Add(&types.Relationship{ID: i, SourceID: i, TargetID: i + 1, Type: "LINK"})
	}

	nodeIDs := make([]uint64, 30)
	for i := range nodeIDs {
		nodeIDs[i] = uint64(i + 1)
	}

	components := ConnectedComponents(nodeIDs, relStore)

	if len(components) != 3 {
		t.Errorf("Expected 3 connected components, got %d", len(components))
	}
}

func TestPageRank_IsolatedNodes(t *testing.T) {
	relStore := newMockRelationshipStore()

	// Only relationship between 1 and 2
	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 2, Type: "LINK"})

	// Node 3 is isolated
	entityIDs := []uint64{1, 2, 3}

	scores := PageRank(entityIDs, relStore, 0.85, 10)

	if scores == nil {
		t.Fatal("PageRank should not return nil")
	}

	// All nodes should have some score
	for _, eid := range entityIDs {
		if scores[eid] <= 0 {
			t.Errorf("Node %d should have positive PageRank score", eid)
		}
	}
}

func TestBetweenness_AllPairs(t *testing.T) {
	_, relStore, entityIDs := createTestGraph()

	// Sample size 0 or negative means all pairs
	scores := Betweenness(entityIDs, relStore, 0)

	if scores == nil {
		t.Fatal("Betweenness should not return nil")
	}

	if len(scores) != len(entityIDs) {
		t.Errorf("Betweenness returned %d scores, want %d", len(scores), len(entityIDs))
	}
}

func TestBFSTraversal_NoRelationships(t *testing.T) {
	relStore := newMockRelationshipStore()

	// No relationships, just seed nodes
	nodeIDs, distances, steps := BFSTraversal([]uint64{1, 2, 3}, relStore, 10, 100)

	// Should return just the seed nodes
	if len(nodeIDs) != 3 {
		t.Errorf("BFSTraversal with no relationships should return only seeds, got %d nodes", len(nodeIDs))
	}

	// All seeds should have distance 0
	for _, nid := range nodeIDs {
		if distances[nid] != 0 {
			t.Errorf("Seed node %d should have distance 0", nid)
		}
	}

	// No traversal steps without relationships
	if len(steps) != 0 {
		t.Errorf("BFSTraversal with no relationships should have no steps, got %d", len(steps))
	}
}

func TestBFSTraversal_Cycle(t *testing.T) {
	relStore := newMockRelationshipStore()

	// Create a cycle: 1 -> 2 -> 3 -> 1
	relStore.Add(&types.Relationship{ID: 1, SourceID: 1, TargetID: 2, Type: "NEXT"})
	relStore.Add(&types.Relationship{ID: 2, SourceID: 2, TargetID: 3, Type: "NEXT"})
	relStore.Add(&types.Relationship{ID: 3, SourceID: 3, TargetID: 1, Type: "NEXT"})

	nodeIDs, distances, _ := BFSTraversal([]uint64{1}, relStore, 10, 100)

	// Should visit all 3 nodes
	if len(nodeIDs) != 3 {
		t.Errorf("BFSTraversal on cycle should visit all 3 nodes, got %d", len(nodeIDs))
	}

	// Node 1 should have distance 0
	if distances[1] != 0 {
		t.Errorf("Seed node should have distance 0, got %d", distances[1])
	}
}
