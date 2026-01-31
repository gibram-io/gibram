// Package memory provides memory management for GibRAM
package memory

import (
	"runtime"
	"sync"
	"time"
)

// Manager manages memory usage across all caches
type Manager struct {
	config *Config

	// Caches
	entityCache    *LRUCache
	textUnitCache  *LRUCache
	documentCache  *LRUCache
	communityCache *LRUCache

	// Control
	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// NewManager creates a new memory manager
func NewManager(config *Config) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	m := &Manager{
		config:         config,
		stopCh:         make(chan struct{}),
		entityCache:    NewLRUCache(config.MaxItems / 4),
		textUnitCache:  NewLRUCache(config.MaxItems / 4),
		documentCache:  NewLRUCache(config.MaxItems / 4),
		communityCache: NewLRUCache(config.MaxItems / 4),
	}

	return m
}

// Start starts background memory management
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.monitorLoop()
}

// Stop stops the memory manager
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *Manager) monitorLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.TTLCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkMemoryPressure()
		}
	}
}

func (m *Manager) checkMemoryPressure() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	currentUsage := int64(memStats.Alloc)

	// Check if we're using too much memory
	if m.config.MaxMemoryBytes > 0 {
		usageRatio := float64(currentUsage) / float64(m.config.MaxMemoryBytes)

		if usageRatio >= 0.95 {
			// Critical: evict 20% immediately
			m.evictLRU(0.2)
		} else if usageRatio >= 0.85 {
			// Warning: evict 10%
			m.evictLRU(0.1)
		} else if usageRatio >= 0.75 {
			// Caution: evict 5%
			m.evictLRU(0.05)
		}
	}
}

func (m *Manager) evictLRU(fraction float64) {
	if fraction <= 0 || fraction > 1 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Calculate items to evict from each cache
	entityEvict := int(float64(m.entityCache.Len()) * fraction)
	textUnitEvict := int(float64(m.textUnitCache.Len()) * fraction)
	documentEvict := int(float64(m.documentCache.Len()) * fraction)
	communityEvict := int(float64(m.communityCache.Len()) * fraction)

	// Evict from each cache using bulk eviction
	m.entityCache.EvictOldest(entityEvict)
	m.textUnitCache.EvictOldest(textUnitEvict)
	m.documentCache.EvictOldest(documentEvict)
	m.communityCache.EvictOldest(communityEvict)
}

// GetEntityCache returns the entity cache
func (m *Manager) GetEntityCache() *LRUCache {
	return m.entityCache
}

// GetTextUnitCache returns the text unit cache
func (m *Manager) GetTextUnitCache() *LRUCache {
	return m.textUnitCache
}

// GetDocumentCache returns the document cache
func (m *Manager) GetDocumentCache() *LRUCache {
	return m.documentCache
}

// GetCommunityCache returns the community cache
func (m *Manager) GetCommunityCache() *LRUCache {
	return m.communityCache
}

// Stats returns memory manager statistics
func (m *Manager) Stats() MemoryStats {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	entityHits, entityMisses := m.entityCache.Stats()
	textUnitHits, textUnitMisses := m.textUnitCache.Stats()
	docHits, docMisses := m.documentCache.Stats()
	commHits, commMisses := m.communityCache.Stats()

	return MemoryStats{
		AllocatedBytes:    int64(memStats.Alloc),
		TotalAllocBytes:   int64(memStats.TotalAlloc),
		SystemBytes:       int64(memStats.Sys),
		NumGC:             memStats.NumGC,
		EntityCacheLen:    m.entityCache.Len(),
		TextUnitCacheLen:  m.textUnitCache.Len(),
		DocumentCacheLen:  m.documentCache.Len(),
		CommunityCacheLen: m.communityCache.Len(),
		CacheHits:         entityHits + textUnitHits + docHits + commHits,
		CacheMisses:       entityMisses + textUnitMisses + docMisses + commMisses,
	}
}

// MemoryStats holds memory statistics
type MemoryStats struct {
	AllocatedBytes    int64
	TotalAllocBytes   int64
	SystemBytes       int64
	NumGC             uint32
	EntityCacheLen    int
	TextUnitCacheLen  int
	DocumentCacheLen  int
	CommunityCacheLen int
	CacheHits         int64
	CacheMisses       int64
}
