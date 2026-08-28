package jwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

const Algorithm = "RS256"

type Claims struct {
	Subject        string
	OrganizationID string
	Role           string
	Issuer         string
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

type registeredClaims struct {
	OrganizationID string `json:"org_id"`
	Role           string `json:"role"`
	gojwt.RegisteredClaims
}

func Sign(key *rsa.PrivateKey, kid string, claims Claims) (string, error) {
	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, registeredClaims{
		OrganizationID: claims.OrganizationID,
		Role:           claims.Role,
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   claims.Subject,
			Issuer:    claims.Issuer,
			IssuedAt:  gojwt.NewNumericDate(claims.IssuedAt),
			ExpiresAt: gojwt.NewNumericDate(claims.ExpiresAt),
		},
	})
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("decode pem block: no block found")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse pkcs1 private key: %w", err)
	}
	return key, nil
}
