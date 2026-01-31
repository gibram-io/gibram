// Package vector provides vector index benchmarks
package vector

import (
	"testing"
)

// =============================================================================
// HNSW Add Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_Add_1K(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		idx := NewHNSWIndex(dim, config)
		vectors := make([][]float32, 1000)
		for j := range vectors {
			vectors[j] = randomVector(dim)
		}
		b.StartTimer()

		for j, vec := range vectors {
			mustAdd(b, idx, uint64(j), vec)
		}
	}
}

func BenchmarkHNSWIndex_Add_10K(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping in short mode")
	}

	config := DefaultHNSWConfig()
	dim := 128

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		idx := NewHNSWIndex(dim, config)
		vectors := make([][]float32, 10000)
		for j := range vectors {
			vectors[j] = randomVector(dim)
		}
		b.StartTimer()

		for j, vec := range vectors {
			mustAdd(b, idx, uint64(j), vec)
		}
	}
}

// =============================================================================
// HNSW Search Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_Search_1K(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128
	idx := NewHNSWIndex(dim, config)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAdd(b, idx, uint64(i), randomVector(dim))
	}

	query := randomVector(dim)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(query, 10)
	}
}

func BenchmarkHNSWIndex_Search_10K(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping in short mode")
	}

	config := DefaultHNSWConfig()
	dim := 128
	idx := NewHNSWIndex(dim, config)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		mustAdd(b, idx, uint64(i), randomVector(dim))
	}

	query := randomVector(dim)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(query, 10)
	}
}

func BenchmarkHNSWIndex_Search_TopK(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128
	idx := NewHNSWIndex(dim, config)

	// Pre-populate
	for i := 0; i < 5000; i++ {
		mustAdd(b, idx, uint64(i), randomVector(dim))
	}

	query := randomVector(dim)

	testCases := []int{1, 5, 10, 20, 50}
	for _, k := range testCases {
		b.Run(testName("k", k), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				idx.Search(query, k)
			}
		})
	}
}

// =============================================================================
// HNSW EfSearch Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_Search_EfSearch(b *testing.B) {
	dim := 128

	// Pre-populate
	vectors := make([][]float32, 5000)
	for i := range vectors {
		vectors[i] = randomVector(dim)
	}

	efValues := []int{20, 50, 100, 200}
	for _, ef := range efValues {
		b.Run(testName("ef", ef), func(b *testing.B) {
			config := DefaultHNSWConfig()
			config.EfSearch = ef
			idx := NewHNSWIndex(dim, config)
			for i, vec := range vectors {
				mustAdd(b, idx, uint64(i), vec)
			}

			query := randomVector(dim)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx.Search(query, 10)
			}
		})
	}
}

// =============================================================================
// Dimension Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_Search_Dimensions(b *testing.B) {
	dims := []int{64, 128, 256, 512}
	config := DefaultHNSWConfig()

	for _, dim := range dims {
		b.Run(testName("dim", dim), func(b *testing.B) {
			idx := NewHNSWIndex(dim, config)
			for i := 0; i < 1000; i++ {
				mustAdd(b, idx, uint64(i), randomVector(dim))
			}

			query := randomVector(dim)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx.Search(query, 10)
			}
		})
	}
}

// =============================================================================
// Cosine Similarity Benchmark
// =============================================================================

func BenchmarkCosineSimilarity(b *testing.B) {
	dims := []int{64, 128, 256, 512}

	for _, dim := range dims {
		b.Run(testName("dim", dim), func(b *testing.B) {
			a := randomVector(dim)
			vec := randomVector(dim)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cosineSimilarity(a, vec)
			}
		})
	}
}

// =============================================================================
// Memory Benchmark
// =============================================================================

func BenchmarkHNSWIndex_Memory(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		idx := NewHNSWIndex(dim, config)
		for j := 0; j < 100; j++ {
			mustAdd(b, idx, uint64(j), randomVector(dim))
		}
	}
}

// =============================================================================
// Concurrent Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_ConcurrentSearch(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128
	idx := NewHNSWIndex(dim, config)

	// Pre-populate
	for i := 0; i < 5000; i++ {
		mustAdd(b, idx, uint64(i), randomVector(dim))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		query := randomVector(dim)
		for pb.Next() {
			idx.Search(query, 10)
		}
	})
}

// =============================================================================
// Save/Load Benchmarks
// =============================================================================

func BenchmarkHNSWIndex_SaveLoad(b *testing.B) {
	config := DefaultHNSWConfig()
	dim := 128
	idx := NewHNSWIndex(dim, config)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		mustAdd(b, idx, uint64(i), randomVector(dim))
	}

	b.Run("Save", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var buf discardWriter
			if err := idx.Save(&buf); err != nil {
				b.Fatalf("Save() error: %v", err)
			}
		}
	})
}

// discardWriter discards all written data
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

// =============================================================================
// Helper
// =============================================================================

func testName(prefix string, value int) string {
	return prefix + "_" + itoa(value)
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
