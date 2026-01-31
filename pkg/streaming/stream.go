// Package streaming provides streaming capabilities for large result sets
package streaming

import (
	"context"
	"errors"
	"sync"

	"github.com/gibram-io/gibram/pkg/types"
)

var (
	ErrStreamClosed = errors.New("stream closed")
	ErrBufferFull   = errors.New("stream buffer full")
)

// EntityStream streams entities incrementally
type EntityStream struct {
	ch       chan *types.Entity
	errCh    chan error
	doneCh   chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	closed   bool
	mu       sync.Mutex
	buffSize int
}

// NewEntityStream creates a new entity stream
func NewEntityStream(ctx context.Context, bufferSize int) *EntityStream {
	if bufferSize <= 0 {
		bufferSize = 100
	}

	streamCtx, cancel := context.WithCancel(ctx)

	return &EntityStream{
		ch:       make(chan *types.Entity, bufferSize),
		errCh:    make(chan error, 1),
		doneCh:   make(chan struct{}),
		ctx:      streamCtx,
		cancel:   cancel,
		buffSize: bufferSize,
	}
}

// Send sends an entity to the stream (non-blocking with context)
func (s *EntityStream) Send(entity *types.Entity) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStreamClosed
	}
	s.mu.Unlock()

	select {
	case s.ch <- entity:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Recv receives an entity from the stream
func (s *EntityStream) Recv() (*types.Entity, error) {
	select {
	case entity, ok := <-s.ch:
		if !ok {
			// Check for error
			select {
			case err := <-s.errCh:
				return nil, err
			default:
				return nil, nil // Clean EOF
			}
		}
		return entity, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Close closes the stream
func (s *EntityStream) Close(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.ch)

	if err != nil {
		select {
		case s.errCh <- err:
			break
		default:
			break
		}
	}

	close(s.doneCh)
	s.cancel()
}

// RelationshipStream streams relationships incrementally
type RelationshipStream struct {
	ch       chan *types.Relationship
	errCh    chan error
	doneCh   chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	closed   bool
	mu       sync.Mutex
	buffSize int
}

// NewRelationshipStream creates a new relationship stream
func NewRelationshipStream(ctx context.Context, bufferSize int) *RelationshipStream {
	if bufferSize <= 0 {
		bufferSize = 100
	}

	streamCtx, cancel := context.WithCancel(ctx)

	return &RelationshipStream{
		ch:       make(chan *types.Relationship, bufferSize),
		errCh:    make(chan error, 1),
		doneCh:   make(chan struct{}),
		ctx:      streamCtx,
		cancel:   cancel,
		buffSize: bufferSize,
	}
}

// Send sends a relationship to the stream
func (s *RelationshipStream) Send(rel *types.Relationship) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStreamClosed
	}
	s.mu.Unlock()

	select {
	case s.ch <- rel:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Recv receives a relationship from the stream
func (s *RelationshipStream) Recv() (*types.Relationship, error) {
	select {
	case rel, ok := <-s.ch:
		if !ok {
			select {
			case err := <-s.errCh:
				return nil, err
			default:
				return nil, nil // Clean EOF
			}
		}
		return rel, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Close closes the stream
func (s *RelationshipStream) Close(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.ch)

	if err != nil {
		select {
		case s.errCh <- err:
			break
		default:
			break
		}
	}

	close(s.doneCh)
	s.cancel()
}

// VectorResultStream streams vector search results incrementally
type VectorResultStream struct {
	ch       chan VectorResult
	errCh    chan error
	doneCh   chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	closed   bool
	mu       sync.Mutex
	buffSize int
}

// VectorResult represents a single vector search result
type VectorResult struct {
	ID    uint64
	Score float32
}

// NewVectorResultStream creates a new vector result stream
func NewVectorResultStream(ctx context.Context, bufferSize int) *VectorResultStream {
	if bufferSize <= 0 {
		bufferSize = 100
	}

	streamCtx, cancel := context.WithCancel(ctx)

	return &VectorResultStream{
		ch:       make(chan VectorResult, bufferSize),
		errCh:    make(chan error, 1),
		doneCh:   make(chan struct{}),
		ctx:      streamCtx,
		cancel:   cancel,
		buffSize: bufferSize,
	}
}

// Send sends a result to the stream
func (s *VectorResultStream) Send(result VectorResult) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrStreamClosed
	}
	s.mu.Unlock()

	select {
	case s.ch <- result:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Recv receives a result from the stream
func (s *VectorResultStream) Recv() (VectorResult, error) {
	select {
	case result, ok := <-s.ch:
		if !ok {
			select {
			case err := <-s.errCh:
				return VectorResult{}, err
			default:
				return VectorResult{}, nil // Clean EOF
			}
		}
		return result, nil
	case <-s.ctx.Done():
		return VectorResult{}, s.ctx.Err()
	}
}

// Close closes the stream
func (s *VectorResultStream) Close(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	close(s.ch)

	if err != nil {
		select {
		case s.errCh <- err:
			break
		default:
			break
		}
	}

	close(s.doneCh)
	s.cancel()
}

// BatchWriter batches stream writes for efficiency
type BatchWriter struct {
	stream   interface{} // One of the stream types
	batch    []interface{}
	maxBatch int
	mu       sync.Mutex
}

// NewBatchWriter creates a batch writer
func NewBatchWriter(stream interface{}, maxBatch int) *BatchWriter {
	if maxBatch <= 0 {
		maxBatch = 10
	}

	return &BatchWriter{
		stream:   stream,
		batch:    make([]interface{}, 0, maxBatch),
		maxBatch: maxBatch,
	}
}

// Add adds an item to the batch (flushes when full)
func (bw *BatchWriter) Add(item interface{}) error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	bw.batch = append(bw.batch, item)

	if len(bw.batch) >= bw.maxBatch {
		return bw.flushLocked()
	}

	return nil
}

// Flush flushes the current batch
func (bw *BatchWriter) Flush() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.flushLocked()
}

func (bw *BatchWriter) flushLocked() error {
	if len(bw.batch) == 0 {
		return nil
	}

	// Type switch on stream type and send items
	switch s := bw.stream.(type) {
	case *EntityStream:
		for _, item := range bw.batch {
			if entity, ok := item.(*types.Entity); ok {
				if err := s.Send(entity); err != nil {
					return err
				}
			}
		}
	case *RelationshipStream:
		for _, item := range bw.batch {
			if rel, ok := item.(*types.Relationship); ok {
				if err := s.Send(rel); err != nil {
					return err
				}
			}
		}
	case *VectorResultStream:
		for _, item := range bw.batch {
			if result, ok := item.(VectorResult); ok {
				if err := s.Send(result); err != nil {
					return err
				}
			}
		}
	}

	bw.batch = bw.batch[:0]
	return nil
}
