// Package config provides common configuration types and utilities shared by
// all services. Each service defines its own Config struct that embeds these
// common types and adds service-specific fields.
package config

import (
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ServiceConfig identifies this service in logs and telemetry.
//
// It is a section rather than two root keys so the variables read SERVICE_NAME
// and SERVICE_ENV. Bare NAME and ENV are the two most collision-prone names in
// an unprefixed environment - generic enough that unrelated tooling sets them.
type ServiceConfig struct {
	Name       string `mapstructure:"name"`
	Env        string `mapstructure:"env"`
	InstanceID string `mapstructure:"instance_id"`
}

// ServerConfig holds the HTTP listener's port and timeout budget.
type ServerConfig struct {
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// RequestTimeout bounds each request's context; slow downstream calls
	// (DB, Redis) abort when it fires and handler.WriteServiceError turns the
	// resulting context error into a 504. Validated to stay below
	// write_timeout so that 504 can still reach the client.
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	// DrainDelay is how long /readyz reports 503 before in-flight requests
	// start draining. It exists for orchestrators that remove a pod from
	// service asynchronously: without the gap, connections keep arriving at a
	// server that has already begun shutting down. 0 disables the wait.
	DrainDelay time.Duration `mapstructure:"drain_delay"`
}

// GRPCConfig holds gRPC server transport settings.
//
// Transport and security behavior are owned by platform/grpc. This config only
// contains service-level knobs that operators may need to tune.
type GRPCConfig struct {
	// ServerPort is the TCP port used by the gRPC server.
	ServerPort int `mapstructure:"server_port"`

	// MaxRecvMsgSize and MaxSendMsgSize bound protobuf message sizes.
	// Defaults should remain conservative; increase only when a contract
	// explicitly requires larger messages.
	MaxRecvMsgSize int `mapstructure:"max_recv_msg_size"`
	MaxSendMsgSize int `mapstructure:"max_send_msg_size"`

	// MaxHeaderSize bounds HTTP/2 header metadata accepted by the server.
	MaxHeaderSize int `mapstructure:"max_header_size"`

	// UnaryTimeout is applied only when the caller did not already provide
	// a deadline. Streaming RPCs must define their own lifecycle.
	UnaryTimeout time.Duration `mapstructure:"unary_timeout"`

	// TLS controls gRPC server transport security.
	TLS GRPCTLSConfig `mapstructure:"tls"`
}

// GRPCClientConfig holds outbound gRPC client transport settings.
//
// Connection lifecycle, dialing, retries, interceptors, and transport
// behavior are owned by platform/grpc. This config only contains
// service-level knobs that operators may need to tune.
type GRPCClientConfig struct {
	// Target is the gRPC server target consumed by the configured resolver.
	// It may use a resolver-specific target such as dns:///host:port.
	Target string `mapstructure:"target"`

	// Timeout is the default RPC timeout applied when the caller does not
	// already provide a deadline.
	Timeout time.Duration `mapstructure:"timeout"`

	// MaxRecvMsgSize and MaxSendMsgSize bound protobuf message sizes.
	// Defaults should remain conservative; increase only when a contract
	// explicitly requires larger messages.
	MaxRecvMsgSize int `mapstructure:"max_recv_msg_size"`
	MaxSendMsgSize int `mapstructure:"max_send_msg_size"`

	// TLS controls gRPC client transport security.
	TLS GRPCTLSConfig `mapstructure:"tls"`
}

// GRPCTLSConfig controls TLS/mTLS for gRPC.
//
// In production, TLS should be enabled. Insecure transport must be an
// explicit platform/grpc option and should only be used for local
// development or tests.
type GRPCTLSConfig struct {
	// Enabled enables TLS transport security.
	Enabled bool `mapstructure:"enabled"`

	// CAFile contains the trusted CA bundle used to verify peer certificates.
	// Leave empty to use the system trust store.
	CAFile string `mapstructure:"ca_file"`

	// CertFile and KeyFile contain the service certificate and private key.
	// They are required for a TLS-enabled server and for mTLS clients.
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`

	// ServerName overrides TLS server-name verification when required by the
	// deployment. Leave empty to use the target hostname.
	ServerName string `mapstructure:"server_name"`

	// MutualTLS requires the peer to present a certificate.
	// For servers, this enables client certificate verification. For clients,
	// this requires CertFile and KeyFile to authenticate to the server.
	MutualTLS bool `mapstructure:"mutual_tls"`
}

// PprofConfig gates the profiling listener. Disabled means the listener is never
// created: no socket, no goroutine.
//
// It is a separate listener from /metrics, which is served on the public router,
// because the two have opposite exposure requirements: a scraper must reach
// metrics from off-host, while pprof - raw memory plus an easy way to burn CPU -
// must never be routable. One listener cannot be both, so pprof is hardcoded to
// loopback with no host knob to get wrong.
type PprofConfig struct {
	// Enabled turns the listener on. Off by default: this is an attack-surface
	// decision, not a performance one - pprof samples nothing until an endpoint
	// is actually requested.
	Enabled bool `mapstructure:"enabled"`
	// Port is the loopback port to bind when Enabled. Ignored otherwise, so it
	// can stay filled in as documentation of the conventional choice.
	Port int `mapstructure:"port"`
}

// Env values accepted in SERVICE_ENV; they gate log formatting defaults and are
// stamped on every span and log record.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// PostgresConfig holds the pgx pool settings and startup-migration flag.
type PostgresConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxConns        int32         `mapstructure:"max_conns"`
	MinConns        int32         `mapstructure:"min_conns"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
	// Migrate runs embedded goose migrations at boot. Defaults to false so the
	// production image is safe without a config file: concurrent replicas all
	// racing to migrate on rollout is not something to opt into by accident.
	// .env.example turns it on for local dev.
	Migrate bool `mapstructure:"migrate"`
	// QueryExecMode selects pgx's statement protocol. Leave "cache_statement"
	// when talking to Postgres directly. Behind a connection pooler in
	// transaction mode (pgbouncer, RDS Proxy, Supabase's 6543 port) server-side
	// prepared statements break, and this must be "simple_protocol" or
	// "exec" - a boilerplate-level footgun worth a knob.
	QueryExecMode string `mapstructure:"query_exec_mode"`
}

// LogValue redacts the DSN's password so logging a Config (or a
// PostgresConfig) can never leak credentials.
func (p PostgresConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("dsn", RedactDSN(p.DSN)),
		slog.Int64("max_conns", int64(p.MaxConns)),
		slog.Int64("min_conns", int64(p.MinConns)),
		slog.Duration("max_conn_lifetime", p.MaxConnLifetime),
		slog.Bool("migrate", p.Migrate),
		slog.String("query_exec_mode", p.QueryExecMode),
	)
}

