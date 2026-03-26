package resilience

import (
	"context"
	"math"
	"time"
)

// Retry encapsulates cross-cutting exponential backoff logic.
// By centralizing this, we ensure transient network blips don't immediately cascade
// into hard 500 errors for the end user while respecting downstream rate limits.
func Retry(ctx context.Context, maxAttempts int, baseSleep time.Duration, fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt < maxAttempts-1 {
			sleep := time.Duration(math.Pow(2, float64(attempt))) * baseSleep
			if sleep > 500*time.Millisecond {
				sleep = 500 * time.Millisecond
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleep):
			}
		}
	}
	return err
}
