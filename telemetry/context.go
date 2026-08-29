package telemetry

import "context"

type requestIDContextKey struct{}

// WithRequestID returns a context carrying the HTTP request id, so every log
// line written inside the request correlates to the X-Request-ID the client
// can report back. Unlike the trace id, it survives with tracing disabled.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, id)
}

// RequestIDFrom returns the request id stored in ctx, or "".
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}
