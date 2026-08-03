package recoverutil

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGuardRecoverPanic verifies that Guard catches a panic and does not
// propagate it to the caller.
func TestGuardRecoverPanic(t *testing.T) {
	t.Parallel()

	didNotPanic := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				didNotPanic = false
			}
		}()
		Guard("test-panic", func() {
			panic("boom")
		})
	}()

	if !didNotPanic {
		t.Fatal("Guard should have swallowed the panic but it propagated")
	}
}

// TestGuardNormalExecution verifies that Guard runs the function normally
// when no panic occurs.
func TestGuardNormalExecution(t *testing.T) {
	t.Parallel()

	var ran bool
	Guard("test-normal", func() {
		ran = true
	})

	if !ran {
		t.Fatal("Guard did not execute the wrapped function")
	}
}

// TestGuardGoRunsConcurrently verifies that GuardGo runs the function in a
// separate goroutine and that the panic in it does not crash the test.
func TestGuardGoRunsConcurrently(t *testing.T) {
	t.Parallel()

	var ran atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)

	GuardGo("test-goroutine", func() {
		defer wg.Done()
		ran.Store(1)
	})

	wg.Wait()
	if ran.Load() != 1 {
		t.Fatal("GuardGo did not run the function in a goroutine")
	}
}

// TestGuardGoRecoverPanic verifies that a panic in a GuardGo goroutine is
// recovered and does not crash the test process.
func TestGuardGoRecoverPanic(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	wg.Add(1)

	// Use a channel to ensure the goroutine actually ran and panicked.
	done := make(chan struct{})

	GuardGo("test-goroutine-panic", func() {
		defer wg.Done()
		close(done)
		panic("boom in goroutine")
	})

	// Wait for the goroutine to signal it ran (before the panic).
	select {
	case <-done:
		// Good — the function ran.
	case <-time.After(2 * time.Second):
		t.Fatal("GuardGo goroutine did not run within 2s")
	}

	// wg.Done() was called via defer, so this should not hang.
	wg.Wait()
}

// TestHandlerReturnsCallable verifies that Handler returns a non-nil
// function that can be called.
func TestHandlerReturnsCallable(t *testing.T) {
	t.Parallel()

	var ran bool
	h := Handler("test-handler", func() {
		ran = true
	})
	if h == nil {
		t.Fatal("Handler returned nil")
	}
	h()
	if !ran {
		t.Fatal("Handler wrapper did not call the wrapped function")
	}
}

// TestHandlerRecoversPanic verifies that Handler catches a panic.
func TestHandlerRecoversPanic(t *testing.T) {
	t.Parallel()

	h := Handler("test-handler-panic", func() {
		panic("handler boom")
	})

	didNotPanic := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				didNotPanic = false
			}
		}()
		h()
	}()

	if !didNotPanic {
		t.Fatal("Handler should have swallowed the panic but it propagated")
	}
}

// TestHandler2RecoversPanic verifies that Handler2 catches a panic.
func TestHandler2RecoversPanic(t *testing.T) {
	t.Parallel()

	h := Handler2("test-handler2-panic", func(s any, a int) {
		panic("handler2 boom")
	})

	didNotPanic := true
	func() {
		defer func() {
			if r := recover(); r != nil {
				didNotPanic = false
			}
		}()
		h(nil, 42)
	}()

	if !didNotPanic {
		t.Fatal("Handler2 should have swallowed the panic but it propagated")
	}
}

// TestGuardLoop50Pass runs Guard with a panicking function 50 times
// consecutively to verify reliability under repeated stress — matching the
// 50-pass protocol for shared/concurrent state.
func TestGuardLoop50Pass(t *testing.T) {
	t.Parallel()

	var survived int32
	for i := 0; i < 50; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("iteration %d: panic escaped Guard: %v", i, r)
				}
			}()
			Guard("test-loop-50", func() {
				if i%5 == 0 {
					panic("simulated panic")
				}
				atomic.AddInt32(&survived, 1)
			})
		}()
		if t.Failed() {
			return
		}
	}

	// Non-panic iterations: i not divisible by 5 → 40 iterations.
	if got := atomic.LoadInt32(&survived); got != 40 {
		t.Fatalf("expected 40 normal executions, got %d", got)
	}
}

// TestGuardGoLoop50Pass launches 50 goroutines that each panic, verifying
// that none of them crash the process and all are recovered.
func TestGuardGoLoop50Pass(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	var ranCount atomic.Int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		GuardGo("test-goloop-50", func() {
			defer wg.Done()
			ranCount.Add(1)
			panic("concurrent boom")
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All 50 goroutines completed despite panicking.
	case <-time.After(5 * time.Second):
		t.Fatal("not all 50 goroutines completed within 5s — some may have hung")
	}

	if got := ranCount.Load(); got != 50 {
		t.Fatalf("expected 50 goroutines to have run, got %d", got)
	}
}
