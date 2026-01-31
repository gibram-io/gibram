// Package memory - Unit tests for critical path operations
package memory

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestMemoryPressureEviction tests the critical memory eviction logic
func TestMemoryPressureEviction(t *testing.T) {
	// Test that LRU cache evicts items when capacity is reached
	cache := NewLRUCache(100)
	
	// Fill cache beyond capacity
	for i := 0; i < 150; i++ {
		key := fmt.Sprintf("key%d", i)
		cache.Put(key, []byte("data"), 1)
	}
	
	// Cache should have evicted old items to stay within capacity
	if cache.Len() > 100 {
		t.Errorf("Cache should have evicted items, got len=%d, want <=100", cache.Len())
	}
	
	// Oldest items should be evicted
	if _, found := cache.Get("key0"); found {
		t.Error("Oldest item should have been evicted")
	}
	
	// Newest items should still exist
	if _, found := cache.Get("key149"); !found {
		t.Error("Newest item should still exist")
	}
}

// TestBulkEviction tests the bulk eviction API
func TestBulkEviction(t *testing.T) {
	cache := NewLRUCache(1000)
	
	// Fill cache
	for i := 0; i < 500; i++ {
		key := fmt.Sprintf("key%d", i)
		cache.Put(key, []byte("test"), 4)
	}
	
	initialSize := cache.Len()
	if initialSize != 500 {
		t.Fatalf("Expected 500 items, got %d", initialSize)
	}
	
	// Evict 100 items
	evicted := cache.EvictOldest(100)
	if evicted != 100 {
		t.Errorf("Expected to evict 100, evicted %d", evicted)
	}
	
	finalSize := cache.Len()
	if finalSize != 400 {
		t.Errorf("Expected 400 items remaining, got %d", finalSize)
	}
}

// TestConcurrentMemoryAccess tests thread safety of memory operations
func TestConcurrentMemoryAccess(t *testing.T) {
	cache := NewLRUCache(10000)
	
	var wg sync.WaitGroup
	numGoroutines := 10
	opsPerGoroutine := 1000
	
	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Put(key, []byte("data"), 4)
			}
		}(i)
	}
	
	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				cache.Get(key)
			}
		}(i)
	}
	
	wg.Wait()
	
	// Should not panic or deadlock
}

// TestMemoryLeaks tests for memory leaks in LRU cache
func TestMemoryLeaks(t *testing.T) {
	cache := NewLRUCache(1000)
	
	// Get baseline memory
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	
	// Perform many operations
	for round := 0; round < 10; round++ {
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key%d", i)
			cache.Put(key, make([]byte, 1024), 1024)
		}
		cache.Clear()
	}
	
	// Force GC and check memory
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	
	// Memory should not grow excessively (allow 10MB growth)
	growth := int64(m2.Alloc) - int64(m1.Alloc)
	if growth > 10*1024*1024 {
		t.Errorf("Possible memory leak: grew %d bytes", growth)
	}
}

// BenchmarkMemoryEviction benchmarks the eviction performance
func BenchmarkMemoryEviction(b *testing.B) {
	cache := NewLRUCache(10000)
	
	// Fill cache
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key%d", i)
		cache.Put(key, []byte("data"), 4)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.EvictOldest(100)
		// Refill
		for j := 0; j < 100; j++ {
			key := fmt.Sprintf("key%d", 10000+i*100+j)
			cache.Put(key, []byte("data"), 4)
		}
	}
}
