package kafka

import "context"

// Record is the application-facing Kafka record.
//
// The application layer must not depend on franz-go types. Kafka-specific
// implementation details are converted into this type at the platform
// boundary.
type Record struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   []RecordHeader

	// Context carries the trace context extracted from the record headers
	// (the "receive" span started by kotel). Processing code must continue
	// from it so consume-side spans join the producer's trace; it is nil for
	// records that arrived without a traceparent.
	Context context.Context
}

// RecordHeader is the application-facing Kafka record header.
type RecordHeader struct {
	Key   string
	Value []byte
}

func NewRecordHeader(key, value string) RecordHeader {
	return RecordHeader{
		Key:   key,
		Value: []byte(value),
	}
}

// Clone returns a defensive copy of the record.
//
// Kafka records may be backed by buffers owned by the Kafka client. Keeping
// independent copies prevents accidental use-after-fetch or mutation issues
// when records are passed to asynchronous processing.
func (r Record) Clone() Record {
	return Record{
		Topic:     r.Topic,
		Partition: r.Partition,
		Offset:    r.Offset,
		Key:       append([]byte(nil), r.Key...),
		Value:     append([]byte(nil), r.Value...),
		Headers:   cloneHeaders(r.Headers),
		Context:   r.Context,
	}
}

func cloneHeaders(headers []RecordHeader) []RecordHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make([]RecordHeader, 0, len(headers))
	for _, header := range headers {
		result = append(result, RecordHeader{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		})
	}
	return result
}
