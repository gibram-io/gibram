// Package graph provides graph algorithms including Leiden clustering
package graph

import (
	"math"
	"math/rand"
	"sort"

	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Store Interfaces - for compatibility with adapters
// =============================================================================

// EntityStore interface for accessing entities
type EntityStore interface {
	GetAll() []*types.Entity
	Get(id uint64) (*types.Entity, bool)
}

// RelationshipStore interface for accessing relationships
type RelationshipStore interface {
	GetAll() []*types.Relationship
	Get(id uint64) (*types.Relationship, bool)
	GetOutgoing(entityID uint64) []*types.Relationship
	GetIncoming(entityID uint64) []*types.Relationship
	GetNeighbors(entityID uint64) []*types.Relationship
}

// =============================================================================
// Leiden Clustering Algorithm
// =============================================================================

// LeidenConfig contains parameters for Leiden clustering
type LeidenConfig struct {
	Resolution float64 // resolution parameter (higher = more communities)
	Iterations int     // max iterations per level
	MinDelta   float64 // min modularity improvement to continue
	RandomSeed int64   // random seed for reproducibility

	// Hierarchical Leiden settings
	MaxLevels        int     // max hierarchy levels (default 5)
	MinCommunitySize int     // min entities for community to be split further
	LevelResolution  float64 // resolution multiplier per level (e.g., 0.5 = finer at deeper levels)
}

func DefaultLeidenConfig() LeidenConfig {
	return LeidenConfig{
		Resolution:       1.0,
		Iterations:       10,
		MinDelta:         0.0001,
		RandomSeed:       42,
		MaxLevels:        5,
		MinCommunitySize: 3,
		LevelResolution:  0.7, // Resolution decreases at deeper levels for finer communities
	}
}

// Leiden implements the Leiden community detection algorithm
type Leiden struct {
	config        LeidenConfig
	entities      EntityStore
	relationships RelationshipStore

	// Internal state
	nodeToComm   map[uint64]int                // entity ID -> community ID
	commNodes    map[int][]uint64              // community ID -> []entity IDs
	adjWeights   map[uint64]map[uint64]float64 // adjacency with weights
	nodeStrength map[uint64]float64            // sum of edge weights per node
	totalWeight  float64                       // total edge weight in graph
	rng          *rand.Rand
}

func NewLeiden(entities EntityStore, relationships RelationshipStore, config LeidenConfig) *Leiden {
	return &Leiden{
		config:        config,
		entities:      entities,
		relationships: relationships,
		rng:           rand.New(rand.NewSource(config.RandomSeed)),
	}
}

// HierarchicalCommunity represents a community with its hierarchy level
type HierarchicalCommunity struct {
	EntityIDs []uint64 // entities in this community
	Level     int      // hierarchy level (0 = top level)
	ParentIdx int      // index of parent community (-1 for root)
	Children  []int    // indices of child communities
}

// ComputeHierarchicalCommunities runs hierarchical Leiden up to MaxLevels
// Returns communities organized by level: result[level] = [][]uint64
func (l *Leiden) ComputeHierarchicalCommunities() [][]HierarchicalCommunity {
	l.buildGraph()

	if len(l.adjWeights) == 0 {
		return nil
	}

	// Result: communities per level
	result := make([][]HierarchicalCommunity, 0, l.config.MaxLevels)

	// Level 0: all entities as one "super community"
	allEntities := make([]uint64, 0, len(l.adjWeights))
	for eid := range l.adjWeights {
		allEntities = append(allEntities, eid)
	}

	// Queue of communities to potentially split
	// Each item: (entityIDs, currentLevel, parentIdx in result[level-1])
	type splitTask struct {
		entityIDs []uint64
		level     int
		parentIdx int
	}

	queue := []splitTask{{entityIDs: allEntities, level: 0, parentIdx: -1}}

	for len(queue) > 0 {
		task := queue[0]
		queue = queue[1:]

		// Check level limit
		if task.level >= l.config.MaxLevels {
			continue
		}

		// Skip if too small
		if len(task.entityIDs) < l.config.MinCommunitySize {
			continue
		}

		// Run Leiden on this subset
		subCommunities := l.leidenOnSubset(task.entityIDs, task.level)

		// If only 1 community found or no split, skip
		if len(subCommunities) <= 1 {
			continue
		}

		// Ensure we have space for this level
		for len(result) <= task.level {
			result = append(result, []HierarchicalCommunity{})
		}

		// Add communities to result
		for _, entityIDs := range subCommunities {
			if len(entityIDs) == 0 {
				continue
			}

			commIdx := len(result[task.level])
			hc := HierarchicalCommunity{
				EntityIDs: entityIDs,
				Level:     task.level,
				ParentIdx: task.parentIdx,
				Children:  []int{},
			}
			result[task.level] = append(result[task.level], hc)

			// Update parent's children if applicable
			if task.level > 0 && task.parentIdx >= 0 && task.parentIdx < len(result[task.level-1]) {
				result[task.level-1][task.parentIdx].Children = append(
					result[task.level-1][task.parentIdx].Children, commIdx)
			}

			// Queue for further splitting if large enough
			if len(entityIDs) >= l.config.MinCommunitySize && task.level+1 < l.config.MaxLevels {
				queue = append(queue, splitTask{
					entityIDs: entityIDs,
					level:     task.level + 1,
					parentIdx: commIdx,
				})
			}
		}
	}

	return result
}

// leidenOnSubset runs Leiden on a subset of entities
func (l *Leiden) leidenOnSubset(entityIDs []uint64, level int) [][]uint64 {
	if len(entityIDs) < 2 {
		return [][]uint64{entityIDs}
	}

	// Build subgraph adjacency
	entitySet := make(map[uint64]bool)
	for _, eid := range entityIDs {
		entitySet[eid] = true
	}

	subAdj := make(map[uint64]map[uint64]float64)
	subStrength := make(map[uint64]float64)
	subTotalWeight := 0.0

	for _, eid := range entityIDs {
		subAdj[eid] = make(map[uint64]float64)
		subStrength[eid] = 0
	}

	// Get relationships within subset
	for _, eid := range entityIDs {
		if adj, ok := l.adjWeights[eid]; ok {
			for neighbor, weight := range adj {
				if entitySet[neighbor] {
					subAdj[eid][neighbor] = weight
					subStrength[eid] += weight
					subTotalWeight += weight / 2 // count once
				}
			}
		}
	}

	if subTotalWeight == 0 {
		// No edges, return as single community
		return [][]uint64{entityIDs}
	}

	// Adjusted resolution for this level
	resolution := l.config.Resolution * math.Pow(l.config.LevelResolution, float64(level))

	// Initialize: each node in its own community
	nodeToComm := make(map[uint64]int)
	commNodes := make(map[int][]uint64)
	commID := 0
	for _, eid := range entityIDs {
		nodeToComm[eid] = commID
		commNodes[commID] = []uint64{eid}
		commID++
	}

	// Local moving phase
	for iter := 0; iter < l.config.Iterations; iter++ {
		improved := false

		// Shuffle nodes
		shuffled := make([]uint64, len(entityIDs))
		copy(shuffled, entityIDs)
		l.rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		for _, nodeID := range shuffled {
			currentComm := nodeToComm[nodeID]

			// Find neighbor communities
			neighborComms := make(map[int]bool)
			neighborComms[currentComm] = true
			for neighborID := range subAdj[nodeID] {
				neighborComms[nodeToComm[neighborID]] = true
			}

			bestComm := currentComm
			bestDelta := 0.0

			ki := subStrength[nodeID]
			m2 := 2 * subTotalWeight

			for comm := range neighborComms {
				if comm == currentComm {
					continue
				}

				// Calculate modularity gain
				kiIn := 0.0
				for _, nid := range commNodes[comm] {
					if w, ok := subAdj[nodeID][nid]; ok {
						kiIn += w
					}
				}

				kiOut := 0.0
				for _, nid := range commNodes[currentComm] {
					if nid != nodeID {
						if w, ok := subAdj[nodeID][nid]; ok {
							kiOut += w
						}
					}
				}

				sigmaIn := 0.0
				for _, nid := range commNodes[comm] {
					sigmaIn += subStrength[nid]
				}

				sigmaOut := 0.0
				for _, nid := range commNodes[currentComm] {
					if nid != nodeID {
						sigmaOut += subStrength[nid]
					}
				}

				delta := (kiIn - kiOut) / m2
				delta -= resolution * ki * (sigmaIn - sigmaOut) / (m2 * m2)

				if delta > bestDelta {
					bestDelta = delta
					bestComm = comm
				}
			}

			if bestDelta > l.config.MinDelta && bestComm != currentComm {
				// Move node
				oldNodes := commNodes[currentComm]
				for i, nid := range oldNodes {
					if nid == nodeID {
						commNodes[currentComm] = append(oldNodes[:i], oldNodes[i+1:]...)
						break
					}
				}
				commNodes[bestComm] = append(commNodes[bestComm], nodeID)
				nodeToComm[nodeID] = bestComm
				improved = true
			}
		}

		if !improved {
			break
		}
	}

	// Collect results
	results := make([][]uint64, 0)
	for _, nodes := range commNodes {
		if len(nodes) > 0 {
			results = append(results, nodes)
		}
	}

	return results
}

// ComputeCommunities runs Leiden and returns community assignments
func (l *Leiden) ComputeCommunities() [][]uint64 {
	l.buildGraph()

	if len(l.adjWeights) == 0 {
		return nil
	}

	// Initialize: each node in its own community
	l.initializeCommunities()

	// Main Leiden loop
	for iter := 0; iter < l.config.Iterations; iter++ {
		improved := l.moveNodes()
		if !improved {
			break
		}
	}

	// Collect results
	result := make([][]uint64, 0)
	for _, nodes := range l.commNodes {
		if len(nodes) > 0 {
			result = append(result, nodes)
		}
	}

	return result
}

// buildGraph constructs adjacency from entity and relationship stores
func (l *Leiden) buildGraph() {
	l.adjWeights = make(map[uint64]map[uint64]float64)
	l.nodeStrength = make(map[uint64]float64)
	l.totalWeight = 0

	// Initialize from entities
	for _, ent := range l.entities.GetAll() {
		l.adjWeights[ent.ID] = make(map[uint64]float64)
		l.nodeStrength[ent.ID] = 0
	}

	// Build adjacency from relationships
	for _, rel := range l.relationships.GetAll() {
		weight := float64(rel.Weight)
		if weight == 0 {
			weight = 1.0
		}

		// Bidirectional
		if l.adjWeights[rel.SourceID] == nil {
			l.adjWeights[rel.SourceID] = make(map[uint64]float64)
		}
		if l.adjWeights[rel.TargetID] == nil {
			l.adjWeights[rel.TargetID] = make(map[uint64]float64)
		}

		l.adjWeights[rel.SourceID][rel.TargetID] = weight
		l.adjWeights[rel.TargetID][rel.SourceID] = weight

		l.nodeStrength[rel.SourceID] += weight
		l.nodeStrength[rel.TargetID] += weight
		l.totalWeight += weight
	}
}

// initializeCommunities puts each node in its own community
func (l *Leiden) initializeCommunities() {
	l.nodeToComm = make(map[uint64]int)
	l.commNodes = make(map[int][]uint64)

	commID := 0
	for nodeID := range l.adjWeights {
		l.nodeToComm[nodeID] = commID
		l.commNodes[commID] = []uint64{nodeID}
		commID++
	}
}

// moveNodes performs local moving phase of Leiden
func (l *Leiden) moveNodes() bool {
	improved := false

	// Shuffle nodes
	nodes := make([]uint64, 0, len(l.adjWeights))
	for nodeID := range l.adjWeights {
		nodes = append(nodes, nodeID)
	}
	l.rng.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})

	for _, nodeID := range nodes {
		currentComm := l.nodeToComm[nodeID]

		// Calculate modularity gain for moving to each neighbor's community
		neighborComms := l.getNeighborCommunities(nodeID)

		bestComm := currentComm
		bestDelta := 0.0

		for comm := range neighborComms {
			if comm == currentComm {
				continue
			}

			delta := l.modularityGain(nodeID, currentComm, comm)
			if delta > bestDelta {
				bestDelta = delta
				bestComm = comm
			}
		}

		// Move if improvement
		if bestDelta > l.config.MinDelta && bestComm != currentComm {
			l.moveNode(nodeID, currentComm, bestComm)
			improved = true
		}
	}

	return improved
}

