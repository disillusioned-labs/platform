// Package http provides a hardened JSON HTTP client with bounded response size
// and OpenTelemetry tracing/metrics for every call.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultMaxResponseBodySize = 1 << 20 // 1 MiB
	defaultHTTPTimeout         = 10 * time.Second
	defaultUserAgent           = "disillusioned-labs-http-client/1.0"

	// instrumentationName identifies this package as the source of spans
	// and metrics, mirroring how otelpgx tags every DB span.
	instrumentationName = "github.com/disillusioned-labs/platform/http"
)

// ErrResponseTooLarge is returned when the response body exceeds the
// configured limit. Callers should treat this as a definitive error rather
// than silently consuming a truncated payload.
var ErrResponseTooLarge = errors.New("http response body exceeds configured limit")

// Doer abstracts *http.Client so HTTPClient can be unit-tested with a fake
// transport instead of hitting the network.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Logger is a minimal structured-logging interface. Most logging libraries
// (slog, zap's SugaredLogger, logrus, etc.) already satisfy this shape or
// can be trivially adapted to it.
type Logger interface {
	Warn(msg string, kv ...any)
}

type noopLogger struct{}

func (noopLogger) Warn(string, ...any) {}

// handleErr reports err to the global OTel error handler, mirroring
// otelhttp's own internal helper of the same name.
func handleErr(err error) {
	if err != nil {
		otel.Handle(err)
	}
}

// HTTPClient is a hardened JSON HTTP client: bounded response size and
// OpenTelemetry tracing/metrics for every call.
type HTTPClient struct {
	client          Doer
	maxResponseSize int64
	userAgent       string
	logger          Logger

	tracer trace.Tracer

	// callsFailedCounter counts Do calls that returned a non-nil error to
	// the caller.
	callsFailedCounter metric.Int64Counter

	// callsInFlight tracks logical Do calls currently in progress.
	callsInFlight metric.Int64UpDownCounter

	// responseTooLargeCounter counts responses rejected by
	// WithMaxResponseSize.
	responseTooLargeCounter metric.Int64Counter
}

// Telemetry layering: every Do call produces exactly ONE span (the package's
// own, built in Do). Duration is NOT re-measured here - the otelhttp
// transport already records http.client.request.duration in seconds per the
// stable OTel semantic conventions, and a histogram in this package would
// emit two duration series for one call. What remains here are the
// instruments otelhttp cannot know about: logical failures, in-flight calls,
// and responses rejected by the size cap.

// Option configures an HTTPClient.
type Option func(*HTTPClient)

// WithMaxResponseSize overrides the default 1 MiB response body cap.
func WithMaxResponseSize(n int64) Option {
	return func(c *HTTPClient) { c.maxResponseSize = n }
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *HTTPClient) { c.userAgent = ua }
}

// WithLogger injects a logger used for close warnings. Defaults to a
// no-op logger if nil.
func WithLogger(l Logger) Option {
	return func(c *HTTPClient) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithTracerProvider sets the TracerProvider used to build this client's
// tracer. Defaults to otel.GetTracerProvider() (the global one) if unset.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *HTTPClient) {
		if tp != nil {
			c.tracer = tp.Tracer(instrumentationName)
		}
	}
}

// WithMeterProvider sets the MeterProvider used for this client's metrics.
// Defaults to otel.GetMeterProvider() (the global one) if unset.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *HTTPClient) {
		if mp == nil {
			return
		}
		meter := mp.Meter(instrumentationName)

		callsFailedCounter, err := meter.Int64Counter(
			"http.client.calls.failed",
			metric.WithDescription("Number of HTTPClient.Do calls that returned an error to the caller"),
			metric.WithUnit("{call}"),
		)
		handleErr(err)
		c.callsFailedCounter = callsFailedCounter

		callsInFlight, err := meter.Int64UpDownCounter(
			"http.client.calls.inflight",
			metric.WithDescription("Number of HTTPClient.Do calls currently in progress"),
			metric.WithUnit("{call}"),
		)
		handleErr(err)
		c.callsInFlight = callsInFlight

		responseTooLargeCounter, err := meter.Int64Counter(
			"http.client.response.body_limit_exceeded",
			metric.WithDescription("Number of responses rejected for exceeding the configured max size"),
			metric.WithUnit("{response}"),
		)
		handleErr(err)
		c.responseTooLargeCounter = responseTooLargeCounter
	}
}

