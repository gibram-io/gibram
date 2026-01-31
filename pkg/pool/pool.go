// Package pool provides memory pools for GibRAM to reduce allocations
package pool

import (
	"sync"
)

// VectorPool manages reusable float32 slices for vectors
type VectorPool struct {
	pools map[int]*sync.Pool // keyed by dimension
	mu    sync.RWMutex
}

// NewVectorPool creates a new vector pool
func NewVectorPool() *VectorPool {
	return &VectorPool{
		pools: make(map[int]*sync.Pool),
	}
}

// Get retrieves a vector from the pool (or allocates a new one)
func (vp *VectorPool) Get(dimension int) []float32 {
	vp.mu.RLock()
	pool, ok := vp.pools[dimension]
	vp.mu.RUnlock()

	if !ok {
		// Create pool for this dimension
		vp.mu.Lock()
		// Double-check after acquiring write lock
		pool, ok = vp.pools[dimension]
		if !ok {
			pool = &sync.Pool{
				New: func() interface{} {
					vec := make([]float32, dimension)
					return &vec
				},
			}
			vp.pools[dimension] = pool
		}
		vp.mu.Unlock()
	}

	vecPtr := pool.Get().(*[]float32)
	vec := *vecPtr
	// Clear the vector before reuse
	for i := range vec {
		vec[i] = 0
	}
	return vec
}

// Put returns a vector to the pool for reuse
func (vp *VectorPool) Put(vec []float32) {
	dimension := len(vec)
	vp.mu.RLock()
	pool, ok := vp.pools[dimension]
	vp.mu.RUnlock()

	if ok {
		v := vec
		pool.Put(&v)
	}
}

// BufferPool manages reusable byte slices
type BufferPool struct {
	small  *sync.Pool // < 4KB
	medium *sync.Pool // 4KB - 64KB
	large  *sync.Pool // 64KB - 1MB
}

// NewBufferPool creates a new buffer pool
func NewBufferPool() *BufferPool {
	return &BufferPool{
		small: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 4*1024) // 4KB
				return &buf
			},
		},
		medium: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 64*1024) // 64KB
				return &buf
			},
		},
		large: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 1024*1024) // 1MB
				return &buf
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get(size int) []byte {
	var pool *sync.Pool
	var defaultSize int

	switch {
	case size <= 4*1024:
		pool = bp.small
		defaultSize = 4 * 1024
	case size <= 64*1024:
		pool = bp.medium
		defaultSize = 64 * 1024
	case size <= 1024*1024:
		pool = bp.large
		defaultSize = 1024 * 1024
	default:
		// Too large for pooling, allocate directly
		return make([]byte, size)
	}

	bufPtr := pool.Get().(*[]byte)
	buf := *bufPtr

	// Ensure buffer is large enough
	if len(buf) < size {
		buf = make([]byte, defaultSize)
	}

	return buf[:size]
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf []byte) {
	capacity := cap(buf)

	// Determine which pool this belongs to
	var pool *sync.Pool
	switch {
	case capacity <= 4*1024:
		pool = bp.small
	case capacity <= 64*1024:
		pool = bp.medium
	case capacity <= 1024*1024:
		pool = bp.large
	default:
		// Too large, don't pool it
		return
	}

	// Reset length to capacity before returning
	buf = buf[:cap(buf)]
	pool.Put(&buf)
}

// NodePool manages reusable HNSW nodes
type NodePool struct {
	pool *sync.Pool
}

// NewNodePool creates a new node pool
func NewNodePool() *NodePool {
	return &NodePool{
		pool: &sync.Pool{
			New: func() interface{} {
				return &HNSWNode{}
			},
		},
	}
}

// Get retrieves a node from the pool
func (np *NodePool) Get() *HNSWNode {
	node := np.pool.Get().(*HNSWNode)
	// Reset node state
	node.id = 0
	node.vector = nil
	node.level = 0
	node.friends = nil
	return node
}

// Put returns a node to the pool
func (np *NodePool) Put(node *HNSWNode) {
	// Clear references to help GC
	node.vector = nil
	node.friends = nil
	np.pool.Put(node)
}

// HNSWNode is a simplified node structure for pooling
type HNSWNode struct {
	id      uint64
	vector  []float32
	level   int
	friends [][]*HNSWNode
}

// QueryResultPool manages reusable query result structures
type QueryResultPool struct {
	pool *sync.Pool
}

// NewQueryResultPool creates a new query result pool
func NewQueryResultPool() *QueryResultPool {
	return &QueryResultPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return &QueryResult{
					Entities:      make([]EntityResult, 0, 100),
					Relationships: make([]RelationshipResult, 0, 100),
					Communities:   make([]CommunityResult, 0, 10),
				}
			},
		},
	}
}

// Get retrieves a query result from the pool
func (qp *QueryResultPool) Get() *QueryResult {
	result := qp.pool.Get().(*QueryResult)
	// Reset slices
	result.Entities = result.Entities[:0]
	result.Relationships = result.Relationships[:0]
	result.Communities = result.Communities[:0]
	return result
}

// Put returns a query result to the pool
func (qp *QueryResultPool) Put(result *QueryResult) {
	qp.pool.Put(result)
}

// QueryResult is a simplified result structure for pooling
type QueryResult struct {
	Entities      []EntityResult
	Relationships []RelationshipResult
	Communities   []CommunityResult
}

type EntityResult struct {
	ID    uint64
	Score float32
}

type RelationshipResult struct {
	ID    uint64
	Score float32
}

type CommunityResult struct {
	ID    uint64
	Score float32
}

// Global pools (can be initialized once and shared)
var (
	DefaultVectorPool      = NewVectorPool()
	DefaultBufferPool      = NewBufferPool()
	DefaultNodePool        = NewNodePool()
	DefaultQueryResultPool = NewQueryResultPool()
)