// getNeighborCommunities returns set of communities of neighbors
func (l *Leiden) getNeighborCommunities(nodeID uint64) map[int]bool {
	comms := make(map[int]bool)
	comms[l.nodeToComm[nodeID]] = true // include own community

	for neighborID := range l.adjWeights[nodeID] {
		comms[l.nodeToComm[neighborID]] = true
	}

	return comms
}

// modularityGain calculates the modularity gain of moving node from oldComm to newComm
func (l *Leiden) modularityGain(nodeID uint64, oldComm, newComm int) float64 {
	if l.totalWeight == 0 {
		return 0
	}

	ki := l.nodeStrength[nodeID]
	m2 := 2 * l.totalWeight

	// Sum of weights to nodes in new community
	kiIn := 0.0
	for _, nid := range l.commNodes[newComm] {
		if w, ok := l.adjWeights[nodeID][nid]; ok {
			kiIn += w
		}
	}

	// Sum of weights to nodes in old community (excluding self)
	kiOut := 0.0
	for _, nid := range l.commNodes[oldComm] {
		if nid != nodeID {
			if w, ok := l.adjWeights[nodeID][nid]; ok {
				kiOut += w
			}
		}
	}

	// Sum of strengths in new community
	sigmaIn := 0.0
	for _, nid := range l.commNodes[newComm] {
		sigmaIn += l.nodeStrength[nid]
	}

	// Sum of strengths in old community (excluding node)
	sigmaOut := 0.0
	for _, nid := range l.commNodes[oldComm] {
		if nid != nodeID {
			sigmaOut += l.nodeStrength[nid]
		}
	}

	// Modularity gain formula
	resolution := l.config.Resolution
	gain := (kiIn - kiOut) / m2
	gain -= resolution * ki * (sigmaIn - sigmaOut) / (m2 * m2)

	return gain
}

