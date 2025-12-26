package signaling

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HealthCheckTimeout is the maximum time to wait for a health check
const HealthCheckTimeout = 5 * time.Second

// CheckRelayHealth verifies the relay server is reachable and responding
func CheckRelayHealth(relayURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), HealthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, relayURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	client := &http.Client{Timeout: HealthCheckTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("relay unreachable: %w", err)
	}
	defer resp.Body.Close()

	// Accept 200-299 or 404 (endpoint might not exist but server is up)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == 404 {
		// Health endpoint doesn't exist, but server responded - that's ok
		return nil
	}

	return fmt.Errorf("relay returned status %d", resp.StatusCode)
}

// Backoff provides exponential backoff with jitter for retries
type Backoff struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	current    time.Duration
	attempt    int
}

// DefaultBackoff returns a backoff starting at 1s, max 30s, multiplier 2x
func DefaultBackoff() *Backoff {
	return &Backoff{
		Initial:    1 * time.Second,
		Max:        30 * time.Second,
		Multiplier: 2.0,
		current:    1 * time.Second,
	}
}

// Next returns the next backoff duration
func (b *Backoff) Next() time.Duration {
	if b.attempt == 0 {
		b.current = b.Initial
	} else {
		b.current = time.Duration(float64(b.current) * b.Multiplier)
		if b.current > b.Max {
			b.current = b.Max
		}
	}
	b.attempt++

	// Add jitter (±10%)
	jitter := time.Duration(float64(b.current) * 0.1)
	return b.current + jitter
}

// Reset resets the backoff to initial state
func (b *Backoff) Reset() {
	b.current = b.Initial
	b.attempt = 0
}

// Attempt returns the current attempt number
func (b *Backoff) Attempt() int {
	return b.attempt
}

// RetryWithBackoff retries an operation with exponential backoff
func RetryWithBackoff(ctx context.Context, maxAttempts int, backoff *Backoff, op func() error) error {
	var lastErr error

	for i := 0; i < maxAttempts; i++ {
		if err := op(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if i < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff.Next()):
			}
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", maxAttempts, lastErr)
}