// keywordPassword matches the password field of a libpq keyword/value DSN
// ("host=db password=secret sslmode=require"), which url.Parse cannot redact.
var keywordPassword = regexp.MustCompile(`(?i)\bpassword\s*=\s*('(?:[^']|'')*'|\S+)`)

// RedactDSN replaces the password in a DSN with "xxxxx", handling both accepted
// pgx forms: URL ("postgres://user:pw@host/db") and libpq keyword/value
// ("host=... password=..."). Unparseable input is reported as redacted rather
// than echoed, so a malformed DSN carrying a secret still never reaches the logs.
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	// Keyword/value form has no scheme; url.Parse would return it untouched
	// and leak the password, so handle it first.
	if !strings.Contains(dsn, "://") {
		return keywordPassword.ReplaceAllString(dsn, "password=xxxxx")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "[unparseable dsn redacted]"
	}
	return u.Redacted()
}

// OTelConfig controls OTLP export of traces and metrics.
//
// The keys spell out to the OpenTelemetry SDK's own variable names on purpose.
// The SDK reads those names from the real environment itself, so matching its
// spelling and meaning is what keeps an operator's OTEL_EXPORTER_OTLP_ENDPOINT
// from being silently ignored. Two deliberate deviations:
//   - OTEL_SERVICE_NAME is not read; SERVICE_NAME owns the service identity because the
//     logger uses it too.
//   - OTEL_METRIC_EXPORT_INTERVAL is milliseconds, per spec, unlike every other
//     interval here.
//
// Unregistered OTEL_* still reach the exporter from the real environment
// (_HEADERS, _TIMEOUT, _COMPRESSION) - but not from .env, which is parsed into
// Viper rather than exported into the process.
type OTelConfig struct {
	// SDKDisabled turns off both signals; one signal is disabled with
	// TracesExporter/MetricsExporter = "none".
	SDKDisabled bool `mapstructure:"sdk_disabled"`

	// TracesExporter and MetricsExporter accept "otlp" or "none".
	TracesExporter  string `mapstructure:"traces_exporter"`
	MetricsExporter string `mapstructure:"metrics_exporter"`

	// Endpoint is the collector base URL. Its scheme decides transport
	// security, which is why there is no separate insecure flag.
	Endpoint string `mapstructure:"exporter_otlp_endpoint"`
	// TracesEndpoint and MetricsEndpoint override Endpoint for one signal.
	TracesEndpoint  string `mapstructure:"exporter_otlp_traces_endpoint"`
	MetricsEndpoint string `mapstructure:"exporter_otlp_metrics_endpoint"`

	// TracesSampler is a spec sampler name; see telemetry.NewSampler.
	TracesSampler string `mapstructure:"traces_sampler"`
	// TracesSamplerArg is the ratio for the traceidratio samplers.
	TracesSamplerArg float64 `mapstructure:"traces_sampler_arg"`

	// MetricExportIntervalMillis is the push period, in milliseconds.
	MetricExportIntervalMillis int `mapstructure:"metric_export_interval"`
}

