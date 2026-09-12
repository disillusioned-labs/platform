package authkit

import (
	"context"
	"crypto/rsa"
	"log/slog"
	"sync"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
)

type keyCache struct {
	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey

	fetch   func(context.Context) (map[string]*rsa.PublicKey, []byte, error)
	limiter *rate.Limiter
	group   singleflight.Group
	store   Store
	log     *slog.Logger
}

func newKeyCache(
	fetch func(context.Context) (map[string]*rsa.PublicKey, []byte, error),
	limiter *rate.Limiter,
	store Store,
	log *slog.Logger,
) *keyCache {
	return &keyCache{
		keys:    map[string]*rsa.PublicKey{},
		fetch:   fetch,
		limiter: limiter,
		store:   store,
		log:     log,
	}
}

func (c *keyCache) get(kid string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	k, ok := c.keys[kid]
	c.mu.RUnlock()
	return k, ok
}

func (c *keyCache) replace(keys map[string]*rsa.PublicKey) {
	c.mu.Lock()
	c.keys = keys
	c.mu.Unlock()
}

func (c *keyCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if k, ok := c.get(kid); ok {
		return k, nil
	}

	if !c.limiter.Allow() {
		return nil, ErrUnknownKey
	}

	if _, err, _ := c.group.Do("refresh", func() (any, error) {
		return nil, c.refresh(ctx)
	}); err != nil {
		// The live fetch failed (identity unreachable): the persisted JWKS
		// document is the last remaining source of keys, so a restarting
		// consumer keeps verifying instead of rejecting everything. If the
		// kid is genuinely absent from that document too, the original
		// refresh error is preserved - stale keys never mask a broken fetch.
		if _, loadErr, _ := c.group.Do("load-store", func() (any, error) {
			return nil, c.loadFromStore(ctx)
		}); loadErr != nil {
			return nil, err
		}

		if k, ok := c.get(kid); ok {
			return k, nil
		}
		return nil, err
	}

	if k, ok := c.get(kid); ok {
		return k, nil
	}
	return nil, ErrUnknownKey
}

func (c *keyCache) refresh(ctx context.Context) error {
	keys, raw, err := c.fetch(ctx)
	if err != nil {
		return err
	}
	c.replace(keys)
	c.save(ctx, raw)
	return nil
}

// save persists the raw JWKS document for the next boot's fallback. A failed
// save only degrades the fallback - it never fails a request, because the
// keys are already live in memory.
func (c *keyCache) save(ctx context.Context, raw []byte) {
	if c.store == nil || len(raw) == 0 {
		return
	}

	if err := c.store.Save(ctx, raw); err != nil {
		c.log.WarnContext(ctx, "authkit: persisting jwks failed", "error", err)
	}
}

// loadFromStore rebuilds the in-memory keys from the persisted JWKS document.
// Used by Bootstrap when a live fetch is impossible (identity down).
func (c *keyCache) loadFromStore(ctx context.Context) error {
	if c.store == nil {
		return ErrNoStoredJWKS
	}

	raw, err := c.store.Load(ctx)
	if err != nil {
		return err
	}

	keys, err := parseJWKS(raw)
	if err != nil {
		return err
	}

	c.replace(keys)
	return nil
}

func (c *keyCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.keys)
}
