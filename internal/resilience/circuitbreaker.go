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
	StateHalfOpen              // Testing recovery
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
			cb.state = StateHalfOpen
			cb.mu.Unlock()
			return cb.tryExecution(fn)
		}
		cb.mu.Unlock()
		return ErrCircuitOpen

	case StateHalfOpen:
		cb.mu.Unlock()
		return cb.tryExecution(fn)

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
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
		}
		return err
	}

	// Success — reset
	cb.failureCount = 0
	cb.state = StateClosed
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
