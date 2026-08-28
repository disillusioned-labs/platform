package authkit

import (
	"context"
	"crypto/rsa"
)

type KeySource interface {
	Fetch(ctx context.Context) (map[string]*rsa.PublicKey, []byte, error)
}
