package kafka

import "strings"

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
