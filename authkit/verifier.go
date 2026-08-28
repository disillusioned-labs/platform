package authkit

import (
	"context"
	"crypto/rsa"
	"errors"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

type Verifier struct {
	cache  *keyCache
	issuer string
	skew   time.Duration
	log    *slog.Logger
	tracer trace.Tracer

	refreshEvery time.Duration
	stop         chan struct{}

	errorHandler ErrorHandler
}

func New(cfg Config, opts ...Option) *Verifier {
	s := defaults()
	for _, o := range opts {
		o(&s)
	}

	v := &Verifier{
		issuer:       cfg.Issuer,
		skew:         s.clockSkew,
		log:          s.log,
		tracer:       s.tracer,
		stop:         make(chan struct{}),
		errorHandler: s.errorHandler,
	}

	limiter := rate.NewLimiter(rate.Every(s.unknownKidRate), s.unknownKidBurst)

	var fetch func(context.Context) (map[string]*rsa.PublicKey, []byte, error)
	if s.keySource != nil {
		fetch = s.keySource.Fetch
	} else {
		fetch = func(ctx context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			return fetchJWKS(ctx, s.tracer, s.httpClient, cfg.JWKSURL)
		}
	}

	v.cache = newKeyCache(fetch, limiter)

	v.refreshEvery = s.refreshEvery
	return v
}

func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	ctx, span := v.tracer.Start(ctx, "authkit.Verify")
	defer span.End()

	tok, err := jwt.ParseWithClaims(raw, &jwt.MapClaims{},
		func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, ErrUnknownKey
			}
			span.SetAttributes(attribute.String("authkit.kid", kid))
			return v.cache.key(ctx, kid)
		},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithLeeway(v.skew),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		mapped := mapJWTError(err)
		span.RecordError(err)
		span.SetStatus(codes.Error, mapped.Error())
		v.log.WarnContext(ctx, "authkit: token verification failed", "error", mapped)
		return Claims{}, mapped
	}

	mc, ok := tok.Claims.(*jwt.MapClaims)
	if !ok {
		err := ErrInvalidClaims
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		v.log.WarnContext(ctx, "authkit: invalid token claims", "error", err)
		return Claims{}, err
	}

	c, err := toClaims(*mc)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		v.log.WarnContext(ctx, "authkit: invalid token claims", "error", err)
		return Claims{}, err
	}
	span.SetAttributes(
		attribute.String("authkit.subject", c.Subject),
		attribute.String("authkit.org_id", c.OrgID),
	)
	return c, nil
}

func (v *Verifier) Bootstrap(ctx context.Context) error {
	ctx, span := v.tracer.Start(ctx, "authkit.Bootstrap")
	defer span.End()

	if err := v.cache.refresh(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "jwks fetch failed at boot")
		v.log.WarnContext(ctx, "authkit: jwks fetch failed at boot", "err", err)
		return err
	}
	span.SetAttributes(attribute.Int("authkit.keys_loaded", v.cache.len()))
	v.log.InfoContext(ctx, "authkit: jwks loaded", "keys", v.cache.len())
	return nil
}

func (v *Verifier) Start(ctx context.Context) {
	t := time.NewTicker(v.refreshEvery)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-t.C:
				spanCtx, span := v.tracer.Start(ctx, "authkit.ScheduledRefresh")
				if err := v.cache.refresh(spanCtx); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, "scheduled refresh failed")
					v.log.WarnContext(ctx, "authkit: scheduled refresh failed", "err", err)
				} else {
					span.SetAttributes(attribute.Int("authkit.keys_loaded", v.cache.len()))
				}
				span.End()
			case <-v.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (v *Verifier) Stop() { close(v.stop) }

func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrExpired
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrInvalidSignature
	case errors.Is(err, ErrUnknownKey):
		return ErrUnknownKey
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrMalformed
	default:
		return ErrInvalidClaims
	}
}

func toClaims(m jwt.MapClaims) (Claims, error) {
	str := func(k string) string {
		s, _ := m[k].(string)
		return s
	}
	num := func(k string) int64 {
		f, _ := m[k].(float64)
		return int64(f)
	}

	c := Claims{
		Subject:   str("sub"),
		OrgID:     str("org_id"),
		Role:      str("role"),
		Issuer:    str("iss"),
		IssuedAt:  time.Unix(num("iat"), 0),
		ExpiresAt: time.Unix(num("exp"), 0),
	}

	if c.Subject == "" || c.OrgID == "" || c.Role == "" || num("iat") == 0 {
		return Claims{}, ErrInvalidClaims
	}
	return c, nil
}
