package authkit

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	JWKSURL string
	Issuer  string
}

type Option func(*settings)

type ErrorHandler func(
	http.ResponseWriter,
	*http.Request,
	error,
)

type settings struct {
	httpClient      *http.Client
	refreshEvery    time.Duration
	unknownKidRate  time.Duration
	unknownKidBurst int
	clockSkew       time.Duration
	keySource       KeySource
	errorHandler    ErrorHandler
	log             *slog.Logger
	tracer          trace.Tracer
}

func defaults() settings {
	return settings{
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		refreshEvery:    time.Hour,
		unknownKidRate:  time.Minute,
		unknownKidBurst: 1,
		clockSkew:       30 * time.Second,
		log:             slog.Default(),
		tracer:          otel.Tracer("authkit"),
	}
}

func WithHTTPClient(c *http.Client) Option {
	return func(s *settings) {
		s.httpClient = c
	}
}

func WithRefreshInterval(d time.Duration) Option {
	return func(s *settings) {
		s.refreshEvery = d
	}
}

func WithUnknownKidLimit(every time.Duration, burst int) Option {
	return func(s *settings) {
		s.unknownKidRate = every
		s.unknownKidBurst = burst
	}
}

func WithClockSkew(d time.Duration) Option {
	return func(s *settings) {
		s.clockSkew = d
	}
}

func WithKeySource(source KeySource) Option {
	return func(s *settings) {
		s.keySource = source
	}
}

func WithErrorHandler(handler ErrorHandler) Option {
	return func(s *settings) {
		s.errorHandler = handler
	}
}

func WithLogger(l *slog.Logger) Option {
	return func(s *settings) {
		s.log = l
	}
}

func WithTracer(t trace.Tracer) Option {
	return func(s *settings) {
		s.tracer = t
	}
}
