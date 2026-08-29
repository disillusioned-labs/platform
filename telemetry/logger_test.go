package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type recordingHandler struct {
	records []slog.Record
	attrs   []slog.Attr
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.attrs = append(h.attrs, attrs...)
	return h
}

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingHandler) find(key string) (slog.Value, bool) {
	var value slog.Value
	found := false

	for _, r := range h.records {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key {
				value = a.Value
				found = true
			}
			return true
		})
	}

	return value, found
}

func TestRequestIDAttachedToEveryLine(t *testing.T) {
	rec := &recordingHandler{}
	log := slog.New(traceHandler{rec})

	ctx := WithRequestID(context.Background(), "req-123")
	log.InfoContext(ctx, "first")
	log.ErrorContext(ctx, "second", "error", errors.New("boom"))

	for i, r := range rec.records {
		if r.Message != "first" && r.Message != "second" {
			continue
		}

		value, ok := rec.find("request_id")
		if !ok {
			t.Fatalf("record %d: request_id attr missing", i)
		}
		if value.String() != "req-123" {
			t.Fatalf("record %d: request_id = %q, want %q", i, value.String(), "req-123")
		}
	}
}

func TestRequestIDOmittedWhenAbsent(t *testing.T) {
	rec := &recordingHandler{}
	log := slog.New(traceHandler{rec})

	log.InfoContext(context.Background(), "without request id")

	if _, ok := rec.find("request_id"); ok {
		t.Fatal("request_id attr present without WithRequestID")
	}
}

func TestWithRequestIDIgnoresEmpty(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	if got := RequestIDFrom(ctx); got != "" {
		t.Fatalf("RequestIDFrom() = %q, want empty", got)
	}
}