// TracesEnabled reports whether spans should be exported.
func (o OTelConfig) TracesEnabled() bool {
	return !o.SDKDisabled && o.TracesExporter == OTelExporterOTLP
}

// MetricsEnabled reports whether metrics should be exported. There is no scrape
// endpoint, so disabling this means no metrics at all.
func (o OTelConfig) MetricsEnabled() bool {
	return !o.SDKDisabled && o.MetricsExporter == OTelExporterOTLP
}

// TraceEndpoint resolves the per-signal override against the base endpoint.
func (o OTelConfig) TraceEndpoint() string {
	if o.TracesEndpoint != "" {
		return o.TracesEndpoint
	}
	return o.Endpoint
}

// MetricEndpoint resolves the per-signal override against the base endpoint.
func (o OTelConfig) MetricEndpoint() string {
	if o.MetricsEndpoint != "" {
		return o.MetricsEndpoint
	}
	return o.Endpoint
}

// MetricExportInterval converts the spec's millisecond integer to a Duration.
func (o OTelConfig) MetricExportInterval() time.Duration {
	return time.Duration(o.MetricExportIntervalMillis) * time.Millisecond
}

// Exporter values accepted in OTEL_TRACES_EXPORTER / OTEL_METRICS_EXPORTER.
const (
	// OTelExporterOTLP pushes over OTLP/gRPC.
	OTelExporterOTLP = "otlp"
	// OTelExporterNone disables the signal.
	OTelExporterNone = "none"
)

// OTelSamplers are the OTEL_TRACES_SAMPLER values this app implements. The
// spec's jaeger_remote and xray are rejected by validation rather than ignored:
// a sampler that silently is not the one you asked for is how an incident ends
// up with no trace to look at.
var OTelSamplers = []string{
	"always_on",
	"always_off",
	"traceidratio",
	"parentbased_always_on",
	"parentbased_always_off",
	"parentbased_traceidratio",
}

// LogConfig sets slog level ("debug".."error") and format ("text" or "json").
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// RedisMode controls how the app treats its Redis dependency.
type RedisMode string

