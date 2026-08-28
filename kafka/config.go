package kafka

import (
	"fmt"
	"strings"
	"time"
)

// KafkaConfig holds the Kafka client configuration. Services define their own
// top-level config structs and map into this type, keeping the platform
// library decoupled from any specific config package.
type KafkaConfig struct {
	Brokers     []string
	ClientID    string
	PingTimeout time.Duration
	Producer    ProducerConfig
	Consumer    ConsumerConfig
}

// ProducerConfig tunes the franz-go producer.
type ProducerConfig struct {
	// RecordRetries is the number of times a record will be retried on
	// recoverable errors before giving up.
	RecordRetries int64
	// RecordDeliveryTimeout is the maximum time a record will remain in the
	// producer buffer before being dropped if not delivered.
	RecordDeliveryTimeout time.Duration
}

// ConsumerConfig tunes the franz-go consumer.
type ConsumerConfig struct {
	// Group is the consumer group name. Leave empty for producer-only
	// deployments.
	Group string
	// Topic is the topic to consume from. Leave empty for producer-only
	// deployments.
	Topics []string
	// DLQTopic is the dead-letter queue topic. Leave empty for producer-only
	// deployments.
	DLQTopic string
	Retry    RetryConfig
}

// RetryConfig controls the consumer retry backoff policy.
type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// Validate checks that all required fields are set and semantically valid.
// Producer-only deployments (empty Group/Topic/DLQTopic) are valid.
func (c KafkaConfig) Validate() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("kafka.brokers must not be empty")
	}
	for i, broker := range c.Brokers {
		if strings.TrimSpace(broker) == "" {
			return fmt.Errorf("kafka.brokers[%d] must not be empty", i)
		}
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("kafka.client_id must not be empty")
	}
	if c.PingTimeout <= 0 {
		return fmt.Errorf("kafka.ping_timeout must be > 0, got %s", c.PingTimeout)
	}
	if c.Producer.RecordRetries < 0 {
		return fmt.Errorf("kafka.producer.record_retries must be >= 0, got %d", c.Producer.RecordRetries)
	}
	if c.Producer.RecordDeliveryTimeout <= 0 {
		return fmt.Errorf("kafka.producer.record_delivery_timeout must be > 0, got %s", c.Producer.RecordDeliveryTimeout)
	}

	// Validate consumer config only if a consumer group is configured.
	if c.Consumer.Group != "" {
		if len(c.Consumer.Topics) == 0 {
			return fmt.Errorf("kafka.consumer.topic must not be empty when consumer.group is set")
		}
		for i, topic := range c.Consumer.Topics {
			topic = strings.TrimSpace(topic)
			if topic == "" {
				return fmt.Errorf("kafka.consumer.topics[%d] must not be empty", i)
			}

			if topic == c.Consumer.DLQTopic {
				return fmt.Errorf("kafka.consumer.topics[%d] must not be the same as kafka.consumer.dlq_topic", i)
			}
		}
		if c.Consumer.DLQTopic == "" {
			return fmt.Errorf("kafka.consumer.dlq_topic must not be empty when consumer.group is set")
		}
		retry := c.Consumer.Retry
		if retry.MaxAttempts < 1 {
			return fmt.Errorf("kafka.consumer.retry.max_attempts must be >= 1, got %d", retry.MaxAttempts)
		}
		if retry.InitialDelay <= 0 {
			return fmt.Errorf("kafka.consumer.retry.initial_delay must be > 0, got %s", retry.InitialDelay)
		}
		if retry.MaxDelay <= 0 {
			return fmt.Errorf("kafka.consumer.retry.max_delay must be > 0, got %s", retry.MaxDelay)
		}
		if retry.MaxDelay < retry.InitialDelay {
			return fmt.Errorf("kafka.consumer.retry.max_delay (%s) must be >= initial_delay (%s)",
				retry.MaxDelay, retry.InitialDelay)
		}
	}

	return nil
}
