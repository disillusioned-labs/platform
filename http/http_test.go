package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// installSpanRecorder swaps the global tracer provider for one backed by a
// recorder - exactly what telemetry.Setup does in a real service - and
// restores the previous provider on cleanup.
func installSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return recorder
}

// TestSingleSpanPerCall pins the telemetry layering: WrapTransport silences
// the otelhttp span, so one Do call ends exactly one span - not two.
func TestSingleSpanPerCall(t *testing.T) {
	recorder := installSpanRecorder(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewHTTPClient(nil) // default client uses WrapTransport
	if _, _, err := client.Do(context.Background(), http.MethodGet, srv.URL+"/thing", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span per Do call, got %d", len(spans))
	}
}

// TestURLRedactedInSpan proves secrets in the query string (and userinfo)
// never reach span attributes or the span name.
func TestURLRedactedInSpan(t *testing.T) {
	recorder := installSpanRecorder(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	secret := "super-secret-access-token"
	url := srv.URL + "/ocr?access_token=" + secret + "&page=1"
	if _, _, err := NewHTTPClient(nil).Do(context.Background(), http.MethodGet, url, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	for _, attr := range span.Attributes() {
		if string(attr.Key) == "url.full" && strings.Contains(attr.Value.Emit(), secret) {
			t.Fatalf("secret leaked into url.full: %s", attr.Value.Emit())
		}
	}
	if strings.Contains(span.Name(), secret) || strings.Contains(span.Name(), "page=1") {
		t.Fatalf("query leaked into span name: %s", span.Name())
	}
	// The redacted URL must still be useful: path and scheme survive.
	var urlFull string
	for _, attr := range span.Attributes() {
		if string(attr.Key) == "url.full" {
			urlFull = attr.Value.AsString()
		}
	}
	if urlFull == "" || !strings.Contains(urlFull, "/ocr") {
		t.Fatalf("url.full missing path: %q", urlFull)
	}
}

// TestTooLargeMetric pins the package-specific instruments that otelhttp
// cannot know about: the size-cap rejection counter.
func TestTooLargeMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	client := NewHTTPClient(nil, WithMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 2<<20))) // 2 MiB > 1 MiB default cap
	}))
	defer srv.Close()

	if _, _, err := client.Do(context.Background(), http.MethodGet, srv.URL, nil, nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}

	var tooLarge int64
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, m := range data.ScopeMetrics {
		for _, k := range m.Metrics {
			if k.Name != "http.client.response.body_limit_exceeded" {
				continue
			}
			if sum, ok := k.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					tooLarge += dp.Value
				}
			}
		}
	}
	if tooLarge != 1 {
		t.Fatalf("body_limit_exceeded = %d, want 1", tooLarge)
	}
}
