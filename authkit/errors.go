package authkit

import "errors"

var (
	ErrNoToken = errors.New("authkit: no bearer token")

	ErrMalformed = errors.New("authkit: malformed token")

	ErrUnknownKey = errors.New("authkit: unknown key id")

	ErrInvalidSignature = errors.New("authkit: invalid signature")

	ErrExpired = errors.New("authkit: token expired")

	ErrInvalidClaims = errors.New("authkit: invalid claims")

	ErrRevoked = errors.New("authkit: token revoked")

	ErrNoKeys = errors.New("authkit: no keys available")
)
