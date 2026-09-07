package grpc

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// MetadataRequestID is the gRPC metadata key for request correlation.
	MetadataRequestID = "x-request-id"

	// maxRequestIDLength prevents abuse via oversized metadata values.
	maxRequestIDLength = 64
)

// slowRequestThreshold logs a warning when a request exceeds this duration.
// Var (not const) so tests can override the threshold.
var slowRequestThreshold = 500 * time.Millisecond

func UnaryDefaultTimeout(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if timeout <= 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func UnaryRecovery(logger Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if logger != nil {
					logger.Error(
						"grpc panic recovered",
						"method", info.FullMethod,
						"panic", fmt.Sprint(recovered),
						"stack", string(debug.Stack()),
					)
				}

				resp = nil
				err = status.Error(
					codes.Internal,
					"internal server error",
				)
			}
		}()

		return handler(ctx, req)
	}
}

func StreamRecovery(logger Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if logger != nil {
					logger.Error(
						"grpc stream panic recovered",
						"method", info.FullMethod,
						"panic", fmt.Sprint(recovered),
						"stack", string(debug.Stack()),
					)
				}

				err = status.Error(
					codes.Internal,
					"internal server error",
				)
			}
		}()

		return handler(srv, stream)
	}
}

// UnaryRequestIDServer extracts or generates a request ID from incoming gRPC
// metadata, stores it in context, logs duration, and returns it in response
// metadata. The request ID enables cross-service correlation in logs and traces.
//
// Interceptor order: Recovery → RequestID → Auth → Business logic.
func UnaryRequestIDServer(logger Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		requestID := extractOrCreateRequestID(ctx)

		ctx = requestIDToContext(ctx, requestID)

		resp, err := handler(ctx, req)

		duration := time.Since(start)

		logRequestCompletion(logger, info.FullMethod, requestID, duration, err)

		return resp, err
	}
}

// StreamRequestIDServer is the stream variant of UnaryRequestIDServer.
func StreamRequestIDServer(logger Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		requestID := extractOrCreateRequestID(stream.Context())

		wrapped := &requestIDStream{
			ServerStream: stream,
			requestID:    requestID,
		}

		err := handler(srv, wrapped)

		duration := time.Since(start)

		logRequestCompletion(logger, info.FullMethod, requestID, duration, err)

		return err
	}
}

// UnaryForwardMetadataClient forwards specified keys from the incoming
// context's metadata to outgoing gRPC metadata. Only keys present in the
// whitelist are forwarded; empty values are skipped.
func UnaryForwardMetadataClient(keys []string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = forwardMetadata(ctx, keys)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamForwardMetadataClient is the stream variant of UnaryForwardMetadataClient.
func StreamForwardMetadataClient(keys []string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = forwardMetadata(ctx, keys)

		return streamer(ctx, desc, cc, method, opts...)
	}
}

// ValidateRequestID checks whether id is a valid request ID.
// Valid formats: UUID (36 chars) or ULID (26 chars base32).
// Maximum length is 64 characters. Rejects strings containing null bytes
// or newlines to prevent header injection.
func ValidateRequestID(id string) bool {
	if len(id) == 0 || len(id) > maxRequestIDLength {
		return false
	}

	if strings.ContainsAny(id, "\x00\n\r") {
		return false
	}

	if len(id) == 36 {
		return isValidUUID(id)
	}

	if len(id) == 26 {
		return isValidULID(id)
	}

	return false
}

// --- internal helpers ---

func extractOrCreateRequestID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(MetadataRequestID); len(vals) > 0 {
			id := vals[0]

			if ValidateRequestID(id) {
				return id
			}
		}
	}

	return uuid.New().String()
}

func logRequestCompletion(logger Logger, method, requestID string, duration time.Duration, err error) {
	if logger == nil {
		return
	}

	args := []any{
		"method", method,
		"request_id", requestID,
		"duration_ms", duration.Milliseconds(),
	}

	if err != nil {
		args = append(args, "error", err.Error())
	}

	if duration > slowRequestThreshold {
		logger.Warn("slow grpc request", args...)
	} else if err != nil {
		logger.Error("grpc request failed", args...)
	}
}

func forwardMetadata(ctx context.Context, keys []string) context.Context {
	if len(keys) == 0 {
		return ctx
	}

	incoming, _ := metadata.FromIncomingContext(ctx)

	outgoing, _ := metadata.FromOutgoingContext(ctx)
	outgoing = outgoing.Copy()

	for _, key := range keys {
		if vals := incoming.Get(key); len(vals) > 0 && vals[0] != "" {
			outgoing.Set(key, vals...)
		}
	}

	if len(outgoing) == 0 {
		return ctx
	}

	return metadata.NewOutgoingContext(ctx, outgoing)
}

func isValidUUID(id string) bool {
	if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}

	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}

		if !isHexChar(c) {
			return false
		}
	}

	return true
}

func isValidULID(id string) bool {
	for _, c := range id {
		if !isCrockfordBase32(c) {
			return false
		}
	}

	return true
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isCrockfordBase32(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'H') || (c >= 'J' && c <= 'N') || (c >= 'P' && c <= 'T') || (c >= 'V' && c <= 'X') || c == 'Y' || c == 'Z'
}

// requestIDStream wraps a grpc.ServerStream to inject the request ID into
// the stream context.
type requestIDStream struct {
	grpc.ServerStream
	requestID string
}

func (s *requestIDStream) Context() context.Context {
	return requestIDToContext(s.ServerStream.Context(), s.requestID)
}