// moveNode moves a node from oldComm to newComm
func (l *Leiden) moveNode(nodeID uint64, oldComm, newComm int) {
	// Remove from old community
	oldNodes := l.commNodes[oldComm]
	for i, nid := range oldNodes {
		if nid == nodeID {
			l.commNodes[oldComm] = append(oldNodes[:i], oldNodes[i+1:]...)
			break
		}
	}

	// Add to new community
	l.commNodes[newComm] = append(l.commNodes[newComm], nodeID)
	l.nodeToComm[nodeID] = newComm
}

// =============================================================================
// Community Builder - Creates Community objects from Leiden results
// =============================================================================

// BuildCommunities creates Community objects from clustering results
func BuildCommunities(
	clusters [][]uint64,
	entityStore EntityStore,
	relStore RelationshipStore,
	idGen *types.IDGenerator,
	level int,
) []*types.Community {
	communities := make([]*types.Community, 0, len(clusters))

	for _, entityIDs := range clusters {
		if len(entityIDs) == 0 {
			continue
		}

		// Build title from entity titles (top 3)
		titles := make([]string, 0, 3)
		for i, eid := range entityIDs {
			if i >= 3 {
				break
			}
			if ent, ok := entityStore.Get(eid); ok {
				titles = append(titles, ent.Title)
			}
		}
		title := ""
		for i, t := range titles {
			if i > 0 {
				title += ", "
			}
			title += t
		}

		// Find relationships within community
		entitySet := make(map[uint64]bool)
		for _, eid := range entityIDs {
			entitySet[eid] = true
		}

		relIDs := make([]uint64, 0)
		for _, eid := range entityIDs {
			for _, rel := range relStore.GetOutgoing(eid) {
				if entitySet[rel.TargetID] {
					relIDs = append(relIDs, rel.ID)
				}
			}
		}

		comm := &types.Community{
			ID:              idGen.NextCommunityID(),
			Title:           title,
			Level:           level,
			EntityIDs:       entityIDs,
			RelationshipIDs: relIDs,
			Summary:         "", // To be filled by LLM
			FullContent:     "", // To be filled by LLM
		}

		communities = append(communities, comm)
	}

	return communities
}

