// Package batch provides batch operation APIs for GibRAM
package batch

import (
	"fmt"
	"sync"

	"github.com/gibram-io/gibram/pkg/types"
)

// EntityBatch batches entity insertions for better performance
type EntityBatch struct {
	entities []*types.Entity
	mu       sync.Mutex
	maxSize  int
}

// NewEntityBatch creates a new entity batch
func NewEntityBatch(maxSize int) *EntityBatch {
	if maxSize <= 0 {
		maxSize = 1000
	}

	return &EntityBatch{
		entities: make([]*types.Entity, 0, maxSize),
		maxSize:  maxSize,
	}
}

// Add adds an entity to the batch
func (eb *EntityBatch) Add(entity *types.Entity) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.entities = append(eb.entities, entity)
}

// AddBulk adds multiple entities to the batch
func (eb *EntityBatch) AddBulk(entities []*types.Entity) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.entities = append(eb.entities, entities...)
}

// Size returns the current batch size
func (eb *EntityBatch) Size() int {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	return len(eb.entities)
}

// IsFull checks if the batch is full
func (eb *EntityBatch) IsFull() bool {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	return len(eb.entities) >= eb.maxSize
}

// Flush returns and clears the batch
func (eb *EntityBatch) Flush() []*types.Entity {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if len(eb.entities) == 0 {
		return nil
	}

	result := make([]*types.Entity, len(eb.entities))
	copy(result, eb.entities)
	eb.entities = eb.entities[:0]

	return result
}

// RelationshipBatch batches relationship insertions
type RelationshipBatch struct {
	relationships []*types.Relationship
	mu            sync.Mutex
	maxSize       int
}

// NewRelationshipBatch creates a new relationship batch
func NewRelationshipBatch(maxSize int) *RelationshipBatch {
	if maxSize <= 0 {
		maxSize = 1000
	}

	return &RelationshipBatch{
		relationships: make([]*types.Relationship, 0, maxSize),
		maxSize:       maxSize,
	}
}

// Add adds a relationship to the batch
func (rb *RelationshipBatch) Add(rel *types.Relationship) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.relationships = append(rb.relationships, rel)
}

// AddBulk adds multiple relationships to the batch
func (rb *RelationshipBatch) AddBulk(rels []*types.Relationship) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.relationships = append(rb.relationships, rels...)
}

// Size returns the current batch size
func (rb *RelationshipBatch) Size() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.relationships)
}

// IsFull checks if the batch is full
func (rb *RelationshipBatch) IsFull() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.relationships) >= rb.maxSize
}

// Flush returns and clears the batch
func (rb *RelationshipBatch) Flush() []*types.Relationship {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if len(rb.relationships) == 0 {
		return nil
	}

	result := make([]*types.Relationship, len(rb.relationships))
	copy(result, rb.relationships)
	rb.relationships = rb.relationships[:0]

	return result
}

// VectorBatch batches vector insertions for the HNSW index
type VectorBatch struct {
	vectors []VectorEntry
	mu      sync.Mutex
	maxSize int
}

// VectorEntry represents a vector to be inserted
type VectorEntry struct {
	ID     uint64
	Vector []float32
}

