package resilience

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Table-driven unit tests ---

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 30*time.Second)

	tests := []struct {
		name      string
		fn        func() error
		wantErr   bool
		wantState State
	}{
		{
			name:      "successful call keeps circuit closed",
			fn:        func() error { return nil },
			wantErr:   false,
			wantState: StateClosed,
		},
		{
			name:      "first failure keeps circuit closed",
			fn:        func() error { return errors.New("fail") },
			wantErr:   true,
			wantState: StateClosed,
		},
		{
			name:      "second failure keeps circuit closed",
			fn:        func() error { return errors.New("fail") },
			wantErr:   true,
			wantState: StateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cb.Execute(tt.fn)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cb.State() != tt.wantState {
				t.Errorf("State() = %v, want %v", cb.State(), tt.wantState)
			}
		})
	}
}

// --- Failure tests (MOST IMPORTANT) ---

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 1*time.Second)
	fail := func() error { return errors.New("service down") }

	// Trip the breaker: 3 consecutive failures
	for i := 0; i < 3; i++ {
		cb.Execute(fail)
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after %d failures, got %v", 3, cb.State())
	}

	// Next call should be rejected immediately without executing fn
	called := false
	err := cb.Execute(func() error {
		called = true
		return nil
	})

	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
	if called {
		t.Error("function should NOT have been called when circuit is open")
	}
}

func TestCircuitBreaker_RecoveryAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 100*time.Millisecond) // short recovery for test
	fail := func() error { return errors.New("fail") }

	// Trip it
	cb.Execute(fail)
	cb.Execute(fail)

	if cb.State() != StateOpen {
		t.Fatal("expected open")
	}

	// Wait for recovery timeout
	time.Sleep(150 * time.Millisecond)

	// Next call should go through (half-open probe) and succeed
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("expected success in half-open, got %v", err)
	}

	// Should be back to closed
	if cb.State() != StateClosed {
		t.Errorf("expected StateClosed after recovery, got %v", cb.State())
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, 30*time.Second)

	// 2 failures
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	// 1 success — should reset counter
	cb.Execute(func() error { return nil })

	// 2 more failures — should NOT open (counter was reset)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	if cb.State() != StateClosed {
		t.Error("circuit should still be closed — success should have reset failure count")
	}
}

// --- Thundering herd prevention test ---

func TestCircuitBreaker_HalfOpenSingleProbe(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, 100*time.Millisecond)

	// Trip the breaker
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	// Wait for recovery
	time.Sleep(150 * time.Millisecond)

	// Launch 10 concurrent requests — only 1 should get through
	var callCount atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.Execute(func() error {
				callCount.Add(1)
				time.Sleep(50 * time.Millisecond) // simulate slow probe
				return nil
			})
		}()
	}

	wg.Wait()

	got := callCount.Load()
	if got != 1 {
		t.Errorf("expected exactly 1 probe request, got %d (thundering herd!)", got)
	}
}
