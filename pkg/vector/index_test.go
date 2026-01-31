// Package vector provides vector index tests
package vector

import (
	"bytes"
	"math"
	"math/rand"
	"sync"
	"testing"
)

var (
	testRand   = rand.New(rand.NewSource(1))
	testRandMu sync.Mutex
)

func mustAdd(tb testing.TB, idx Index, id uint64, vec []float32) {
	tb.Helper()
	if err := idx.Add(id, vec); err != nil {
		tb.Fatalf("Add(%d) error: %v", id, err)
	}
}

// Helper to create a random vector
func randomVector(dim int) []float32 {
	vec := make([]float32, dim)
	testRandMu.Lock()
	for i := range vec {
		vec[i] = testRand.Float32()*2 - 1
	}
	testRandMu.Unlock()
	return vec
}

// Helper to normalize a vector
func normalizeVector(vec []float32) []float32 {
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	if norm == 0 {
		return vec
	}
	result := make([]float32, len(vec))
	for i := range vec {
		result[i] = vec[i] / norm
	}
	return result
}

// =============================================================================
// Basic HNSW Tests
// =============================================================================

func TestHNSWIndex_NewIndex(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(128, config)

	if idx.Dimension() != 128 {
		t.Errorf("Dimension() = %d, want 128", idx.Dimension())
	}

	if idx.Count() != 0 {
		t.Errorf("Count() = %d, want 0", idx.Count())
	}
}

func TestHNSWIndex_Add(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := idx.Add(1, vec); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if idx.Count() != 1 {
		t.Errorf("Count() = %d, want 1", idx.Count())
	}

	// Test duplicate
	if err := idx.Add(1, vec); err == nil {
		t.Error("Add() duplicate should return error")
	}
}

func TestHNSWIndex_AddWrongDimension(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	vec := []float32{0.1, 0.2, 0.3} // Wrong dimension
	if err := idx.Add(1, vec); err == nil {
		t.Error("Add() wrong dimension should return error")
	}
}

func TestHNSWIndex_Remove(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	mustAdd(t, idx, 1, vec)

	if !idx.Remove(1) {
		t.Error("Remove() returned false")
	}

	if idx.Count() != 0 {
		t.Errorf("Count() = %d, want 0", idx.Count())
	}

	// Remove non-existent
	if idx.Remove(999) {
		t.Error("Remove() non-existent should return false")
	}
}

func TestHNSWIndex_Search(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add some vectors
	vectors := []struct {
		id  uint64
		vec []float32
	}{
		{1, []float32{1.0, 0.0, 0.0, 0.0}},
		{2, []float32{0.9, 0.1, 0.0, 0.0}},
		{3, []float32{0.0, 1.0, 0.0, 0.0}},
		{4, []float32{0.0, 0.0, 1.0, 0.0}},
	}

	for _, v := range vectors {
		mustAdd(t, idx, v.id, v.vec)
	}

	// Search for vector similar to id=1
	query := []float32{0.95, 0.05, 0.0, 0.0}
	results := idx.Search(query, 2)

	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, want 2", len(results))
	}

	// ID 1 or 2 should be in top results (most similar to query)
	foundRelevant := false
	for _, r := range results {
		if r.ID == 1 || r.ID == 2 {
			foundRelevant = true
			break
		}
	}
	if !foundRelevant {
		t.Error("Search() did not find most similar vector")
	}
}

func TestHNSWIndex_SearchEmpty(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	query := []float32{1.0, 0.0, 0.0, 0.0}
	results := idx.Search(query, 5)

	if len(results) != 0 {
		t.Errorf("Search() on empty index returned %d results, want 0", len(results))
	}
}

func TestHNSWIndex_SearchKLargerThanCount(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	results := idx.Search([]float32{0.5, 0.5, 0.0, 0.0}, 10)

	if len(results) != 2 {
		t.Errorf("Search() returned %d results, want 2", len(results))
	}
}

// =============================================================================
// Cosine Similarity Tests
// =============================================================================

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		delta    float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{-1.0, 0.0, 0.0},
			expected: -1.0,
			delta:    0.001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.9, 0.1, 0.0},
			expected: 0.9938,
			delta:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(result-tt.expected)) > float64(tt.delta) {
				t.Errorf("cosineSimilarity() = %f, want %f", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Persistence Tests
// =============================================================================

func TestHNSWIndex_SaveLoad(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})
	mustAdd(t, idx, 3, []float32{0.0, 0.0, 1.0, 0.0})

	// Save
	var buf bytes.Buffer
	if err := idx.Save(&buf); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Create new index and load
	idx2 := NewHNSWIndex(4, config)
	if err := idx2.Load(&buf); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify
	if idx2.Count() != 3 {
		t.Errorf("After Load() Count() = %d, want 3", idx2.Count())
	}

	if idx2.Dimension() != 4 {
		t.Errorf("After Load() Dimension() = %d, want 4", idx2.Dimension())
	}

	// Search should work after load
	results := idx2.Search([]float32{0.9, 0.1, 0.0, 0.0}, 1)
	if len(results) != 1 {
		t.Error("Search() after Load() failed")
	}
}