// NewVectorBatch creates a new vector batch
func NewVectorBatch(maxSize int) *VectorBatch {
	if maxSize <= 0 {
		maxSize = 1000
	}

	return &VectorBatch{
		vectors: make([]VectorEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a vector to the batch
func (vb *VectorBatch) Add(id uint64, vector []float32) {
	vb.mu.Lock()
	defer vb.mu.Unlock()
	vb.vectors = append(vb.vectors, VectorEntry{ID: id, Vector: vector})
}

// AddBulk adds multiple vectors to the batch
func (vb *VectorBatch) AddBulk(entries []VectorEntry) {
	vb.mu.Lock()
	defer vb.mu.Unlock()
	vb.vectors = append(vb.vectors, entries...)
}

// Size returns the current batch size
func (vb *VectorBatch) Size() int {
	vb.mu.Lock()
	defer vb.mu.Unlock()
	return len(vb.vectors)
}

// IsFull checks if the batch is full
func (vb *VectorBatch) IsFull() bool {
	vb.mu.Lock()
	defer vb.mu.Unlock()
	return len(vb.vectors) >= vb.maxSize
}

// Flush returns and clears the batch
func (vb *VectorBatch) Flush() []VectorEntry {
	vb.mu.Lock()
	defer vb.mu.Unlock()

	if len(vb.vectors) == 0 {
		return nil
	}

	result := make([]VectorEntry, len(vb.vectors))
	copy(result, vb.vectors)
	vb.vectors = vb.vectors[:0]

	return result
}

// BatchProcessor processes batches with automatic flushing
type BatchProcessor struct {
	entityBatch       *EntityBatch
	relationshipBatch *RelationshipBatch
	vectorBatch       *VectorBatch
	flushCallback     FlushCallback
	autoFlush         bool
	mu                sync.Mutex
}

// FlushCallback is called when batches are flushed
type FlushCallback struct {
	OnEntityFlush       func([]*types.Entity) error
	OnRelationshipFlush func([]*types.Relationship) error
	OnVectorFlush       func([]VectorEntry) error
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(maxSize int, autoFlush bool, callback FlushCallback) *BatchProcessor {
	return &BatchProcessor{
		entityBatch:       NewEntityBatch(maxSize),
		relationshipBatch: NewRelationshipBatch(maxSize),
		vectorBatch:       NewVectorBatch(maxSize),
		flushCallback:     callback,
		autoFlush:         autoFlush,
	}
}

// AddEntity adds an entity and flushes if batch is full
func (bp *BatchProcessor) AddEntity(entity *types.Entity) error {
	bp.entityBatch.Add(entity)

	if bp.autoFlush && bp.entityBatch.IsFull() {
		return bp.FlushEntities()
	}

	return nil
}

// AddRelationship adds a relationship and flushes if batch is full
func (bp *BatchProcessor) AddRelationship(rel *types.Relationship) error {
	bp.relationshipBatch.Add(rel)

	if bp.autoFlush && bp.relationshipBatch.IsFull() {
		return bp.FlushRelationships()
	}

	return nil
}

// AddVector adds a vector and flushes if batch is full
func (bp *BatchProcessor) AddVector(id uint64, vector []float32) error {
	bp.vectorBatch.Add(id, vector)

	if bp.autoFlush && bp.vectorBatch.IsFull() {
		return bp.FlushVectors()
	}

	return nil
}

// FlushEntities flushes the entity batch
func (bp *BatchProcessor) FlushEntities() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	entities := bp.entityBatch.Flush()
	if len(entities) == 0 {
		return nil
	}

	if bp.flushCallback.OnEntityFlush != nil {
		return bp.flushCallback.OnEntityFlush(entities)
	}

	return nil
}

// FlushRelationships flushes the relationship batch
func (bp *BatchProcessor) FlushRelationships() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	rels := bp.relationshipBatch.Flush()
	if len(rels) == 0 {
		return nil
	}

	if bp.flushCallback.OnRelationshipFlush != nil {
		return bp.flushCallback.OnRelationshipFlush(rels)
	}

	return nil
}

// FlushVectors flushes the vector batch
func (bp *BatchProcessor) FlushVectors() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	vectors := bp.vectorBatch.Flush()
	if len(vectors) == 0 {
		return nil
	}

	if bp.flushCallback.OnVectorFlush != nil {
		return bp.flushCallback.OnVectorFlush(vectors)
	}

	return nil
}

// FlushAll flushes all batches
func (bp *BatchProcessor) FlushAll() error {
	if err := bp.FlushEntities(); err != nil {
		return fmt.Errorf("entity flush failed: %w", err)
	}

	if err := bp.FlushRelationships(); err != nil {
		return fmt.Errorf("relationship flush failed: %w", err)
	}

	if err := bp.FlushVectors(); err != nil {
		return fmt.Errorf("vector flush failed: %w", err)
	}

	return nil
}

// Stats returns batch processor statistics
func (bp *BatchProcessor) Stats() BatchStats {
	return BatchStats{
		EntityBatchSize:       bp.entityBatch.Size(),
		RelationshipBatchSize: bp.relationshipBatch.Size(),
		VectorBatchSize:       bp.vectorBatch.Size(),
	}
}

// BatchStats holds batch processor statistics
type BatchStats struct {
	EntityBatchSize       int
	RelationshipBatchSize int
	VectorBatchSize       int
}
