package grpc

import "context"

type requestIDKey struct{}

// RequestIDFromContext returns the request ID stored in ctx by a server
// interceptor. Returns empty string if not present.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}

	return ""
}

func requestIDToContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
