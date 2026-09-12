package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ValidateServer validates the common HTTP server configuration.
func ValidateServer(c *ServerConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.Port < 1 || c.Port > 65535 {
		fail(
			"server.port must be between 1 and 65535, got %d",
			c.Port,
		)
	}

	for _, d := range []struct {
		key   string
		value time.Duration
	}{
		{"server.read_timeout", c.ReadTimeout},
		{"server.write_timeout", c.WriteTimeout},
		{"server.idle_timeout", c.IdleTimeout},
		{"server.shutdown_timeout", c.ShutdownTimeout},
		{"server.request_timeout", c.RequestTimeout},
	} {
		if d.value <= 0 {
			fail(
				"%s must be > 0, got %s",
				d.key,
				d.value,
			)
		}
	}

	if c.DrainDelay < 0 {
		fail(
			"server.drain_delay must not be negative, got %s",
			c.DrainDelay,
		)
	}

	return errors.Join(errs...)
}

// ValidateGRPC validates the gRPC server configuration.
func ValidateGRPC(c *GRPCConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.ServerPort < 1 || c.ServerPort > 65535 {
		fail(
			"grpc.server_port must be between 1 and 65535, got %d",
			c.ServerPort,
		)
	}

	if c.MaxRecvMsgSize <= 0 {
		fail(
			"grpc.max_recv_msg_size must be > 0, got %d",
			c.MaxRecvMsgSize,
		)
	}

	if c.MaxSendMsgSize <= 0 {
		fail(
			"grpc.max_send_msg_size must be > 0, got %d",
			c.MaxSendMsgSize,
		)
	}

	if c.MaxHeaderSize <= 0 {
		fail(
			"grpc.max_header_size must be > 0, got %d",
			c.MaxHeaderSize,
		)
	}

	if c.UnaryTimeout <= 0 {
		fail(
			"grpc.unary_timeout must be > 0, got %s",
			c.UnaryTimeout,
		)
	}

	if err := validateGRPCTLS(&c.TLS); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// ValidateGRPCClient validates outbound gRPC client configuration. Targets
// are validated by the consuming service - they are per-dependency keys, not
// part of the shared client settings.
func ValidateGRPCClient(c *GRPCClientConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.Timeout <= 0 {
		fail(
			"grpc_client.timeout must be > 0, got %s",
			c.Timeout,
		)
	}

	if c.MaxRecvMsgSize <= 0 {
		fail(
			"grpc_client.max_recv_msg_size must be > 0, got %d",
			c.MaxRecvMsgSize,
		)
	}

	if c.MaxSendMsgSize <= 0 {
		fail(
			"grpc_client.max_send_msg_size must be > 0, got %d",
			c.MaxSendMsgSize,
		)
	}

	if err := validateGRPCClientTLS(&c.TLS); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateGRPCTLS validates gRPC server TLS configuration.
//
// When TLS is enabled, the server requires its certificate and private key.
// Mutual TLS additionally requires a CA bundle to verify client certificates.
func validateGRPCTLS(c *GRPCTLSConfig) error {
	if !c.Enabled {
		return nil
	}

	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.CertFile) == "" {
		fail(
			"grpc.tls.cert_file must not be empty when TLS is enabled",
		)
	}

	if strings.TrimSpace(c.KeyFile) == "" {
		fail(
			"grpc.tls.key_file must not be empty when TLS is enabled",
		)
	}

	if c.MutualTLS && strings.TrimSpace(c.CAFile) == "" {
		fail(
			"grpc.tls.ca_file must not be empty when mutual TLS is enabled",
		)
	}

	return errors.Join(errs...)
}

// validateGRPCClientTLS validates outbound gRPC client TLS configuration.
//
// A TLS client does not require its own certificate unless mutual TLS is
// enabled. When CAFile is empty, the system trust store is used.
func validateGRPCClientTLS(c *GRPCTLSConfig) error {
	if !c.Enabled {
		return nil
	}

	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.MutualTLS {
		if strings.TrimSpace(c.CAFile) == "" {
			fail(
				"grpc_client.tls.ca_file must not be empty when mutual TLS is enabled",
			)
		}

		if strings.TrimSpace(c.CertFile) == "" {
			fail(
				"grpc_client.tls.cert_file must not be empty when mutual TLS is enabled",
			)
		}

		if strings.TrimSpace(c.KeyFile) == "" {
			fail(
				"grpc_client.tls.key_file must not be empty when mutual TLS is enabled",
			)
		}
	}

	return errors.Join(errs...)
}

// ValidatePprof validates the common pprof configuration.
func ValidatePprof(c *PprofConfig) error {
	if !c.Enabled {
		return nil
	}

	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.Port < 1 || c.Port > 65535 {
		fail(
			"pprof.port must be between 1 and 65535 when pprof.enabled, got %d",
			c.Port,
		)
	}

	return errors.Join(errs...)
}

// ValidatePostgres validates the PostgresConfig fields.
func ValidatePostgres(c *PostgresConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.DSN) == "" {
		fail("postgres.dsn must not be empty")
	}
	if c.MaxConns < 1 {
		fail("postgres.max_conns must be >= 1, got %d", c.MaxConns)
	}
	if c.MinConns < 0 {
		fail("postgres.min_conns must not be negative, got %d", c.MinConns)
	}
	if c.MinConns > c.MaxConns {
		fail("postgres.min_conns (%d) must be <= postgres.max_conns (%d)",
			c.MinConns, c.MaxConns)
	}
	switch c.QueryExecMode {
	case "cache_statement", "cache_describe", "describe_exec", "exec", "simple_protocol":
	default:
		fail("postgres.query_exec_mode must be one of cache_statement|cache_describe|describe_exec|exec|simple_protocol, got %q",
			c.QueryExecMode)
	}

	return errors.Join(errs...)
}

