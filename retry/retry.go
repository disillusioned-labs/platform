package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

var (
	ErrInvalidRetryAttempt = errors.New("invalid retry attempt")
)

type RetryPolicy struct {
	// MaxAttempts is the total number of provider attempts,
	// including the initial attempt.
	//
	// Example:
	//
	//	MaxAttempts = 1 -> initial attempt only
	//	MaxAttempts = 2 -> initial attempt + 1 retry
	//	MaxAttempts = 3 -> initial attempt + 2 retries
	MaxAttempts int

	InitialDelay time.Duration
	MaxDelay     time.Duration
	Jitter       time.Duration
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts <= 0 {
		return fmt.Errorf("max attempts must be greater than zero")
	}

	if p.InitialDelay < 0 {
		return fmt.Errorf("initial delay must not be negative")
	}

	if p.MaxDelay < 0 {
		return fmt.Errorf("max delay must not be negative")
	}

	if p.Jitter < 0 {
		return fmt.Errorf("jitter must not be negative")
	}

	if p.MaxDelay > 0 && p.InitialDelay > p.MaxDelay {
		return fmt.Errorf(
			"initial delay (%s) must be <= max delay (%s)",
			p.InitialDelay,
			p.MaxDelay,
		)
	}

	if p.MaxDelay > 0 && p.Jitter > p.MaxDelay {
		return fmt.Errorf(
			"jitter (%s) must be <= max delay (%s)",
			p.Jitter,
			p.MaxDelay,
		)
	}

	return nil
}

// RetryAllowed reports whether another provider attempt may be made.
//
// retryCount is the number of retries that have already been scheduled.
//
// Example:
//
//	MaxAttempts = 3
//
//	initial attempt:
//	retryCount = 0 -> allowed
//
//	after first failure:
//	retryCount = 1 -> allowed
//
//	after second failure:
//	retryCount = 2 -> allowed
//
//	after third failure:
//	retryCount = 3 -> not allowed
func (p RetryPolicy) RetryAllowed(retryCount int) bool {
	if retryCount < 0 {
		return false
	}

	return retryCount < p.MaxAttempts-1
}

// Delay returns the backoff duration for the given retry number.
//
// retryNumber:
//
//	1 -> InitialDelay
//	2 -> InitialDelay * 2
//	3 -> InitialDelay * 4
//
// The delay is capped at MaxDelay.
//
// Jitter is added after exponential backoff and the final
// result is capped again at MaxDelay.
//
// If InitialDelay is zero, the result is zero.
// If MaxDelay is zero, there is no maximum delay cap.
func (p RetryPolicy) Delay(retryNumber int) time.Duration {
	if retryNumber <= 0 {
		retryNumber = 1
	}

	if p.InitialDelay <= 0 {
		return 0
	}

	delay := p.InitialDelay

	for i := 1; i < retryNumber; i++ {
		if p.MaxDelay > 0 && delay >= p.MaxDelay {
			delay = p.MaxDelay
			break
		}

		// Prevent overflow while doubling.
		if delay > time.Duration(1<<62) {
			if p.MaxDelay > 0 {
				delay = p.MaxDelay
			} else {
				delay = time.Duration(1<<63 - 1)
			}

			break
		}

		delay *= 2

		if p.MaxDelay > 0 && delay >= p.MaxDelay {
			delay = p.MaxDelay
			break
		}
	}

	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	if p.Jitter > 0 {
		jitter := time.Duration(
			rand.Int64N(int64(p.Jitter) + 1),
		)

		maxDuration := time.Duration(1<<63 - 1)

		if jitter > 0 && delay > maxDuration-jitter {
			delay = maxDuration
		} else {
			delay += jitter
		}

		if p.MaxDelay > 0 && delay > p.MaxDelay {
			delay = p.MaxDelay
		}
	}

	return delay
}

// Wait blocks until the retry delay has elapsed or the context
// has been cancelled.
//
// This is useful for in-process retry loops.
//
// Notification delivery scheduling should normally use Delay()
// together with next_retry_at instead.
func (p RetryPolicy) Wait(
	ctx context.Context,
	retryNumber int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if retryNumber <= 0 {
		return ErrInvalidRetryAttempt
	}

	delay := p.Delay(retryNumber)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil

	case <-ctx.Done():
		return ctx.Err()
	}
}
