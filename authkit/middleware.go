package authkit

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/codes"
)

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := v.tracer.Start(
			r.Context(),
			"authkit.Middleware",
		)
		defer span.End()

		token, err := Bearer(r)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "authentication failed")

			v.log.ErrorContext(
				ctx,
				"authentication failed",
				"error", err,
			)

			v.writeError(w, r, err)
			return
		}

		claims, err := v.Verify(ctx, token)
		if err != nil {
			v.writeError(w, r, err)
			return
		}

		next.ServeHTTP(
			w,
			r.WithContext(WithClaims(ctx, claims)),
		)
	})
}

func Bearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", ErrNoToken
	}

	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return "", ErrNoToken
	}

	token := strings.TrimSpace(h[7:])
	if token == "" {
		return "", ErrNoToken
	}

	return token, nil
}

func (v *Verifier) writeError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if v.errorHandler != nil {
		v.errorHandler(w, r, err)
		return
	}

	w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
	http.Error(
		w,
		http.StatusText(http.StatusUnauthorized),
		http.StatusUnauthorized,
	)
}
