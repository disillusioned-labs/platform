package kafka

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/kgo"
)

func IsTransientError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if errors.Is(err, kgo.ErrClientClosed) {
		return false
	}

	return kgo.IsRetryableBrokerErr(err)
}
