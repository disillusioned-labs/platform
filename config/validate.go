package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

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

	if c.Name == "" {
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

// ValidateKafka validates the common KafkaConfig fields (brokers, client_id,
// ping_timeout). Services that use consumer or producer fields validate those
// separately.
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
		fail("kafka.ping_timeout must be > 0, got %s", c.PingTimeout)
	}

	return errors.Join(errs...)
}
