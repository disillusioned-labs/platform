package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"static creds ok", Config{Region: "us-east-1", AccessKeyID: "ak", SecretAccessKey: "sk"}, false},
		{"default chain ok (no creds)", Config{Region: "us-east-1"}, false},
		{"region missing", Config{}, true},
		{"half credentials", Config{Region: "us-east-1", AccessKeyID: "ak"}, true},
		{"retry mode ok", Config{Region: "us-east-1", RetryMode: "adaptive"}, false},
		{"retry mode unknown", Config{Region: "us-east-1", RetryMode: "turbo"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// testClient points the client at an httptest server: Put/Get/Stat/Delete
// exercise the real SDK pipeline (middleware, checksums, signing) without AWS.
func testClient(t *testing.T, handler http.Handler, allowlist ...string) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := Config{
		Endpoint:        srv.URL,
		Region:          "us-east-1",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
		UsePathStyle:    true,
		BucketAllowlist: allowlist,
	}
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

type storedObject struct {
	body        []byte
	contentType string
}

func TestPutGetDeleteRoundTrip(t *testing.T) {
	objects := map[string]storedObject{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			objects[key] = storedObject{body, r.Header.Get("Content-Type")}
			w.Header().Set("ETag", `"abc123"`)
		case http.MethodGet:
			obj, ok := objects[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", obj.contentType)
			w.Write(obj.body)
		case http.MethodDelete:
			delete(objects, key)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	c, err := New(context.Background(), Config{
		Endpoint: srv.URL, Region: "us-east-1",
		AccessKeyID: "ak", SecretAccessKey: "sk", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	etag, err := c.Put(context.Background(), "docs", "2026/08/nota.jpg", int64(len("nota-bytes")), "image/jpeg", bytes.NewReader([]byte("nota-bytes")))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if etag != `"abc123"` {
		t.Fatalf("unexpected etag %q", etag)
	}

	body, ct, err := c.Get(context.Background(), "docs", "2026/08/nota.jpg")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer body.Close()
	got, _ := io.ReadAll(body)
	if string(got) != "nota-bytes" || ct != "image/jpeg" {
		t.Fatalf("round trip mismatch: %q / %q", got, ct)
	}

	if err := c.Delete(context.Background(), "docs", "2026/08/nota.jpg"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := c.Get(context.Background(), "docs", "2026/08/nota.jpg"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestBucketAllowlistRefusedBeforeNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request reached the server for a non-allowlisted bucket")
	}))
	defer srv.Close()

	c, err := New(context.Background(), Config{
		Endpoint: srv.URL, Region: "us-east-1",
		AccessKeyID: "ak", SecretAccessKey: "sk",
		BucketAllowlist: []string{"docs"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Put(context.Background(), "other", "k", 1, "", bytes.NewReader([]byte("x")))
	if !errors.Is(err, ErrBucketNotAllowed) {
		t.Fatalf("expected ErrBucketNotAllowed, got %v", err)
	}
}

func TestPresignGetIsOfflineAndSigned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("presigning must not touch the network")
	}))
	defer srv.Close()

	c, err := New(context.Background(), Config{
		Endpoint: srv.URL, Region: "us-east-1",
		AccessKeyID: "ak", SecretAccessKey: "sk", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	url, err := c.PresignGet(context.Background(), "docs", "2026/08/nota.jpg", 15*time.Minute, "nota.jpg")
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	for _, want := range []string{"X-Amz-Signature", "X-Amz-Expires=900", "nota.jpg"} {
		if !strings.Contains(url, want) {
			t.Fatalf("presigned url missing %q: %s", want, url)
		}
	}
}

func TestSpanEmitted(t *testing.T) {
	// otelaws reads the GLOBAL providers (same as redisotel), so the test
	// installs the recorder there - this is exactly what telemetry.Setup does
	// in a real service.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c, err := New(context.Background(), Config{
		Endpoint: srv.URL, Region: "us-east-1",
		AccessKeyID: "ak", SecretAccessKey: "sk", UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Put(context.Background(), "docs", "k.txt", 2, "text/plain", bytes.NewReader([]byte("ok"))); err != nil {
		t.Fatalf("Put: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("no span recorded; otelaws middleware missing?")
	}
	found := false
	for _, s := range spans {
		for _, attr := range s.Attributes() {
			if string(attr.Key) == "aws.s3.bucket" && attr.Value.AsString() == "docs" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no span carries aws.s3.bucket=docs; spans: %d", len(spans))
	}
}