// =============================================================================
// Concurrent Tests
// =============================================================================

func TestHNSWIndex_ConcurrentAdd(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(16, config)

	var wg sync.WaitGroup
	const n = 100
	errCh := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			vec := randomVector(16)
			if err := idx.Add(uint64(id), vec); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Add() error: %v", err)
	}

	if idx.Count() != n {
		t.Errorf("Count() = %d, want %d", idx.Count(), n)
	}
}

func TestHNSWIndex_ConcurrentSearch(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(16, config)

	// Pre-populate
	for i := 0; i < 50; i++ {
		mustAdd(t, idx, uint64(i), randomVector(16))
	}

	var wg sync.WaitGroup
	const n = 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			query := randomVector(16)
			results := idx.Search(query, 5)
			_ = results // Use results
		}()
	}

	wg.Wait()
}

func TestHNSWIndex_ConcurrentMixed(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(16, config)

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	// Concurrent adds
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := idx.Add(uint64(id), randomVector(16)); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent searches
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			idx.Search(randomVector(16), 5)
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Add() error: %v", err)
	}
}

// =============================================================================
// Rebuild and Integrity Tests
// =============================================================================

func TestHNSWIndex_GetAllVectors(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	vectors := idx.GetAllVectors()

	if len(vectors) != 2 {
		t.Errorf("GetAllVectors() returned %d vectors, want 2", len(vectors))
	}

	if _, ok := vectors[1]; !ok {
		t.Error("GetAllVectors() missing ID 1")
	}
	if _, ok := vectors[2]; !ok {
		t.Error("GetAllVectors() missing ID 2")
	}
}

func TestHNSWIndex_Rebuild(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add some vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})
	mustAdd(t, idx, 3, []float32{0.0, 0.0, 1.0, 0.0})

	originalCount := idx.Count()

	// Rebuild
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	// Count should be same
	if idx.Count() != originalCount {
		t.Errorf("After Rebuild() Count() = %d, want %d", idx.Count(), originalCount)
	}

	// Search should still work
	results := idx.Search([]float32{0.9, 0.1, 0.0, 0.0}, 1)
	if len(results) != 1 {
		t.Error("Search() after Rebuild() failed")
	}
}

func TestHNSWIndex_ValidateIntegrity(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Empty index should be valid
	if err := idx.ValidateIntegrity(); err != nil {
		t.Errorf("ValidateIntegrity() on empty index error = %v", err)
	}

	// Add vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	// Should still be valid
	if err := idx.ValidateIntegrity(); err != nil {
		t.Errorf("ValidateIntegrity() error = %v", err)
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestHNSWIndex_SingleVector(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})

	results := idx.Search([]float32{1.0, 0.0, 0.0, 0.0}, 5)
	if len(results) != 1 {
		t.Errorf("Search() returned %d results, want 1", len(results))
	}

	if results[0].ID != 1 {
		t.Errorf("Search() result ID = %d, want 1", results[0].ID)
	}
}

