package jwks

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
)

type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func ParseRSAPublicKey(pemKey string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("failed to decode PEM")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	publicKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is %T, want *rsa.PublicKey", key)
	}

	return publicKey, nil
}

func PublicKeyToJWKS(pubPEM string, kid string) (JWK, error) {
	rsaPub, err := ParseRSAPublicKey(pubPEM)
	if err != nil {
		return JWK{}, fmt.Errorf("parse RSA public key: %w", err)
	}

	n := base64.RawURLEncoding.EncodeToString(rsaPub.N.Bytes())

	e := base64.RawURLEncoding.EncodeToString(
		[]byte{
			byte(rsaPub.E >> 16),
			byte(rsaPub.E >> 8),
			byte(rsaPub.E),
		},
	)

	return JWK{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		Alg: "RS256",
		N:   n,
		E:   e,
	}, nil
}
