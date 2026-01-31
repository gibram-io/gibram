// Package types - Critical path tests for session management
package types

import (
	"sync"
	"testing"
	"time"
)

// TestSessionQuotaEnforcement tests session-level quota checks
func TestSessionQuotaEnforcement(t *testing.T) {
	session := NewSession("test-session")

	// Set quotas
	session.SetQuotas(100, 200, 50, 1024*1024)

	// Test entity quota
	if err := session.CheckEntityQuota(50); err != nil {
		t.Errorf("Should allow 50 entities: %v", err)
	}

	session.IncrementEntity(50)

	if err := session.CheckEntityQuota(51); err == nil {
		t.Error("Should reject 51 more entities (total 101 > 100)")
	}

	// Test relationship quota
	if err := session.CheckRelationshipQuota(200); err != nil {
		t.Errorf("Should allow 200 relationships: %v", err)
	}

	session.IncrementRelationship(200)

	if err := session.CheckRelationshipQuota(1); err == nil {
		t.Error("Should reject 1 more relationship (total 201 > 200)")
	}

	// Test memory quota
	if err := session.CheckMemoryQuota(512 * 1024); err != nil {
		t.Errorf("Should allow 512KB: %v", err)
	}

	session.AddMemory(512 * 1024)

	if err := session.CheckMemoryQuota(513 * 1024); err == nil {
		t.Error("Should reject 513KB more (total > 1MB)")
	}
}

// TestNanosecondTTLPrecision tests nanosecond TTL accuracy
func TestNanosecondTTLPrecision(t *testing.T) {
	session := NewSession("test-session")

	// Set TTL to 100 milliseconds (in nanoseconds)
	ttl := int64(100 * time.Millisecond)
	session.SetTTL(ttl)

	// Should not be expired immediately
	if session.IsExpired() {
		t.Error("Session should not be expired immediately")
	}

	// Wait 150ms
	time.Sleep(150 * time.Millisecond)

	// Should be expired now
	if !session.IsExpired() {
		t.Error("Session should be expired after 150ms")
	}
}

// TestSessionConcurrentAccess tests thread safety
func TestSessionConcurrentAccess(t *testing.T) {
	session := NewSession("test-session")
	session.SetQuotas(10000, 10000, 10000, 100*1024*1024)

	var wg sync.WaitGroup
	numGoroutines := 10
	opsPerGoroutine := 100

	// Concurrent increment operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				session.IncrementEntity(1)
				session.IncrementRelationship(1)
				session.AddMemory(1024)
				session.Touch()
			}
		}()
	}

	wg.Wait()

	// Verify counts (use direct field access, not GetInfo which doesn't include counts)
	expectedCount := numGoroutines * opsPerGoroutine

	session.mu.RLock()
	entityCount := session.EntityCount
	relCount := session.RelationshipCount
	session.mu.RUnlock()

	if entityCount != expectedCount {
		t.Errorf("Expected %d entities, got %d", expectedCount, entityCount)
	}

	if relCount != expectedCount {
		t.Errorf("Expected %d relationships, got %d", expectedCount, relCount)
	}
}

// TestIdleTTL tests idle timeout functionality
func TestIdleTTL(t *testing.T) {
	session := NewSession("test-session")

	// Set idle TTL to 50ms
	idleTTL := int64(50 * time.Millisecond)
	session.SetIdleTTL(idleTTL)

	// Touch to reset idle timer
	session.Touch()

	// Wait 30ms and touch again (should reset)
	time.Sleep(30 * time.Millisecond)
	session.Touch()

	// Wait another 30ms (total 60ms from first touch, but only 30ms since last touch)
	time.Sleep(30 * time.Millisecond)

	// Should not be expired (touched 30ms ago)
	if session.IsExpired() {
		t.Error("Session should not be expired (touched 30ms ago)")
	}

	// Wait 25ms more without touching (total 55ms idle)
	time.Sleep(25 * time.Millisecond)

	// Should be expired now
	if !session.IsExpired() {
		t.Error("Session should be expired (55ms idle > 50ms limit)")
	}
}

// BenchmarkSessionQuotaCheck benchmarks quota checking performance
func BenchmarkSessionQuotaCheck(b *testing.B) {
	session := NewSession("bench-session")
	session.SetQuotas(1000000, 1000000, 1000000, 1024*1024*1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := session.CheckEntityQuota(1); err != nil {
			b.Fatalf("CheckEntityQuota() error: %v", err)
		}
		if err := session.CheckRelationshipQuota(1); err != nil {
			b.Fatalf("CheckRelationshipQuota() error: %v", err)
		}
		if err := session.CheckMemoryQuota(1024); err != nil {
			b.Fatalf("CheckMemoryQuota() error: %v", err)
		}
	}
}