// ValidateRedis validates the common RedisConfig fields.
func ValidateRedis(c *RedisConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	switch c.Mode {
	case RedisModeDisabled:
	case RedisModeOptional, RedisModeRequired:
		if strings.TrimSpace(c.Addr) == "" {
			fail(
				"redis.addr must be set when redis.mode is %s",
				c.Mode,
			)
		}
	default:
		fail(
			"redis.mode must be one of disabled|optional|required, got %q",
			c.Mode,
		)
	}

	if c.DB < 0 {
		fail("redis.db must not be negative, got %d", c.DB)
	}

	return errors.Join(errs...)
}

// ValidateCache validates the common CacheConfig fields.
func ValidateCache(c *CacheConfig) error {
	if c.DefaultTTL <= 0 {
		return fmt.Errorf(
			"cache.default_ttl must be > 0, got %s",
			c.DefaultTTL,
		)
	}

	return nil
}

// ValidateRateLimit validates the common RateLimitConfig fields.
func ValidateRateLimit(c *RateLimitConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if !c.Enabled {
		return nil
	}

	if c.Requests <= 0 {
		fail(
			"ratelimit.requests must be > 0 when ratelimit.enabled, got %d",
			c.Requests,
		)
	}

	if c.Window <= 0 {
		fail(
			"ratelimit.window must be > 0 when ratelimit.enabled, got %s",
			c.Window,
		)
	}

	return errors.Join(errs...)
}

// ValidateOTel validates the OTelConfig fields.
func ValidateOTel(c *OTelConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	for _, e := range []struct {
		key   string
		value string
	}{
		{"otel.traces_exporter", c.TracesExporter},
		{"otel.metrics_exporter", c.MetricsExporter},
	} {
		if e.value != OTelExporterOTLP && e.value != OTelExporterNone {
			fail("%s must be %s or %s, got %q", e.key, OTelExporterOTLP, OTelExporterNone, e.value)
		}
	}
	// Only validated per signal: a broken endpoint must not block boot for a
	// deployment that exports neither.
	for _, e := range []struct {
		key      string
		endpoint string
		enabled  bool
	}{
		{"otel.exporter_otlp_traces_endpoint", c.TraceEndpoint(), c.TracesEnabled()},
		{"otel.exporter_otlp_metrics_endpoint", c.MetricEndpoint(), c.MetricsEnabled()},
	} {
		if !e.enabled {
			continue
		}
		if err := ValidateOTLPEndpoint(e.endpoint); err != nil {
			fail("%s: %w", e.key, err)
		}
	}
	if c.TracesEnabled() {
		if !slices.Contains(OTelSamplers, c.TracesSampler) {
			fail("otel.traces_sampler must be one of %s, got %q",
				strings.Join(OTelSamplers, "|"), c.TracesSampler)
		}
		if c.TracesSamplerArg < 0 || c.TracesSamplerArg > 1 {
			fail("otel.traces_sampler_arg must be in 0.0..1.0, got %v", c.TracesSamplerArg)
		}
	}
	if c.MetricsEnabled() && c.MetricExportIntervalMillis <= 0 {
		fail("otel.metric_export_interval must be > 0 milliseconds, got %d",
			c.MetricExportIntervalMillis)
	}

	return errors.Join(errs...)
}