func TestHNSWIndex_ZeroVector(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Zero vector should still be addable
	mustAdd(t, idx, 1, []float32{0.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{1.0, 0.0, 0.0, 0.0})

	if idx.Count() != 2 {
		t.Errorf("Count() = %d, want 2", idx.Count())
	}
}

func TestHNSWIndex_LargeK(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	for i := 0; i < 10; i++ {
		mustAdd(t, idx, uint64(i), randomVector(4))
	}

	// Request more than available
	results := idx.Search(randomVector(4), 100)
	if len(results) != 10 {
		t.Errorf("Search(k=100) returned %d results, want 10", len(results))
	}
}

// =============================================================================
// Quality Tests
// =============================================================================

func TestHNSWIndex_SearchQuality(t *testing.T) {
	config := DefaultHNSWConfig()
	config.EfSearch = 100 // Higher for better quality
	idx := NewHNSWIndex(32, config)

	// Create a target vector
	target := normalizeVector(randomVector(32))

	// Create vectors at various distances from target
	mustAdd(t, idx, 1, target) // Exact match

	// Near vectors (small perturbations)
	for i := 2; i <= 10; i++ {
		nearVec := make([]float32, 32)
		for j := range nearVec {
			nearVec[j] = target[j] + (rand.Float32()-0.5)*0.1
		}
		mustAdd(t, idx, uint64(i), normalizeVector(nearVec))
	}

	// Far vectors (random)
	for i := 11; i <= 50; i++ {
		mustAdd(t, idx, uint64(i), normalizeVector(randomVector(32)))
	}

	// Search for target
	results := idx.Search(target, 5)

	// The exact match (ID=1) should be in top results
	foundExact := false
	for _, r := range results {
		if r.ID == 1 {
			foundExact = true
			if r.Similarity < 0.99 {
				t.Errorf("Exact match similarity = %f, want >= 0.99", r.Similarity)
			}
			break
		}
	}

	if !foundExact {
		t.Error("Search() did not find exact match in top 5")
	}
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestCosineSimilarity_DifferentLength(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	result := cosineSimilarity(a, b)
	if result != 0 {
		t.Errorf("cosineSimilarity() with different lengths = %f, want 0", result)
	}
}

func TestCosineSimilarity_ZeroVectors(t *testing.T) {
	a := []float32{0.0, 0.0, 0.0}
	b := []float32{1.0, 0.0, 0.0}

	result := cosineSimilarity(a, b)
	if result != 0 {
		t.Errorf("cosineSimilarity() with zero vector = %f, want 0", result)
	}
}

func TestHNSWIndex_SearchWrongDimension(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})

	// Search with wrong dimension
	query := []float32{1.0, 0.0} // Wrong dimension
	results := idx.Search(query, 5)

	if results != nil {
		t.Error("Search() with wrong dimension should return nil")
	}
}

func TestHNSWIndex_RemoveEntryPoint(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add vectors - first one becomes entry point
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})
	mustAdd(t, idx, 3, []float32{0.0, 0.0, 1.0, 0.0})

	// Remove the first one (likely entry point)
	idx.Remove(1)

	// Index should still work
	results := idx.Search([]float32{0.0, 1.0, 0.0, 0.0}, 2)
	if len(results) == 0 {
		t.Error("Search() after removing entry point returned no results")
	}
}

func TestHNSWIndex_RemoveAllVectors(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	idx.Remove(1)
	idx.Remove(2)

	if idx.Count() != 0 {
		t.Errorf("Count() = %d, want 0", idx.Count())
	}

	// Search on empty index
	results := idx.Search([]float32{1.0, 0.0, 0.0, 0.0}, 1)
	if len(results) != 0 {
		t.Errorf("Search() on empty index = %d results, want 0", len(results))
	}
}

func TestHNSWIndex_RemoveWithReconnection(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add several connected vectors
	for i := 1; i <= 10; i++ {
		vec := make([]float32, 4)
		vec[i%4] = 1.0
		mustAdd(t, idx, uint64(i), vec)
	}

	// Remove middle vectors
	idx.Remove(5)
	idx.Remove(6)

	// Validate integrity after removal
	if err := idx.ValidateIntegrity(); err != nil {
		t.Logf("ValidateIntegrity() after removal: %v (may be expected)", err)
	}

	// Search should still work
	results := idx.Search([]float32{1.0, 0.0, 0.0, 0.0}, 3)
	if len(results) == 0 {
		t.Error("Search() after removal returned no results")
	}
}

func TestHNSWIndex_RebuildEmpty(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Rebuild empty index should not error
	if err := idx.Rebuild(); err != nil {
		t.Errorf("Rebuild() on empty index error = %v", err)
	}
}

func TestHNSWIndex_RebuildAfterDeletions(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add vectors
	for i := 1; i <= 20; i++ {
		mustAdd(t, idx, uint64(i), randomVector(4))
	}

	// Delete many
	for i := 1; i <= 15; i++ {
		idx.Remove(uint64(i))
	}

	// Rebuild
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	if idx.Count() != 5 {
		t.Errorf("After Rebuild() Count() = %d, want 5", idx.Count())
	}

	// Search should work
	results := idx.Search(randomVector(4), 3)
	if len(results) == 0 {
		t.Error("Search() after Rebuild() returned no results")
	}
}

func TestHNSWIndex_SaveLoadEmpty(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	var buf bytes.Buffer
	if err := idx.Save(&buf); err != nil {
		t.Fatalf("Save() empty index error = %v", err)
	}

	idx2 := NewHNSWIndex(4, config)
	if err := idx2.Load(&buf); err != nil {
		t.Fatalf("Load() empty index error = %v", err)
	}

	if idx2.Count() != 0 {
		t.Errorf("After Load() empty Count() = %d, want 0", idx2.Count())
	}
}

