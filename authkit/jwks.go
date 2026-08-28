package authkit

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const maxJWKSBytes = 1 << 20

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

func fetchJWKS(ctx context.Context, tracer trace.Tracer, client *http.Client, url string) (map[string]*rsa.PublicKey, []byte, error) {
	ctx, span := tracer.Start(ctx, "authkit.fetchJWKS", trace.WithAttributes(
		attribute.String("authkit.jwks_url", url),
	))
	defer span.End()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "build request failed")
		return nil, nil, err
	}

	response, err := client.Do(request)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "http request failed")
		return nil, nil, err
	}
	defer response.Body.Close()

	span.SetAttributes(attribute.Int("http.status_code", response.StatusCode))
	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("authkit: jwks status %d", response.StatusCode)
		span.RecordError(err)
		span.SetStatus(codes.Error, "unexpected status")
		return nil, nil, err
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBytes))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read body failed")
		return nil, nil, err
	}
	span.SetAttributes(attribute.Int("authkit.jwks_bytes", len(raw)))

	keys, err := parseJWKS(raw)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "parse jwks failed")
		return nil, nil, err
	}
	span.SetAttributes(attribute.Int("authkit.keys_parsed", len(keys)))

	return keys, raw, nil
}

func parseJWKS(raw []byte) (map[string]*rsa.PublicKey, error) {
	var set jwkSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return nil, fmt.Errorf("authkit: jwks decode: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		if k.Kid == "" {
			continue
		}
		pub, err := jwkToPublicKey(k)
		if err != nil {
			return nil, err
		}
		keys[k.Kid] = pub
	}

	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	return keys, nil
}

func jwkToPublicKey(k jwk) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("authkit: jwk %q modulus: %w", k.Kid, err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("authkit: jwk %q exponent: %w", k.Kid, err)
	}

	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() > 1<<31-1 || e.Int64() < 3 {
		return nil, fmt.Errorf("authkit: jwk %q exponent out of range", k.Kid)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: int(e.Int64()),
	}, nil
}
