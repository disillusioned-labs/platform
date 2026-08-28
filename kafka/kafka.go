// Package kafka owns the Kafka client lifecycle.
//
// The package intentionally exposes the underlying franz-go client only
// through the constructor. Producers and consumers are thin wrappers around
// the same client and are wired by the application layer.
package kafka

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
	"go.opentelemetry.io/otel"
)

// Option configures the Kafka client.
//
// We intentionally reuse franz-go's option type instead of introducing a
// second functional-options abstraction.
type Option = kgo.Opt

// New creates and validates the shared Kafka client.
//
// The client is configured for both producing and consuming:
//
//   - producer delivery uses all ISR acknowledgements;
//   - producer retries and delivery timeout come from config;
//   - consumer group membership comes from config;
//   - auto-commit is disabled so the application commits only after durable
//     processing succeeds;
//   - franz-go OpenTelemetry hooks are installed on the shared client.
//
// Ping verifies Kafka connectivity before the application continues startup.
func New(
	ctx context.Context,
	cfg KafkaConfig,
	opts ...Option,
) (*kgo.Client, error) {
	defaultOpts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),

		// Producer configuration.
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerBatchCompression(
			kgo.Lz4Compression(),
			kgo.SnappyCompression(),
			kgo.NoCompression(),
		),
		kgo.RecordRetries(int(cfg.Producer.RecordRetries)),
		kgo.RecordDeliveryTimeout(cfg.Producer.RecordDeliveryTimeout),
		kgo.AllowAutoTopicCreation(),
	}

	// Consumer configuration — only set when a consumer group is configured.
	if cfg.Consumer.Group != "" {
		defaultOpts = append(defaultOpts,
			kgo.ConsumerGroup(cfg.Consumer.Group),
			kgo.DisableAutoCommit(),
		)
	}

	// OpenTelemetry integration.
	tracer := kotel.NewTracer(
		kotel.TracerProvider(otel.GetTracerProvider()),
		kotel.TracerPropagator(otel.GetTextMapPropagator()),
	)

	meter := kotel.NewMeter(
		kotel.MeterProvider(otel.GetMeterProvider()),
	)

	kotelService := kotel.NewKotel(
		kotel.WithTracer(tracer),
		kotel.WithMeter(meter),
	)

	defaultOpts = append(
		defaultOpts,
		kgo.WithHooks(kotelService.Hooks()...),
	)

	// Caller options are applied last so explicit overrides win.
	client, err := kgo.NewClient(
		append(defaultOpts, opts...)...,
	)
	if err != nil {
		return nil, fmt.Errorf("create kafka client: %w", err)
	}

	// Verify connectivity before returning the client.
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()

	if err := client.Ping(pingCtx); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping kafka: %w", err)
	}

	return client, nil
}

// Close releases Kafka client resources.
//
// Because producer and consumer wrappers share the same client, the
// application should close the client once rather than closing each wrapper
// independently.
func Close(client *kgo.Client) {
	if client == nil {
		return
	}
	client.Close()
}
