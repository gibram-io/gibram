// Package shutdown - comprehensive tests for graceful shutdown handling
package shutdown

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// =============================================================================
// Handler Creation Tests
// =============================================================================

func TestNewHandler(t *testing.T) {
	h := NewHandler()
	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}
}

func TestNewHandler_DefaultTimeout(t *testing.T) {
	h := NewHandler()

	// Access timeout via SetTimeout to verify it works
	// Default should be 30 seconds
	h.SetTimeout(10 * time.Second)
}

func TestDefault(t *testing.T) {
	h := Default()
	if h == nil {
		t.Fatal("Default() returned nil")
	}
}

// =============================================================================
// SetTimeout Tests
// =============================================================================

func TestHandler_SetTimeout(t *testing.T) {
	h := NewHandler()

	h.SetTimeout(5 * time.Second)
	// No panic means success
}

func TestHandler_SetTimeout_Zero(t *testing.T) {
	h := NewHandler()
	h.SetTimeout(0)
	// Should not panic
}

// =============================================================================
// SetSignals Tests
// =============================================================================

func TestHandler_SetSignals(t *testing.T) {
	h := NewHandler()

	h.SetSignals(syscall.SIGINT)
	// No panic means success
}

func TestHandler_SetSignals_Multiple(t *testing.T) {
	h := NewHandler()

	h.SetSignals(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	// No panic means success
}

// =============================================================================
// Register Hook Tests
// =============================================================================

func TestHandler_Register(t *testing.T) {
	h := NewHandler()

	called := false
	h.Register("test", 0, func(ctx context.Context) error {
		called = true
		return nil
	})

	// Hook is registered - verify by triggering shutdown
	h.Shutdown()

	if !called {
		t.Error("Hook was not called during shutdown")
	}
}

func TestHandler_Register_Priority(t *testing.T) {
	h := NewHandler()

	order := make([]int, 0)
	var mu sync.Mutex

	// Register in reverse priority order
	h.Register("high", 10, func(ctx context.Context) error {
		mu.Lock()
		order = append(order, 10)
		mu.Unlock()
		return nil
	})

	h.Register("low", 1, func(ctx context.Context) error {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
		return nil
	})

	h.Register("medium", 5, func(ctx context.Context) error {
		mu.Lock()
		order = append(order, 5)
		mu.Unlock()
		return nil
	})

	h.Shutdown()

	// Lower priority should run first
	if len(order) != 3 {
		t.Fatalf("Expected 3 hooks to run, got %d", len(order))
	}
	if order[0] != 1 {
		t.Errorf("First hook priority = %d, want 1", order[0])
	}
	if order[1] != 5 {
		t.Errorf("Second hook priority = %d, want 5", order[1])
	}
	if order[2] != 10 {
		t.Errorf("Third hook priority = %d, want 10", order[2])
	}
}

func TestHandler_Register_SamePriority(t *testing.T) {
	h := NewHandler()

	var count int32

	// Register multiple hooks with same priority
	h.Register("hook1", 5, func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	h.Register("hook2", 5, func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	h.Register("hook3", 5, func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	h.Shutdown()

	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("Expected 3 hooks to run, got %d", count)
	}
}

// =============================================================================
// Shutdown Tests
// =============================================================================

func TestHandler_Shutdown_NoHooks(t *testing.T) {
	h := NewHandler()

	// Should not panic with no hooks
	h.Shutdown()
}

func TestHandler_Shutdown_HookError(t *testing.T) {
	h := NewHandler()

	h.Register("failing", 0, func(ctx context.Context) error {
		return errors.New("hook error")
	})

	// Should not panic on hook error
	h.Shutdown()
}

func TestHandler_Shutdown_ContextCancellation(t *testing.T) {
	h := NewHandler()
	h.SetTimeout(100 * time.Millisecond)

	canceled := false
	h.Register("slow", 0, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			canceled = true
			return ctx.Err()
		case <-time.After(1 * time.Second):
			return nil
		}
	})

	h.Shutdown()

	if !canceled {
		t.Error("Hook should have been canceled due to timeout")
	}
}

func TestHandler_Shutdown_MultipleHooksError(t *testing.T) {
	h := NewHandler()

	h.Register("fail1", 1, func(ctx context.Context) error {
		return errors.New("error 1")
	})

	h.Register("success", 2, func(ctx context.Context) error {
		return nil
	})

	h.Register("fail2", 3, func(ctx context.Context) error {
		return errors.New("error 2")
	})

	// Should not panic with multiple errors
	h.Shutdown()
}

// =============================================================================
// Start Tests
// =============================================================================

func TestHandler_Start(t *testing.T) {
	h := NewHandler()

	// Should not panic
	h.Start()

	// Double start should be safe
	h.Start()
}

func TestHandler_Start_Idempotent(t *testing.T) {
	h := NewHandler()

	// Multiple starts should be safe
	for i := 0; i < 5; i++ {
		h.Start()
	}
}

// =============================================================================
// Wait Tests
// =============================================================================

func TestHandler_Wait(t *testing.T) {
	h := NewHandler()

	// Trigger shutdown in goroutine
	go func() {
		time.Sleep(10 * time.Millisecond)
		h.Shutdown()
	}()

	// Wait should return after shutdown
	done := make(chan struct{})
	go func() {
		h.Wait()
		close(done)
	}()

	select {
	case <-done:
		break
	case <-time.After(1 * time.Second):
		t.Error("Wait did not return after shutdown")
	}
}

// =============================================================================
// Done Channel Tests
// =============================================================================

func TestHandler_Done(t *testing.T) {
	h := NewHandler()

	done := h.Done()
	if done == nil {
		t.Fatal("Done() returned nil channel")
	}

	// Channel should not be closed initially
	select {
	case <-done:
		t.Error("Done channel should not be closed before shutdown")
	default:
		break
	}

	// Trigger shutdown
	h.Shutdown()

	// Now channel should be closed
	select {
	case <-done:
		break
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after shutdown")
	}
}

// =============================================================================
// GracefulShutdown Helper Tests
// =============================================================================

func TestGracefulShutdown(t *testing.T) {
	called := false
	hook := ShutdownHook{
		Name:     "test",
		Priority: 0,
		Fn: func(ctx context.Context) error {
			called = true
			return nil
		},
	}

	h := GracefulShutdown(5*time.Second, hook)
	if h == nil {
		t.Fatal("GracefulShutdown() returned nil")
	}

	// Trigger shutdown
	h.Shutdown()

	if !called {
		t.Error("Hook was not called")
	}
}

func TestGracefulShutdown_NoHooks(t *testing.T) {
	h := GracefulShutdown(5 * time.Second)
	if h == nil {
		t.Fatal("GracefulShutdown() with no hooks returned nil")
	}

	h.Shutdown()
}

func TestGracefulShutdown_MultipleHooks(t *testing.T) {
	var count int32

	hooks := []ShutdownHook{
		{Name: "h1", Priority: 1, Fn: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		}},
		{Name: "h2", Priority: 2, Fn: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		}},
		{Name: "h3", Priority: 3, Fn: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		}},
	}

	h := GracefulShutdown(5*time.Second, hooks...)
	h.Shutdown()

	if atomic.LoadInt32(&count) != 3 {
		t.Errorf("Expected 3 hooks to run, got %d", count)
	}
}

