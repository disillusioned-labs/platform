package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// --- ValidateRequestID ---

func TestValidateRequestID_ValidUUID(t *testing.T) {
	if !ValidateRequestID("550e8400-e29b-41d4-a716-446655440000") {
		t.Error("expected valid UUID to pass")
	}
}

func TestValidateRequestID_ValidULID(t *testing.T) {
	if !ValidateRequestID("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Error("expected valid ULID to pass")
	}
}

func TestValidateRequestID_Empty(t *testing.T) {
	if ValidateRequestID("") {
		t.Error("expected empty string to fail")
	}
}

func TestValidateRequestID_TooLong(t *testing.T) {
	if ValidateRequestID("a]2345678901234567890123456789012345678901234567890123456789012345") {
		t.Error("expected >64 chars to fail")
	}
}

func TestValidateRequestID_NullByte(t *testing.T) {
	if ValidateRequestID("550e8400-e29b-41d4-a716-4466554400\x00") {
		t.Error("expected null byte to fail")
	}
}

func TestValidateRequestID_Newline(t *testing.T) {
	if ValidateRequestID("550e8400-e29b-41d4-a716-4466554400\n") {
		t.Error("expected newline to fail")
	}
}

func TestValidateRequestID_CarriageReturn(t *testing.T) {
	if ValidateRequestID("550e8400-e29b-41d4-a716-4466554400\r") {
		t.Error("expected carriage return to fail")
	}
}

func TestValidateRequestID_InvalidUUIDFormat(t *testing.T) {
	if ValidateRequestID("550e8400e29b41d4a716446655440000") {
		t.Error("expected UUID without dashes to fail")
	}
}

func TestValidateRequestID_InvalidULIDChars(t *testing.T) {
	if ValidateRequestID("01ARZ3NDEKTSV4RRFFQ69G5FA!") {
		t.Error("expected invalid ULID chars to fail")
	}
}

func TestValidateRequestID_WrongLength(t *testing.T) {
	if ValidateRequestID("abc123") {
		t.Error("expected wrong length to fail")
	}
}

// --- RequestIDFromContext ---

func TestRequestIDFromContext_Present(t *testing.T) {
	ctx := context.Background()
	ctx = requestIDToContext(ctx, "test-id-123")

	if got := RequestIDFromContext(ctx); got != "test-id-123" {
		t.Errorf("expected test-id-123, got %s", got)
	}
}

func TestRequestIDFromContext_Absent(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

// --- UnaryRequestIDServer ---

func TestUnaryRequestIDServer_ExtractsFromMetadata(t *testing.T) {
	md := metadata.Pairs(MetadataRequestID, "550e8400-e29b-41d4-a716-446655440000")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedRequestID string

	handler := func(ctx context.Context, req any) (any, error) {
		capturedRequestID = RequestIDFromContext(ctx)
		return "response", nil
	}

	interceptor := UnaryRequestIDServer(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	resp, err := interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != "response" {
		t.Errorf("expected response, got %v", resp)
	}

	if capturedRequestID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected extracted request ID, got %s", capturedRequestID)
	}
}

func TestUnaryRequestIDServer_GeneratesWhenMissing(t *testing.T) {
	ctx := context.Background()

	var capturedRequestID string

	handler := func(ctx context.Context, req any) (any, error) {
		capturedRequestID = RequestIDFromContext(ctx)
		return nil, nil
	}

	interceptor := UnaryRequestIDServer(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedRequestID == "" {
		t.Error("expected generated request ID, got empty")
	}

	if !ValidateRequestID(capturedRequestID) {
		t.Errorf("generated request ID is not valid: %s", capturedRequestID)
	}
}

func TestUnaryRequestIDServer_ReplacesInvalidID(t *testing.T) {
	md := metadata.Pairs(MetadataRequestID, "invalid-id!@#$")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var capturedRequestID string

	handler := func(ctx context.Context, req any) (any, error) {
		capturedRequestID = RequestIDFromContext(ctx)
		return nil, nil
	}

	interceptor := UnaryRequestIDServer(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	_, err := interceptor(ctx, nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedRequestID == "invalid-id!@#$" {
		t.Error("expected invalid ID to be replaced")
	}

	if !ValidateRequestID(capturedRequestID) {
		t.Errorf("replacement request ID is not valid: %s", capturedRequestID)
	}
}

func TestUnaryRequestIDServer_LogsSlowRequest(t *testing.T) {
	var logged bool
	var logMsg string

	slowLogger := &testLogger{
		warnFn: func(msg string, args ...any) {
			logged = true
			logMsg = msg
		},
	}

	handler := func(ctx context.Context, req any) (any, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, nil
	}

	interceptor := UnaryRequestIDServer(slowLogger)
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	// Use a very short threshold to trigger slow logging
	origThreshold := slowRequestThreshold
	slowRequestThreshold = 1 * time.Millisecond
	defer func() { slowRequestThreshold = origThreshold }()

	_, err := interceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !logged {
		t.Error("expected slow request to be logged")
	}

	if logMsg != "slow grpc request" {
		t.Errorf("expected 'slow grpc request', got %s", logMsg)
	}
}

// --- UnaryForwardMetadataClient ---

func TestUnaryForwardMetadataClient_ForwardsWhitelistedKeys(t *testing.T) {
	incoming := metadata.Pairs(
		"x-request-id", "req-123",
		"x-tenant-id", "tenant-456",
		"x-secret", "should-not-forward",
	)
	ctx := metadata.NewIncomingContext(context.Background(), incoming)

	var outgoingMD metadata.MD

	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		outgoingMD, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}

	interceptor := UnaryForwardMetadataClient([]string{"x-request-id", "x-tenant-id"})

	err := interceptor(ctx, "/test/Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outgoingMD.Get("x-request-id")[0] != "req-123" {
		t.Error("expected x-request-id to be forwarded")
	}

	if outgoingMD.Get("x-tenant-id")[0] != "tenant-456" {
		t.Error("expected x-tenant-id to be forwarded")
	}

	if len(outgoingMD.Get("x-secret")) > 0 {
		t.Error("expected x-secret to NOT be forwarded")
	}
}

func TestUnaryForwardMetadataClient_SkipsEmptyValues(t *testing.T) {
	incoming := metadata.Pairs("x-request-id", "")
	ctx := metadata.NewIncomingContext(context.Background(), incoming)

	var outgoingMD metadata.MD

	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		outgoingMD, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}

	interceptor := UnaryForwardMetadataClient([]string{"x-request-id"})

	err := interceptor(ctx, "/test/Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outgoingMD) > 0 {
		t.Error("expected no outgoing metadata when value is empty")
	}
}

func TestUnaryForwardMetadataClient_EmptyWhitelist(t *testing.T) {
	incoming := metadata.Pairs("x-request-id", "req-123")
	ctx := metadata.NewIncomingContext(context.Background(), incoming)

	var outgoingMD metadata.MD

	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		outgoingMD, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}

	interceptor := UnaryForwardMetadataClient(nil)

	err := interceptor(ctx, "/test/Method", nil, nil, nil, invoker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outgoingMD) > 0 {
		t.Error("expected no outgoing metadata with empty whitelist")
	}
}

// --- test helpers ---

type testLogger struct {
	warnFn func(msg string, args ...any)
	errorFn func(msg string, args ...any)
}

func (l *testLogger) Warn(msg string, args ...any) {
	if l.warnFn != nil {
		l.warnFn(msg, args...)
	}
}

func (l *testLogger) Error(msg string, args ...any) {
	if l.errorFn != nil {
		l.errorFn(msg, args...)
	}
}
