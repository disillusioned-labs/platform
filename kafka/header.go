package kafka

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// HeaderCarrier adapts Kafka headers to the OpenTelemetry propagation
// TextMapCarrier interface.
type HeaderCarrier struct {
	headers *[]RecordHeader
}

func NewHeaderCarrier(headers *[]RecordHeader) *HeaderCarrier {
	return &HeaderCarrier{
		headers: headers,
	}
}

func (c *HeaderCarrier) Get(key string) string {
	if c == nil || c.headers == nil {
		return ""
	}

	for _, header := range *c.headers {
		if strings.EqualFold(header.Key, key) {
			return string(header.Value)
		}
	}

	return ""
}

func (c *HeaderCarrier) Set(key, value string) {
	if c == nil || c.headers == nil {
		return
	}

	*c.headers = append(*c.headers, RecordHeader{
		Key:   key,
		Value: []byte(value),
	})
}

func (c *HeaderCarrier) Keys() []string {
	if c == nil || c.headers == nil {
		return nil
	}

	keys := make([]string, 0, len(*c.headers))

	for _, header := range *c.headers {
		keys = append(keys, header.Key)
	}

	return keys
}

// HeaderValue returns the first header matching key.
func HeaderValue(
	headers []RecordHeader,
	key string,
) ([]byte, bool) {
	for _, header := range headers {
		if strings.EqualFold(header.Key, key) {
			return header.Value, true
		}
	}

	return nil, false
}

// HeaderString returns a Kafka header value as a string.
func HeaderString(
	headers []RecordHeader,
	key string,
) (string, bool) {
	value, ok := HeaderValue(headers, key)
	if !ok {
		return "", false
	}

	return string(value), true
}

// RequiredHeader returns a Kafka header value or an error if missing or empty.
func RequiredHeader(headers []RecordHeader, key string) (string, error) {
	value, ok := HeaderString(headers, key)
	if !ok || value == "" {
		return "", fmt.Errorf("missing required header %q", key)
	}
	return value, nil
}

// RequiredUUIDHeader returns a Kafka header value parsed as a UUID.
func RequiredUUIDHeader(headers []RecordHeader, key string) (uuid.UUID, error) {
	value, err := RequiredHeader(headers, key)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid header %q: %w", key, err)
	}
	return id, nil
}

// RequiredIntHeader returns a Kafka header value parsed as an integer.
func RequiredIntHeader(headers []RecordHeader, key string) (int, error) {
	value, err := RequiredHeader(headers, key)
	if err != nil {
		return 0, err
	}
	version, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid header %q: %w", key, err)
	}
	return version, nil
}

// OptionalHeader returns a Kafka header value as *string, nil if missing.
func OptionalHeader(headers []RecordHeader, key string) *string {
	value, ok := HeaderString(headers, key)
	if !ok || value == "" {
		return nil
	}
	return &value
}
