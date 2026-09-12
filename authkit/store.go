package authkit

import "context"

// Store persists the raw JWKS document between verifier restarts. A service
// that boots while identity is down cannot fetch JWKS; with a store it
// verifies tokens from the last successfully fetched key set instead of
// failing to start.
//
// The verifier treats the store as a one-way write-through cache: Save
// failures are logged and never fail a request (stale keys are better than
// none), and Load is only consulted in Bootstrap after a live fetch has
// already failed. Implementations therefore do not need read consistency
// guarantees beyond "returns the last document some process saved".
//
// A nil Store (the default) keeps the previous memory-only behaviour.
type Store interface {
	// Load returns the raw JWKS document previously saved by Save.
	// Return an error (e.g. ErrNoStoredJWKS) when nothing has been saved yet.
	Load(ctx context.Context) ([]byte, error)
	// Save persists the raw JWKS document for later Load calls.
	Save(ctx context.Context, jwks []byte) error
}

// ErrNoStoredJWKS is returned by Store implementations when no JWKS document
// has been persisted yet. The verifier's Bootstrap treats it the same as any
// other store failure: without a live fetch there is nothing to verify with.
const ErrNoStoredJWKS = storeError("no stored jwks")

type storeError string

func (e storeError) Error() string { return string(e) }
