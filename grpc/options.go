package grpc

import (
	"crypto/tls"
	"math"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const (
	DefaultUnaryTimeout = 10 * time.Second
	DefaultMaxRecvSize  = 4 << 20 // 4 MiB
	DefaultMaxSendSize  = 4 << 20 // 4 MiB
)

type Logger interface {
	Error(msg string, args ...any)
	Warn(msg string, args ...any)
}

type Config struct {
	UnaryTimeout time.Duration

	MaxRecvMsgSize int
	MaxSendMsgSize int
	MaxHeaderSize  int

	TLSConfig *tls.Config

	KeepaliveParams      *keepalive.ClientParameters
	ServerKeepalive      *keepalive.ServerParameters
	KeepaliveEnforcement *keepalive.EnforcementPolicy

	Credentials credentials.PerRPCCredentials

	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider

	Logger Logger

	UnaryClientInterceptors  []grpc.UnaryClientInterceptor
	StreamClientInterceptors []grpc.StreamClientInterceptor

	UnaryServerInterceptors  []grpc.UnaryServerInterceptor
	StreamServerInterceptors []grpc.StreamServerInterceptor
}

type Option func(*Config) error

func defaultConfig() Config {
	return Config{
		UnaryTimeout:   DefaultUnaryTimeout,
		MaxRecvMsgSize: DefaultMaxRecvSize,
		MaxSendMsgSize: DefaultMaxSendSize,
		MaxHeaderSize:  64 << 10, // 64 KiB
	}
}

func WithUnaryTimeout(timeout time.Duration) Option {
	return func(c *Config) error {
		if timeout <= 0 {
			return errInvalidOption("unary timeout must be greater than zero")
		}

		c.UnaryTimeout = timeout
		return nil
	}
}

func WithMaxRecvMsgSize(size int) Option {
	return func(c *Config) error {
		if size <= 0 {
			return errInvalidOption("max receive message size must be greater than zero")
		}

		c.MaxRecvMsgSize = size
		return nil
	}
}

func WithMaxSendMsgSize(size int) Option {
	return func(c *Config) error {
		if size <= 0 {
			return errInvalidOption("max send message size must be greater than zero")
		}

		c.MaxSendMsgSize = size
		return nil
	}
}

func WithMaxHeaderSize(size int) Option {
	return func(c *Config) error {
		if size <= 0 {
			return errInvalidOption("max header size must be greater than zero")
		}

		if uint64(size) > math.MaxUint32 {
			return errInvalidOption("max header size exceeds uint32 maximum")
		}

		c.MaxHeaderSize = size
		return nil
	}
}

func WithTLS(config *tls.Config) Option {
	return func(c *Config) error {
		hardened, err := HardenTLSConfig(config)
		if err != nil {
			return err
		}

		c.TLSConfig = hardened
		return nil
	}
}

func WithKeepalive(params keepalive.ClientParameters) Option {
	return func(c *Config) error {
		c.KeepaliveParams = &params
		return nil
	}
}

func WithServerKeepalive(params keepalive.ServerParameters) Option {
	return func(c *Config) error {
		c.ServerKeepalive = &params
		return nil
	}
}

func WithKeepaliveEnforcement(policy keepalive.EnforcementPolicy) Option {
	return func(c *Config) error {
		c.KeepaliveEnforcement = &policy
		return nil
	}
}

func WithPerRPCCredentials(creds credentials.PerRPCCredentials) Option {
	return func(c *Config) error {
		if creds == nil {
			return errInvalidOption("per-RPC credentials must not be nil")
		}

		c.Credentials = creds
		return nil
	}
}

func WithLogger(logger Logger) Option {
	return func(c *Config) error {
		c.Logger = logger
		return nil
	}
}

func WithTracerProvider(provider trace.TracerProvider) Option {
	return func(c *Config) error {
		c.TracerProvider = provider
		return nil
	}
}

func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(c *Config) error {
		c.MeterProvider = provider
		return nil
	}
}

func WithUnaryClientInterceptor(interceptor grpc.UnaryClientInterceptor) Option {
	return func(c *Config) error {
		if interceptor != nil {
			c.UnaryClientInterceptors = append(
				c.UnaryClientInterceptors,
				interceptor,
			)
		}

		return nil
	}
}

func WithStreamClientInterceptor(interceptor grpc.StreamClientInterceptor) Option {
	return func(c *Config) error {
		if interceptor != nil {
			c.StreamClientInterceptors = append(
				c.StreamClientInterceptors,
				interceptor,
			)
		}

		return nil
	}
}

func WithUnaryServerInterceptor(interceptor grpc.UnaryServerInterceptor) Option {
	return func(c *Config) error {
		if interceptor != nil {
			c.UnaryServerInterceptors = append(
				c.UnaryServerInterceptors,
				interceptor,
			)
		}

		return nil
	}
}

func WithStreamServerInterceptor(interceptor grpc.StreamServerInterceptor) Option {
	return func(c *Config) error {
		if interceptor != nil {
			c.StreamServerInterceptors = append(
				c.StreamServerInterceptors,
				interceptor,
			)
		}

		return nil
	}
}

func errInvalidOption(message string) error {
	return &configError{message: message}
}

type configError struct {
	message string
}

func (e *configError) Error() string {
	return e.message
}