// ValidateLog validates the LogConfig fields.
func ValidateLog(c *LogConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	// Both are matched case-insensitively downstream; validate the same way so
	// a typo fails here instead of silently falling back to info/json.
	switch strings.ToLower(c.Level) {
	case "debug", "info", "warn", "error":
	default:
		fail("log.level must be one of debug|info|warn|error, got %q", c.Level)
	}
	switch strings.ToLower(c.Format) {
	case "text", "json":
	default:
		fail("log.format must be text or json, got %q", c.Format)
	}

	return errors.Join(errs...)
}

// ValidateOTLPEndpoint enforces the spec's URL form. The scheme selects TLS, so
// a bare "host:4317" - what everyone types first - has no defined transport
// security and is rejected with the fix spelled out.
func ValidateOTLPEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("must be set while the signal is exported")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("must be a URL, got %q", endpoint)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"must include an http:// or https:// scheme, which selects transport security; got %q (try %q)",
			endpoint, "http://"+endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host, got %q", endpoint)
	}
	return nil
}

// ValidateService validates the ServiceConfig fields.
func ValidateService(c *ServiceConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.Name) == "" {
		fail("service.name must not be empty")
	}

	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		fail("service.env must be one of %s|%s|%s, got %q",
			EnvDevelopment, EnvStaging, EnvProduction, c.Env)
	}

	return errors.Join(errs...)
}

// ValidateKafka validates the common KafkaConfig fields.
//
// Producer and consumer-specific fields are intentionally not validated here
// because a service may use only one side. Services should validate the
// producer or consumer configuration they actually use.
func ValidateKafka(c *KafkaConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if len(c.Brokers) == 0 {
		fail("kafka.brokers must not be empty")
	}

	for i, broker := range c.Brokers {
		if strings.TrimSpace(broker) == "" {
			fail("kafka.brokers[%d] must not be empty", i)
		}
	}

	if strings.TrimSpace(c.ClientID) == "" {
		fail("kafka.client_id must not be empty")
	}

	if c.PingTimeout <= 0 {
		fail(
			"kafka.ping_timeout must be > 0, got %s",
			c.PingTimeout,
		)
	}

	return errors.Join(errs...)
}

func ValidateKafkaProducer(c *KafkaProducerConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.RecordRetries < 0 {
		fail(
			"kafka.producer.record_retries must not be negative, got %d",
			c.RecordRetries,
		)
	}

	if c.RecordDeliveryTimeout <= 0 {
		fail(
			"kafka.producer.record_delivery_timeout must be > 0, got %s",
			c.RecordDeliveryTimeout,
		)
	}

	return errors.Join(errs...)
}

func ValidateKafkaConsumer(c *KafkaConsumerConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.Group) == "" {
		fail("kafka.consumer.group must not be empty")
	}

	if len(c.Topics) == 0 {
		fail("kafka.consumer.topics must not be empty")
	}

	for i, topic := range c.Topics {
		if strings.TrimSpace(topic) == "" {
			fail("kafka.consumer.topics[%d] must not be empty", i)
		}
	}

	if strings.TrimSpace(c.DLQTopic) == "" {
		fail("kafka.consumer.dlq_topic must not be empty")
	}

	if err := ValidateKafkaRetry(&c.Retry); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func ValidateKafkaRetry(c *KafkaRetryConfig) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if c.MaxAttempts < 1 {
		fail(
			"kafka.consumer.retry.max_attempts must be >= 1, got %d",
			c.MaxAttempts,
		)
	}

	if c.InitialDelay <= 0 {
		fail(
			"kafka.consumer.retry.initial_delay must be > 0, got %s",
			c.InitialDelay,
		)
	}

	if c.MaxDelay <= 0 {
		fail(
			"kafka.consumer.retry.max_delay must be > 0, got %s",
			c.MaxDelay,
		)
	}

	if c.InitialDelay > c.MaxDelay {
		fail(
			"kafka.consumer.retry.initial_delay (%s) must be <= kafka.consumer.retry.max_delay (%s)",
			c.InitialDelay,
			c.MaxDelay,
		)
	}

	return errors.Join(errs...)
}
