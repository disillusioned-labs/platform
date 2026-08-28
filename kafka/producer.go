package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Producer is the contract for publishing Kafka records. Services depend on
// this interface so tests can substitute a fake.
type Producer interface {
	Publish(ctx context.Context, record Record) error
}

// ClientProducer implements Producer using franz-go.
type ClientProducer struct {
	client *kgo.Client
}

// NewProducer creates a producer backed by an existing Kafka client.
//
// The Kafka client is shared so producers and consumers can use the same
// connection pool and metadata/cache.
func NewProducer(client *kgo.Client) *ClientProducer {
	return &ClientProducer{
		client: client,
	}
}

// Publish publishes one record and waits until Kafka acknowledges delivery.
//
// The caller's context controls cancellation. If the context is cancelled
// while the record is waiting for delivery, Publish returns the context error.
func (p *ClientProducer) Publish(
	ctx context.Context,
	record Record,
) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("kafka producer is not initialized")
	}

	if record.Topic == "" {
		return fmt.Errorf("kafka topic must not be empty")
	}

	kafkaRecord := &kgo.Record{
		Topic:   record.Topic,
		Key:     append([]byte(nil), record.Key...),
		Value:   append([]byte(nil), record.Value...),
		Headers: toKGOHeaders(record.Headers),
	}

	_, err := p.client.ProduceSync(ctx, kafkaRecord).First()
	if err != nil {
		return fmt.Errorf(
			"publish kafka record topic=%s: %w",
			record.Topic,
			err,
		)
	}

	return nil
}

func toKGOHeaders(headers []RecordHeader) []kgo.RecordHeader {
	if len(headers) == 0 {
		return nil
	}
	result := make([]kgo.RecordHeader, 0, len(headers))
	for _, header := range headers {
		result = append(result, kgo.RecordHeader{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		})
	}
	return result
}
