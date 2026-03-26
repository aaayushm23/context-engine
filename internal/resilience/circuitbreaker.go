package resilience

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed   State = iota // Closed circuit freely accepts and routes requests.
	StateOpen                  // Open circuit instantly sheds load to protect downstream.
	StateHalfOpen              // Half-open conditionally allows exactly one test probe.
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
	halfOpenProbing  bool // Serialization lock preventing thundering herd during recovery.
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
		// The halfOpenProbing flag prevents a "thundering herd" scenario when the circuit recovers.
		// If 100 requests arrive exactly when the timeout elapses, we only want ONE to test the waters.
		if time.Since(cb.lastFailure) > cb.recoveryTimeout {
			if cb.halfOpenProbing {
				// We intentionally fail-fast here rather than queuing, preserving system capacity.
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
		// Strict serialization during the testing phase: any request that isn't the assigned probe
		// immediately receives a cached failure to prevent overwhelming a recovering upstream.
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
			// If the single probe fails, we instantly collapse back to the Open state,
			// resetting the recovery timer without accumulating multiple failures.
			cb.state = StateOpen
			cb.halfOpenProbing = false
		} else if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
		}
		return err
	}

	// A successful execution (especially in HalfOpen) proves upstream stability,
	// allowing us to safely flush the failure counter and open the floodgates.
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
