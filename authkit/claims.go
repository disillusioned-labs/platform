package authkit

import (
	"context"
	"time"
)

type Claims struct {
	Subject   string
	OrgID     string
	Role      string
	Issuer    string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type ctxKey struct{}

func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}