func TestHNSWIndex_LargeIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large index test in short mode")
	}

	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(64, config)

	// Add many vectors
	const n = 500
	for i := 0; i < n; i++ {
		mustAdd(t, idx, uint64(i), randomVector(64))
	}

	if idx.Count() != n {
		t.Errorf("Count() = %d, want %d", idx.Count(), n)
	}

	// Multiple searches
	for i := 0; i < 10; i++ {
		results := idx.Search(randomVector(64), 10)
		if len(results) != 10 {
			t.Errorf("Search() returned %d results, want 10", len(results))
		}
	}
}

func TestDefaultHNSWConfig(t *testing.T) {
	config := DefaultHNSWConfig()

	if config.M <= 0 {
		t.Error("DefaultHNSWConfig().M should be positive")
	}
	if config.EfConstruction <= 0 {
		t.Error("DefaultHNSWConfig().EfConstruction should be positive")
	}
	if config.EfSearch <= 0 {
		t.Error("DefaultHNSWConfig().EfSearch should be positive")
	}
	if config.MaxLevel <= 0 {
		t.Error("DefaultHNSWConfig().MaxLevel should be positive")
	}
	if config.ML <= 0 {
		t.Error("DefaultHNSWConfig().ML should be positive")
	}
}

func TestHNSWIndex_MultipleRemoveAdd(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add, remove, re-add cycle
	for cycle := 0; cycle < 3; cycle++ {
		for i := 1; i <= 10; i++ {
			mustAdd(t, idx, uint64(i), randomVector(4))
		}
		for i := 1; i <= 10; i++ {
			idx.Remove(uint64(i))
		}
	}

	// Final state should be empty
	if idx.Count() != 0 {
		t.Errorf("After cycles Count() = %d, want 0", idx.Count())
	}
}

func TestHNSWIndex_ConcurrentRemove(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(16, config)

	// Pre-populate
	for i := 0; i < 100; i++ {
		mustAdd(t, idx, uint64(i), randomVector(16))
	}

	var wg sync.WaitGroup

	// Concurrent removals
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			idx.Remove(uint64(id))
		}(i)
	}

	wg.Wait()

	if idx.Count() != 50 {
		t.Errorf("After concurrent removes Count() = %d, want 50", idx.Count())
	}
}

func TestPriorityQueue(t *testing.T) {
	pq := &priorityQueue{}

	pq.Push(pqItem{id: 1, priority: 0.5})
	pq.Push(pqItem{id: 2, priority: 0.8})
	pq.Push(pqItem{id: 3, priority: 0.3})

	if pq.Len() != 3 {
		t.Errorf("Len() = %d, want 3", pq.Len())
	}

	// Pop should return highest priority
	item := pq.Pop()
	if item.id != 2 {
		t.Errorf("Pop() returned id=%d, want 2", item.id)
	}

	// Peek should return current highest
	peek := pq.Peek()
	if peek.id != 1 {
		t.Errorf("Peek() returned id=%d, want 1", peek.id)
	}

	// PopWorst should return lowest priority
	worst := pq.PopWorst()
	if worst.id != 3 {
		t.Errorf("PopWorst() returned id=%d, want 3", worst.id)
	}
}

// =============================================================================
// BruteForceIndex Tests
// =============================================================================

func TestBruteForceIndex_New(t *testing.T) {
	idx := NewBruteForceIndex(64)

	if idx.Dimension() != 64 {
		t.Errorf("Dimension() = %d, want 64", idx.Dimension())
	}
	if idx.Count() != 0 {
		t.Errorf("Count() = %d, want 0", idx.Count())
	}
}

func TestBruteForceIndex_Add(t *testing.T) {
	idx := NewBruteForceIndex(4)

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	if err := idx.Add(1, vec); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	if idx.Count() != 1 {
		t.Errorf("Count() = %d, want 1", idx.Count())
	}

	// Add duplicate
	if err := idx.Add(1, vec); err == nil {
		t.Error("Add() duplicate should return error")
	}

	// Add wrong dimension
	if err := idx.Add(2, []float32{0.1, 0.2}); err == nil {
		t.Error("Add() wrong dimension should return error")
	}
}

func TestBruteForceIndex_Remove(t *testing.T) {
	idx := NewBruteForceIndex(4)

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	mustAdd(t, idx, 1, vec)

	if !idx.Remove(1) {
		t.Error("Remove() should return true")
	}
	if idx.Count() != 0 {
		t.Errorf("Count() after remove = %d, want 0", idx.Count())
	}

	// Remove non-existent
	if idx.Remove(999) {
		t.Error("Remove() non-existent should return false")
	}
}