// BuildHierarchicalCommunities creates Community objects from hierarchical Leiden results
// Returns all communities across all levels with proper level assignment
func BuildHierarchicalCommunities(
	hierarchical [][]HierarchicalCommunity,
	entityStore EntityStore,
	relStore RelationshipStore,
	idGen *types.IDGenerator,
) []*types.Community {
	communities := make([]*types.Community, 0)

	for level, levelComms := range hierarchical {
		for _, hc := range levelComms {
			if len(hc.EntityIDs) == 0 {
				continue
			}

			// Build title from entity titles (top 3)
			titles := make([]string, 0, 3)
			for i, eid := range hc.EntityIDs {
				if i >= 3 {
					break
				}
				if ent, ok := entityStore.Get(eid); ok {
					titles = append(titles, ent.Title)
				}
			}
			title := ""
			for i, t := range titles {
				if i > 0 {
					title += ", "
				}
				title += t
			}

			// Find relationships within community
			entitySet := make(map[uint64]bool)
			for _, eid := range hc.EntityIDs {
				entitySet[eid] = true
			}

			relIDs := make([]uint64, 0)
			for _, eid := range hc.EntityIDs {
				for _, rel := range relStore.GetOutgoing(eid) {
					if entitySet[rel.TargetID] {
						relIDs = append(relIDs, rel.ID)
					}
				}
			}

			comm := &types.Community{
				ID:              idGen.NextCommunityID(),
				Title:           title,
				Level:           level, // Use hierarchy level
				EntityIDs:       hc.EntityIDs,
				RelationshipIDs: relIDs,
				Summary:         "", // To be filled by LLM
				FullContent:     "", // To be filled by LLM
			}

			communities = append(communities, comm)
		}
	}

	return communities
}