// NewHTTPClient builds an HTTPClient. Pass nil to use a default *http.Client
// whose transport is wrapped with otelhttp, giving every outbound request a
// child span plus trace-context propagation for free.
func NewHTTPClient(client Doer, opts ...Option) *HTTPClient {
	if client == nil {
		client = &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: WrapTransport(http.DefaultTransport),
		}
	}
	c := &HTTPClient{
		client:          client,
		maxResponseSize: defaultMaxResponseBodySize,
		userAgent:       defaultUserAgent,
		logger:          noopLogger{},
		tracer:          otel.GetTracerProvider().Tracer(instrumentationName),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.callsFailedCounter == nil {
		WithMeterProvider(otel.GetMeterProvider())(c)
	}
	return c
}

// WrapTransport wraps rt with otelhttp so requests sent through it get W3C
// trace-context propagation plus the semconv http.client.request.duration
// metric (seconds). Its own span is silenced with a no-op tracer provider:
// spans come from HTTPClient.Do, so one call produces exactly one span -
// without this, every call would carry a duplicate transport-level span.
func WrapTransport(rt http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(rt,
		otelhttp.WithTracerProvider(trace.NewNoopTracerProvider()),
	)
}

// Do executes an HTTP request with JSON body marshaling and a bounded
// response read.
//
// The whole call runs inside one span, so the http.client.call.duration
// metric records the duration of the single HTTP round trip.
func (c *HTTPClient) Do(
	ctx context.Context,
	method string,
	url string,
	headers map[string]string,
	body any,
) (statusCode int, response []byte, err error) {
	payload, err := marshalBody(body)
	if err != nil {
		return 0, nil, err
	}

	ctx, span := c.tracer.Start(ctx, spanName(method, url),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(method),
			// Query string is redacted: query params are the most common
			// place for tokens and API keys to travel (access_token=...),
			// and they must never reach span attributes.
			semconv.URLFull(safeURL(url)),
		),
	)
	defer span.End()

	methodAttr := metric.WithAttributes(semconv.HTTPRequestMethodKey.String(method))

	c.callsInFlight.Add(ctx, 1, methodAttr)
	defer c.callsInFlight.Add(ctx, -1, methodAttr)

	defer func() {
		if err != nil {
			c.callsFailedCounter.Add(ctx, 1, metric.WithAttributes(
				semconv.HTTPRequestMethodKey.String(method),
				semconv.ErrorType(err),
			))
		}
	}()

	statusCode, response, err = c.doOnce(ctx, method, url, headers, payload)
	if err != nil {
		finishSpan(span, statusCode, err)
		return statusCode, response, err
	}

	finishSpan(span, statusCode, nil)
	return statusCode, response, nil
}

func (c *HTTPClient) doOnce(
	ctx context.Context,
	method, url string,
	headers map[string]string,
	payload []byte,
) (int, []byte, error) {
	req, err := buildRequest(ctx, method, url, headers, payload, c.userAgent)
	if err != nil {
		return 0, nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("execute http request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		if cerr := resp.Body.Close(); cerr != nil {
			c.logger.Warn("close response body failed", "err", cerr)
		}
	}()

	data, err := readLimited(resp.Body, c.maxResponseSize)
	if err != nil {
		if errors.Is(err, ErrResponseTooLarge) {
			c.responseTooLargeCounter.Add(ctx, 1, metric.WithAttributes(
				semconv.HTTPRequestMethodKey.String(method),
			))
		}
		return resp.StatusCode, nil, fmt.Errorf("read http response: %w", err)
	}

	return resp.StatusCode, data, nil
}

// finishSpan records the final outcome of a Do call on its span.
func finishSpan(span trace.Span, statusCode int, err error) {
	if statusCode > 0 {
		span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))
	}
	switch {
	case err != nil:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	case statusCode >= 400:
		span.SetStatus(codes.Error, fmt.Sprintf("http %d", statusCode))
	default:
		span.SetStatus(codes.Ok, "")
	}
}

// spanName builds a low-cardinality span name from method + host + path.
// The query string is deliberately dropped to avoid high-cardinality and
// sensitive data in span names.
func spanName(method, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return method
	}
	return method + " " + u.Host + u.Path
}

// safeURL renders a URL without its query string and userinfo, so secrets
// travelling in either place never reach span attributes. Unparseable input
// is returned as-is - the request itself will fail downstream anyway.
func safeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.RawQuery == "" && u.User == nil {
		return rawURL
	}
	clone := *u
	clone.RawQuery = ""
	clone.User = nil
	return clone.String()
}

func marshalBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return payload, nil
}

func buildRequest(
	ctx context.Context,
	method, url string,
	headers map[string]string,
	payload []byte,
	userAgent string,
) (*http.Request, error) {
	var bodyReader io.Reader
	if payload != nil {
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", userAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return req, nil
}

// readLimited reads up to limit+1 bytes so it can distinguish "body exactly
// at the limit" from "body was truncated".
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}