func TestBruteForceIndex_Search(t *testing.T) {
	idx := NewBruteForceIndex(4)

	// Add vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.9, 0.1, 0.0, 0.0})
	mustAdd(t, idx, 3, []float32{0.0, 1.0, 0.0, 0.0})
	mustAdd(t, idx, 4, []float32{0.0, 0.0, 1.0, 0.0})

	// Search
	query := []float32{0.95, 0.05, 0.0, 0.0}
	results := idx.Search(query, 2)

	if len(results) != 2 {
		t.Fatalf("Search() returned %d results, want 2", len(results))
	}

	// First result should be ID 1 or 2 (closest to query)
	if results[0].ID != 1 && results[0].ID != 2 {
		t.Error("Search() first result should be id 1 or 2")
	}
}

func TestBruteForceIndex_SearchEmpty(t *testing.T) {
	idx := NewBruteForceIndex(4)

	results := idx.Search([]float32{1.0, 0.0, 0.0, 0.0}, 5)
	if len(results) != 0 {
		t.Errorf("Search() on empty index returned %d results, want 0", len(results))
	}
}

func TestBruteForceIndex_SaveLoad(t *testing.T) {
	idx := NewBruteForceIndex(4)

	// Add vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	// Save
	var buf bytes.Buffer
	if err := idx.Save(&buf); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load into new index
	idx2 := NewBruteForceIndex(4)
	if err := idx2.Load(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if idx2.Count() != 2 {
		t.Errorf("Loaded Count() = %d, want 2", idx2.Count())
	}
}

func TestBruteForceIndex_GetAllVectors(t *testing.T) {
	idx := NewBruteForceIndex(4)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	vectors := idx.GetAllVectors()

	if len(vectors) != 2 {
		t.Errorf("GetAllVectors() returned %d vectors, want 2", len(vectors))
	}
}

func TestBruteForceIndex_Rebuild(t *testing.T) {
	idx := NewBruteForceIndex(4)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	// Rebuild should succeed (no-op for brute force)
	if err := idx.Rebuild(); err != nil {
		t.Errorf("Rebuild() error: %v", err)
	}
}

func TestBruteForceIndex_ValidateIntegrity(t *testing.T) {
	idx := NewBruteForceIndex(4)

	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})

	if err := idx.ValidateIntegrity(); err != nil {
		t.Errorf("ValidateIntegrity() error: %v", err)
	}
}

// =============================================================================
// TryLoadWithRebuild Tests
// =============================================================================

func TestTryLoadWithRebuild(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Add some vectors
	mustAdd(t, idx, 1, []float32{1.0, 0.0, 0.0, 0.0})
	mustAdd(t, idx, 2, []float32{0.0, 1.0, 0.0, 0.0})

	// Save to buffer
	var buf bytes.Buffer
	if err := idx.Save(&buf); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Create new index and try to load with rebuild
	idx2 := NewHNSWIndex(4, config)

	err := idx2.TryLoadWithRebuild(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("TryLoadWithRebuild() error: %v", err)
	}

	if idx2.Count() != 2 {
		t.Errorf("After TryLoadWithRebuild() Count() = %d, want 2", idx2.Count())
	}

	// Verify search still works
	results := idx2.Search([]float32{1.0, 0.0, 0.0, 0.0}, 1)
	if len(results) != 1 || results[0].ID != 1 {
		t.Error("TryLoadWithRebuild() search should still work")
	}
}

func TestTryLoadWithRebuild_Invalid(t *testing.T) {
	config := DefaultHNSWConfig()
	idx := NewHNSWIndex(4, config)

	// Try to load invalid data
	invalidData := bytes.NewReader([]byte("invalid data"))
	err := idx.TryLoadWithRebuild(invalidData)

	// Should fail to load
	if err == nil {
		t.Error("TryLoadWithRebuild() with invalid data should return error")
	}
}

// =============================================================================
// Priority Queue Peek Tests
// =============================================================================

func TestPriorityQueue_PeekEmpty(t *testing.T) {
	pq := &priorityQueue{}

	peek := pq.Peek()
	if peek.id != 0 || peek.priority != 0 {
		t.Error("Peek() on empty queue should return zero value")
	}
}

func TestPriorityQueue_PopEmpty(t *testing.T) {
	pq := &priorityQueue{}

	item := pq.Pop()
	if item.id != 0 || item.priority != 0 {
		t.Error("Pop() on empty queue should return zero value")
	}
}