// =============================================================================
// Graph Traversal Utilities
// =============================================================================

// BFSTraversal performs breadth-first search from seed entities
func BFSTraversal(
	seedIDs []uint64,
	relStore RelationshipStore,
	maxHops int,
	maxNodes int,
) ([]uint64, map[uint64]int, []types.TraversalStep) {
	// Returns: visited node IDs, node -> hop distance, traversal steps

	visited := make(map[uint64]int) // nodeID -> hop distance
	var traversal []types.TraversalStep

	// Initialize with seeds at hop 0
	queue := make([]uint64, 0)
	for _, sid := range seedIDs {
		if _, seen := visited[sid]; !seen {
			visited[sid] = 0
			queue = append(queue, sid)
		}
	}

	for len(queue) > 0 && len(visited) < maxNodes {
		currentID := queue[0]
		queue = queue[1:]

		currentHop := visited[currentID]
		if currentHop >= maxHops {
			continue
		}

		// Get neighbors
		outgoing := relStore.GetOutgoing(currentID)
		incoming := relStore.GetIncoming(currentID)

		for _, rel := range outgoing {
			if _, seen := visited[rel.TargetID]; !seen {
				visited[rel.TargetID] = currentHop + 1
				queue = append(queue, rel.TargetID)

				traversal = append(traversal, types.TraversalStep{
					FromEntityID:   currentID,
					ToEntityID:     rel.TargetID,
					RelationshipID: rel.ID,
					RelType:        rel.Type,
					Weight:         rel.Weight,
					Hop:            currentHop + 1,
				})

				if len(visited) >= maxNodes {
					break
				}
			}
		}

		if len(visited) >= maxNodes {
			break
		}

		for _, rel := range incoming {
			if _, seen := visited[rel.SourceID]; !seen {
				visited[rel.SourceID] = currentHop + 1
				queue = append(queue, rel.SourceID)

				traversal = append(traversal, types.TraversalStep{
					FromEntityID:   currentID,
					ToEntityID:     rel.SourceID,
					RelationshipID: rel.ID,
					RelType:        rel.Type,
					Weight:         rel.Weight,
					Hop:            currentHop + 1,
				})

				if len(visited) >= maxNodes {
					break
				}
			}
		}
	}

	// Convert to sorted list
	nodeIDs := make([]uint64, 0, len(visited))
	for nid := range visited {
		nodeIDs = append(nodeIDs, nid)
	}
	sort.Slice(nodeIDs, func(i, j int) bool {
		return visited[nodeIDs[i]] < visited[nodeIDs[j]]
	})

	return nodeIDs, visited, traversal
}

