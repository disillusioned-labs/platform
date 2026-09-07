package grpc

import (
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
)

type RetryPolicy struct {
	ServiceName string
	MethodName  string

	MaxAttempts uint32

	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64

	RetryableCodes []codes.Code
}

type retryServiceConfig struct {
	MethodConfig []methodConfig `json:"methodConfig"`
}

type methodConfig struct {
	Name        []methodName `json:"name"`
	RetryPolicy retryPolicy  `json:"retryPolicy"`
}

type methodName struct {
	Service string `json:"service"`
	Method  string `json:"method"`
}

type retryPolicy struct {
	MaxAttempts          uint32   `json:"maxAttempts"`
	InitialBackoff       string   `json:"initialBackoff"`
	MaxBackoff           string   `json:"maxBackoff"`
	BackoffMultiplier    float64  `json:"backoffMultiplier"`
	RetryableStatusCodes []string `json:"retryableStatusCodes"`
}

func NewRetryServiceConfig(policy RetryPolicy) (string, error) {
	if policy.ServiceName == "" {
		return "", fmt.Errorf("grpc: retry service name is required")
	}

	if policy.MethodName == "" {
		return "", fmt.Errorf("grpc: retry method name is required")
	}

	if policy.MaxAttempts < 2 {
		return "", fmt.Errorf("grpc: max attempts must be >= 2")
	}

	if policy.InitialBackoff <= 0 {
		return "", fmt.Errorf("grpc: initial backoff must be > 0")
	}

	if policy.MaxBackoff <= 0 {
		return "", fmt.Errorf("grpc: max backoff must be > 0")
	}

	if policy.MaxBackoff < policy.InitialBackoff {
		return "", fmt.Errorf("grpc: max backoff must be >= initial backoff")
	}

	if policy.BackoffMultiplier < 1 {
		return "", fmt.Errorf("grpc: backoff multiplier must be > 0")
	}

	if len(policy.RetryableCodes) == 0 {
		return "", fmt.Errorf("grpc: at least one retryable status code is required")
	}

	codesList := make([]string, 0, len(policy.RetryableCodes))

	for _, code := range policy.RetryableCodes {
		codesList = append(codesList, code.String())
	}

	config := retryServiceConfig{
		MethodConfig: []methodConfig{
			{
				Name: []methodName{
					{
						Service: policy.ServiceName,
						Method:  policy.MethodName,
					},
				},
				RetryPolicy: retryPolicy{
					MaxAttempts:          policy.MaxAttempts,
					InitialBackoff:       policy.InitialBackoff.String(),
					MaxBackoff:           policy.MaxBackoff.String(),
					BackoffMultiplier:    policy.BackoffMultiplier,
					RetryableStatusCodes: codesList,
				},
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("grpc: marshal retry config: %w", err)
	}

	return string(data), nil
}
