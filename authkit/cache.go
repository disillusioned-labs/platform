package authkit

import (
	"context"
	"crypto/rsa"
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
}

func newKeyCache(
	fetch func(context.Context) (map[string]*rsa.PublicKey, []byte, error),
	limiter *rate.Limiter,
) *keyCache {
	return &keyCache{
		keys:    map[string]*rsa.PublicKey{},
		fetch:   fetch,
		limiter: limiter,
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
		return nil, err
	}

	if k, ok := c.get(kid); ok {
		return k, nil
	}
	return nil, ErrUnknownKey
}

func (c *keyCache) refresh(ctx context.Context) error {
	keys, _, err := c.fetch(ctx)
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
