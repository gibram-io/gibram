// Package engine provides the query engine benchmarks
package engine

import (
	"testing"

	"github.com/gibram-io/gibram/pkg/types"
)

const benchSessionID = "bench-session-1"

// =============================================================================
// Document Operation Benchmarks
// =============================================================================

func BenchmarkEngine_AddDocument(b *testing.B) {
	e := NewEngine(128)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mustAddDocument(b, e, benchSessionID, "doc-"+benchItoa(i), "file.txt")
	}
}

func BenchmarkEngine_GetDocument(b *testing.B) {
	e := NewEngine(128)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAddDocument(b, e, benchSessionID, "doc-"+benchItoa(i), "file.txt")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.GetDocument(benchSessionID, uint64(i%1000)+1)
	}
}

// =============================================================================
// Entity Operation Benchmarks
// =============================================================================

func BenchmarkEngine_AddEntity(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}
}

func BenchmarkEngine_GetEntity(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.GetEntity(benchSessionID, uint64(i%1000)+1)
	}
}

func BenchmarkEngine_GetEntityByTitle(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.GetEntityByTitle(benchSessionID, "Entity "+benchItoa(i%1000))
	}
}

// =============================================================================
// TextUnit Operation Benchmarks
// =============================================================================

func BenchmarkEngine_AddTextUnit(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)
	doc := mustAddDocument(b, e, benchSessionID, "doc-1", "file.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mustAddTextUnit(b, e, benchSessionID, "tu-"+benchItoa(i), doc.ID, "Content", embedding, 10)
	}
}

// =============================================================================
// Relationship Operation Benchmarks
// =============================================================================

func BenchmarkEngine_AddRelationship(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate entities
	entities := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		ent := mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
		entities[i] = ent.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sourceIdx := i % 1000
		targetIdx := (i + 1) % 1000
		mustAddRelationship(b, e, benchSessionID, "rel-"+benchItoa(i), entities[sourceIdx], entities[targetIdx], "RELATED", "Desc", 1.0)
	}
}

// =============================================================================
// Query Benchmarks
// =============================================================================

func BenchmarkEngine_Query_100Entities(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 100; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Query(benchSessionID, spec); err != nil {
			b.Fatalf("Query error: %v", err)
		}
	}
}

func BenchmarkEngine_Query_1KEntities(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping in short mode")
	}

	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Query(benchSessionID, spec); err != nil {
			b.Fatalf("Query error: %v", err)
		}
	}
}

func BenchmarkEngine_Query_5KEntities(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping in short mode")
	}

	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 5000; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Query(benchSessionID, spec); err != nil {
			b.Fatalf("Query error: %v", err)
		}
	}
}

func BenchmarkEngine_Query_WithGraphExpansion(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate with linked entities
	entities := make([]uint64, 500)
	for i := 0; i < 500; i++ {
		ent := mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
		entities[i] = ent.ID
	}

	// Add relationships (chain)
	for i := 0; i < 499; i++ {
		mustAddRelationship(b, e, benchSessionID, "rel-"+benchItoa(i), entities[i], entities[i+1], "RELATED", "Desc", 1.0)
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding
	spec.KHops = 2
	spec.SearchTypes = []types.SearchType{types.SearchTypeEntity}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Query(benchSessionID, spec); err != nil {
			b.Fatalf("Query error: %v", err)
		}
	}
}

// =============================================================================
// Parallel Query Benchmarks
// =============================================================================

func BenchmarkEngine_Query_Parallel(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := e.Query(benchSessionID, spec); err != nil {
				b.Fatalf("Query error: %v", err)
			}
		}
	})
}

// =============================================================================
// Explain Benchmarks
// =============================================================================

func BenchmarkEngine_Explain(b *testing.B) {
	e := NewEngine(128)
	embedding := benchRandomVector(128)

	// Pre-populate
	for i := 0; i < 100; i++ {
		mustAddEntity(b, e, benchSessionID, "ent-"+benchItoa(i), "Entity "+benchItoa(i), "test", "Desc", embedding)
	}

	// Run a query to get QueryID
	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding
	result, err := e.Query(benchSessionID, spec)
	if err != nil {
		b.Fatalf("Query error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Explain(result.QueryID)
	}
}

// =============================================================================
// LRU Cache Benchmarks
// =============================================================================

func BenchmarkQueryLogLRU_Set(b *testing.B) {
	cache := newQueryLogLRU(10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(uint64(i), &queryLog{})
	}
}

func BenchmarkQueryLogLRU_Get(b *testing.B) {
	cache := newQueryLogLRU(10000)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		cache.Set(uint64(i), &queryLog{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(uint64(i % 10000))
	}
}

// =============================================================================
// Helpers
// =============================================================================

func benchRandomVector(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(i%10) / 10.0
	}
	return v
}

func benchItoa(i int) string {
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