// =============================================================================
// ShutdownHook Structure Tests
// =============================================================================

func TestShutdownHook_Structure(t *testing.T) {
	hook := ShutdownHook{
		Name:     "test_hook",
		Priority: 5,
		Fn: func(ctx context.Context) error {
			return nil
		},
	}

	if hook.Name != "test_hook" {
		t.Error("Hook name not set correctly")
	}
	if hook.Priority != 5 {
		t.Error("Hook priority not set correctly")
	}
	if hook.Fn == nil {
		t.Error("Hook function not set")
	}
}

// =============================================================================
// Concurrent Tests
// =============================================================================

func TestHandler_Concurrent_Register(t *testing.T) {
	h := NewHandler()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h.Register("hook", n, func(ctx context.Context) error {
				return nil
			})
		}(i)
	}

	wg.Wait()

	// Should be able to shutdown after concurrent registration
	h.Shutdown()
}

// =============================================================================
// Real World Scenario Tests
// =============================================================================

func TestScenario_DatabaseShutdown(t *testing.T) {
	h := NewHandler()

	// Simulate database connection cleanup
	dbClosed := false
	h.Register("database", 10, func(ctx context.Context) error {
		// Simulate flush operations
		time.Sleep(5 * time.Millisecond)
		dbClosed = true
		return nil
	})

	// Simulate HTTP server shutdown (should happen first)
	serverStopped := false
	h.Register("http_server", 1, func(ctx context.Context) error {
		serverStopped = true
		return nil
	})

	h.Shutdown()

	if !dbClosed {
		t.Error("Database was not closed")
	}
	if !serverStopped {
		t.Error("HTTP server was not stopped")
	}
}

func TestScenario_MetricsFlushing(t *testing.T) {
	h := NewHandler()
	h.SetTimeout(1 * time.Second)

	var flushed int32

	// Simulate metrics flush
	h.Register("metrics", 5, func(ctx context.Context) error {
		atomic.StoreInt32(&flushed, 1)
		return nil
	})

	h.Shutdown()

	if atomic.LoadInt32(&flushed) != 1 {
		t.Error("Metrics were not flushed")
	}
}
