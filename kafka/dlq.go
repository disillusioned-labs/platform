package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var dlqTracer = otel.Tracer("platform/kafka/dlq")

const (
	HeaderDLQSourceTopic     = "dlq-source-topic"
	HeaderDLQSourcePartition = "dlq-source-partition"
	HeaderDLQSourceOffset    = "dlq-source-offset"
	HeaderDLQAttempt         = "dlq-attempt"
	HeaderDLQErrorType       = "dlq-error-type"
	HeaderDLQErrorCode       = "dlq-error-code"
)

// DLQMetadata contains metadata describing why and where a record was
// dead-lettered.
type DLQMetadata struct {
	SourceTopic     string
	SourcePartition int32
	SourceOffset    int64
	Attempt         int
	ErrorType       string
	ErrorCode       string
}

// DLQPublisher publishes failed records to a dedicated dead-letter topic.
//
// It owns the DLQ transport contract: metadata is converted into standardized
// Kafka headers here, while the underlying Producer is responsible only for
// publishing the record.
type DLQPublisher struct {
	producer Producer
	topic    string
	log      *slog.Logger
}

func NewDLQPublisher(
	producer Producer,
	topic string,
	log *slog.Logger,
) *DLQPublisher {
	return &DLQPublisher{
		producer: producer,
		topic:    topic,
		log:      log,
	}
}

// Publish publishes a failed record to the configured DLQ topic.
//
// The original key, value, and headers are preserved. DLQ metadata is appended
// as standardized headers.
//
// A successful return means Kafka acknowledged the DLQ record. The caller can
// then safely commit the original record offset.
func (p *DLQPublisher) Publish(
	ctx context.Context,
	record Record,
	metadata DLQMetadata,
) error {
	if p == nil || p.producer == nil {
		return fmt.Errorf("kafka dlq publisher is not initialized")
	}

	if p.topic == "" {
		return fmt.Errorf("kafka dlq topic must not be empty")
	}

	if record.Topic == "" {
		return fmt.Errorf("kafka dlq source record topic must not be empty")
	}

	if metadata.SourceTopic == "" {
		return fmt.Errorf("kafka dlq source topic must not be empty")
	}

	if metadata.Attempt <= 0 {
		return fmt.Errorf("kafka dlq attempt must be greater than zero")
	}

	if metadata.ErrorType == "" {
		return fmt.Errorf("kafka dlq error type must not be empty")
	}

	ctx, span := dlqTracer.Start(ctx, "kafka.dlq.publish")
	defer span.End()

	headers := make([]RecordHeader, 0, len(record.Headers)+6)

	headers = append(headers, record.Headers...)

	headers = append(
		headers,
		NewRecordHeader(HeaderDLQSourceTopic, metadata.SourceTopic),
		NewRecordHeader(HeaderDLQSourcePartition,
			strconv.FormatInt(int64(metadata.SourcePartition), 10)),
		NewRecordHeader(HeaderDLQSourceOffset,
			strconv.FormatInt(metadata.SourceOffset, 10)),
		NewRecordHeader(HeaderDLQAttempt,
			strconv.Itoa(metadata.Attempt)),
		NewRecordHeader(HeaderDLQErrorType, metadata.ErrorType),
	)

	if metadata.ErrorCode != "" {
		headers = append(
			headers,
			NewRecordHeader(HeaderDLQErrorCode, metadata.ErrorCode),
		)
	}

	err := p.producer.Publish(
		ctx,
		Record{
			Topic:   p.topic,
			Key:     record.Key,
			Value:   record.Value,
			Headers: headers,
		},
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish kafka dlq failed")
		return fmt.Errorf("publish kafka dlq: %w", err)
	}

	if p.log != nil {
		p.log.Warn(
			"kafka record moved to dlq",
			"source_topic", metadata.SourceTopic,
			"source_partition", metadata.SourcePartition,
			"source_offset", metadata.SourceOffset,
			"dlq_topic", p.topic,
			"attempt", metadata.Attempt,
			"error_type", metadata.ErrorType,
			"error_code", metadata.ErrorCode,
		)
	}

	return nil
}
