package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Consumer implements polling and offset commits using franz-go.
type Consumer struct {
	client *kgo.Client
}

// Consumer-group configuration belongs to the Kafka client configuration.
func NewConsumer(client *kgo.Client) *Consumer {
	return &Consumer{
		client: client,
	}
}

// Poll fetches Kafka records and converts them into application-facing
// kafka.Record values.
//
// franz-go types never escape this package.
func (c *Consumer) Poll(ctx context.Context) ([]Record, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("kafka consumer is not initialized")
	}

	fetches := c.client.PollFetches(ctx)

	if fetches.IsClientClosed() {
		return nil, nil
	}

	if err := fetches.Err(); err != nil {
		return nil, fmt.Errorf("poll kafka: %w", err)
	}

	records := make([]Record, 0, fetches.NumRecords())

	fetches.EachRecord(func(record *kgo.Record) {
		if record != nil {
			records = append(records, fromKGORecord(record))
		}
	})

	return records, nil
}

// CommitRecords commits offsets for successfully processed records.
//
// This method accepts application-facing records so the consumer layer does
// not need to depend on franz-go's record type.
func (c *Consumer) CommitRecords(
	ctx context.Context,
	records ...Record,
) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("kafka consumer is not initialized")
	}

	if len(records) == 0 {
		return nil
	}

	kgoRecords := make([]*kgo.Record, 0, len(records))

	for _, record := range records {
		kgoRecords = append(kgoRecords, &kgo.Record{
			Topic:     record.Topic,
			Partition: record.Partition,
			Offset:    record.Offset,
		})
	}

	if err := c.client.CommitRecords(ctx, kgoRecords...); err != nil {
		return fmt.Errorf("commit kafka offsets: %w", err)
	}

	return nil
}

func fromKGORecord(record *kgo.Record) Record {
	headers := make([]RecordHeader, 0, len(record.Headers))

	for _, header := range record.Headers {
		headers = append(headers, RecordHeader{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		})
	}

	return Record{
		Topic:     record.Topic,
		Partition: record.Partition,
		Offset:    record.Offset,
		Key:       append([]byte(nil), record.Key...),
		Value:     append([]byte(nil), record.Value...),
		Headers:   headers,
		Context:   record.Context,
	}
}
