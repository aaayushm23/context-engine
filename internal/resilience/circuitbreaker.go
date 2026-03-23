package resilience

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed   State = iota // Normal operation
	StateOpen                  // Failing, reject requests
	StateHalfOpen              // Testing recovery — only one probe allowed
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failureCount     int
	failureThreshold int
	recoveryTimeout  time.Duration
	lastFailure      time.Time
	name             string
	halfOpenProbing  bool // prevents thundering herd in half-open state
}

func NewCircuitBreaker(name string, threshold int, recovery time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: threshold,
		recoveryTimeout:  recovery,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()

	switch cb.state {
	case StateOpen:
		// Check if recovery timeout has elapsed
		if time.Since(cb.lastFailure) > cb.recoveryTimeout {
			// Only allow ONE probe request through (prevents thundering herd)
			if cb.halfOpenProbing {
				// Another goroutine is already probing — reject this one
				cb.mu.Unlock()
				return ErrCircuitOpen
			}
			cb.state = StateHalfOpen
			cb.halfOpenProbing = true
			cb.mu.Unlock()
			return cb.tryExecution(fn)
		}
		cb.mu.Unlock()
		return ErrCircuitOpen

	case StateHalfOpen:
		// A probe is already in progress — reject all other requests
		if cb.halfOpenProbing {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
		cb.mu.Unlock()
		return ErrCircuitOpen

	default: // Closed
		cb.mu.Unlock()
		return cb.tryExecution(fn)
	}
}

func (cb *CircuitBreaker) tryExecution(fn func() error) error {
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()
		if cb.state == StateHalfOpen {
			// Probe failed — back to open
			cb.state = StateOpen
			cb.halfOpenProbing = false
		} else if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
		}
		return err
	}

	// Success — reset to closed
	cb.failureCount = 0
	cb.state = StateClosed
	cb.halfOpenProbing = false
	return nil
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) Name() string {
	return cb.name
}