const (
	// RedisModeDisabled skips Redis entirely: no connection, no caching.
	RedisModeDisabled RedisMode = "disabled"
	// RedisModeOptional degrades gracefully: an unreachable Redis logs a
	// warning and the app runs uncached.
	RedisModeOptional RedisMode = "optional"
	// RedisModeRequired makes an unreachable Redis fatal at startup (fail-fast).
	RedisModeRequired RedisMode = "required"
)

// RedisConfig holds the Redis connection settings; Mode decides how hard the
// app depends on it (see RedisMode).
type RedisConfig struct {
	Mode     RedisMode `mapstructure:"mode"`
	Addr     string    `mapstructure:"addr"`
	Password string    `mapstructure:"password"`
	DB       int       `mapstructure:"db"`
}

// LogValue keeps the Redis password out of logs.
func (r RedisConfig) LogValue() slog.Value {
	pw := ""
	if r.Password != "" {
		pw = "xxxxx"
	}
	return slog.GroupValue(
		slog.String("mode", string(r.Mode)),
		slog.String("addr", r.Addr),
		slog.String("password", pw),
		slog.Int("db", r.DB),
	)
}

// CacheConfig tunes the object cache built on Redis.
type CacheConfig struct {
	DefaultTTL time.Duration `mapstructure:"default_ttl"`
}

// RateLimitConfig bounds per-client-IP request rate on the API subtree. Probes
// and /metrics are never limited (see server.New).
//
// The limiter is per process: N replicas allow N*Requests per window. Swap in a
// shared store (httprate-redis) before relying on it as a hard global cap.
type RateLimitConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Requests is how many requests one IP may make per Window.
	Requests int `mapstructure:"requests"`
	// Window is the length of the counting window.
	Window time.Duration `mapstructure:"window"`
}

// KafkaConfig holds the Kafka connection settings. Producer and consumer
// settings are nested so env vars follow the kafka.producer.* and
// kafka.consumer.* pattern. Services that only use one side leave the other
// at its zero value.
type KafkaConfig struct {
	Brokers     []string            `mapstructure:"brokers"`
	ClientID    string              `mapstructure:"client_id"`
	PingTimeout time.Duration       `mapstructure:"ping_timeout"`
	Producer    KafkaProducerConfig `mapstructure:"producer"`
	Consumer    KafkaConsumerConfig `mapstructure:"consumer"`
}

// KafkaConsumerConfig holds Kafka consumer-specific settings.
type KafkaConsumerConfig struct {
	Group    string           `mapstructure:"group"`
	Topics   []string         `mapstructure:"topics"`
	DLQTopic string           `mapstructure:"dlq_topic"`
	Retry    KafkaRetryConfig `mapstructure:"retry"`
}

// KafkaRetryConfig holds retry settings for Kafka consumer.
type KafkaRetryConfig struct {
	MaxAttempts  int           `mapstructure:"max_attempts"`
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	MaxDelay     time.Duration `mapstructure:"max_delay"`
}

// KafkaProducerConfig holds Kafka producer-specific settings.
type KafkaProducerConfig struct {
	RecordRetries         int64         `mapstructure:"record_retries"`
	RecordDeliveryTimeout time.Duration `mapstructure:"record_delivery_timeout"`
}

// NormalizeKafkaBrokers splits comma-separated broker values and trims whitespace.
func NormalizeKafkaBrokers(brokers []string) []string {
	var result []string

	for _, value := range brokers {
		for broker := range strings.SplitSeq(value, ",") {
			broker = strings.TrimSpace(broker)
			if broker != "" {
				result = append(result, broker)
			}
		}
	}

	return result
}

// NormalizeKafkaTopics splits comma-separated topic values and trims whitespace.
func NormalizeKafkaTopics(topics []string) []string {
	var result []string

	for _, value := range topics {
		for topic := range strings.SplitSeq(value, ",") {
			topic = strings.TrimSpace(topic)
			if topic != "" {
				result = append(result, topic)
			}
		}
	}

	return result
}

// InstanceID returns the hostname or a random UUID.
func InstanceID() string {
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}

	return uuid.NewString()
}