// PageRank computes PageRank scores for entities
func PageRank(
	entityIDs []uint64,
	relStore RelationshipStore,
	damping float64,
	iterations int,
) map[uint64]float64 {
	n := len(entityIDs)
	if n == 0 {
		return nil
	}

	// Initialize scores
	scores := make(map[uint64]float64)
	for _, eid := range entityIDs {
		scores[eid] = 1.0 / float64(n)
	}

	entitySet := make(map[uint64]bool)
	for _, eid := range entityIDs {
		entitySet[eid] = true
	}

	// Build outgoing degree
	outDegree := make(map[uint64]int)
	for _, eid := range entityIDs {
		count := 0
		for _, rel := range relStore.GetOutgoing(eid) {
			if entitySet[rel.TargetID] {
				count++
			}
		}
		outDegree[eid] = count
	}

	// Iterate
	for iter := 0; iter < iterations; iter++ {
		newScores := make(map[uint64]float64)

		for _, eid := range entityIDs {
			sum := 0.0
			for _, rel := range relStore.GetIncoming(eid) {
				if entitySet[rel.SourceID] && outDegree[rel.SourceID] > 0 {
					sum += scores[rel.SourceID] / float64(outDegree[rel.SourceID])
				}
			}
			newScores[eid] = (1-damping)/float64(n) + damping*sum
		}

		// Normalize
		total := 0.0
		for _, s := range newScores {
			total += s
		}
		if total > 0 {
			for eid := range newScores {
				newScores[eid] /= total
			}
		}

		scores = newScores
	}

	return scores
}

// ConnectedComponents finds connected components in the graph
func ConnectedComponents(
	entityIDs []uint64,
	relStore RelationshipStore,
) [][]uint64 {
	visited := make(map[uint64]bool)
	components := make([][]uint64, 0)

	entitySet := make(map[uint64]bool)
	for _, eid := range entityIDs {
		entitySet[eid] = true
	}

	for _, startID := range entityIDs {
		if visited[startID] {
			continue
		}

		// BFS to find component
		component := make([]uint64, 0)
		queue := []uint64{startID}
		visited[startID] = true

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			component = append(component, curr)

			// Get neighbors
			neighbors := relStore.GetNeighbors(curr)
			for _, rel := range neighbors {
				// Extract the other entity ID
				nid := rel.TargetID
				if nid == curr {
					nid = rel.SourceID
				}
				if entitySet[nid] && !visited[nid] {
					visited[nid] = true
					queue = append(queue, nid)
				}
			}
		}

		components = append(components, component)
	}

	return components
}

// Betweenness calculates approximate betweenness centrality
func Betweenness(
	entityIDs []uint64,
	relStore RelationshipStore,
	sampleSize int,
) map[uint64]float64 {
	scores := make(map[uint64]float64)
	for _, eid := range entityIDs {
		scores[eid] = 0
	}

	entitySet := make(map[uint64]bool)
	for _, eid := range entityIDs {
		entitySet[eid] = true
	}

	// Sample source nodes
	sources := entityIDs
	if sampleSize > 0 && sampleSize < len(entityIDs) {
		sources = make([]uint64, sampleSize)
		perm := rand.Perm(len(entityIDs))
		for i := 0; i < sampleSize; i++ {
			sources[i] = entityIDs[perm[i]]
		}
	}

	// BFS from each source
	for _, source := range sources {
		// BFS to find shortest paths
		dist := make(map[uint64]int)
		paths := make(map[uint64]int)
		pred := make(map[uint64][]uint64)

		dist[source] = 0
		paths[source] = 1

		queue := []uint64{source}
		order := []uint64{source}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]

			neighbors := relStore.GetNeighbors(curr)
			for _, rel := range neighbors {
				// Extract the other entity ID
				nid := rel.TargetID
				if nid == curr {
					nid = rel.SourceID
				}
				if !entitySet[nid] {
					continue
				}

				if _, seen := dist[nid]; !seen {
					dist[nid] = dist[curr] + 1
					paths[nid] = 0
					queue = append(queue, nid)
					order = append(order, nid)
				}

				if dist[nid] == dist[curr]+1 {
					paths[nid] += paths[curr]
					pred[nid] = append(pred[nid], curr)
				}
			}
		}

		// Accumulate betweenness
		delta := make(map[uint64]float64)
		for _, eid := range entityIDs {
			delta[eid] = 0
		}

		// Process in reverse BFS order
		for i := len(order) - 1; i >= 0; i-- {
			w := order[i]
			for _, v := range pred[w] {
				if paths[w] > 0 {
					delta[v] += (float64(paths[v]) / float64(paths[w])) * (1 + delta[w])
				}
			}
			if w != source {
				scores[w] += delta[w]
			}
		}
	}

	// Normalize
	scale := 1.0
	if sampleSize > 0 && sampleSize < len(entityIDs) {
		scale = float64(len(entityIDs)) / float64(sampleSize)
	}
	for eid := range scores {
		scores[eid] *= scale / 2.0 // divide by 2 for undirected
	}

	return scores
}
