package authkit

import (
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type memoryStore struct {
	mu   sync.Mutex
	jwks []byte
}

func (m *memoryStore) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.jwks) == 0 {
		return nil, ErrNoStoredJWKS
	}
	return m.jwks, nil
}

func (m *memoryStore) Save(_ context.Context, jwks []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.jwks = jwks
	return nil
}

// testJWKS builds a syntactically valid JWKS document from a fresh 1024-bit
// key - the tests never verify signatures, they only exercise cache/store
// plumbing, so key strength does not matter.
func testJWKS(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(crand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	b64 := func(bz []byte) string { return base64.RawURLEncoding.EncodeToString(bz) }
	doc := fmt.Sprintf(
		`{"keys":[{"kid":"test-key","kty":"RSA","alg":"RS256","use":"sig","n":%q,"e":%q}]}`,
		b64(key.N.Bytes()),
		b64(big.NewInt(int64(key.E)).Bytes()),
	)

	return []byte(doc)
}

func TestRefreshPersistsRawJWKS(t *testing.T) {
	store := &memoryStore{}
	cache := newKeyCache(
		func(context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			raw := testJWKS(t)
			keys, err := parseJWKS(raw)
			return keys, raw, err
		},
		rate.NewLimiter(rate.Every(time.Minute), 1),
		store,
		slog.Default(),
	)

	if err := cache.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	raw, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("store load after refresh: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("stored jwks must not be empty")
	}
}

func TestBootstrapFallsBackToStoreWhenFetchFails(t *testing.T) {
	store := &memoryStore{}
	if err := store.Save(context.Background(), testJWKS(t)); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	fetchFailures := 0
	cache := newKeyCache(
		func(context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			fetchFailures++
			return nil, nil, errors.New("identity unreachable")
		},
		rate.NewLimiter(rate.Every(time.Minute), 1),
		store,
		slog.Default(),
	)

	// The path Bootstrap takes when the live fetch fails.
	if err := cache.refresh(context.Background()); err == nil {
		t.Fatal("refresh must fail when fetch fails")
	}
	if err := cache.loadFromStore(context.Background()); err != nil {
		t.Fatalf("load from store: %v", err)
	}

	if _, ok := cache.get("test-key"); !ok {
		t.Fatal("keys from persisted jwks must be served after fallback")
	}
	if fetchFailures != 1 {
		t.Fatalf("want exactly one fetch attempt, got %d", fetchFailures)
	}
}

func TestLoadFromStoreWithoutStoreFails(t *testing.T) {
	cache := newKeyCache(
		func(context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			return nil, nil, errors.New("down")
		},
		rate.NewLimiter(rate.Every(time.Minute), 1),
		nil,
		slog.Default(),
	)

	if err := cache.loadFromStore(context.Background()); !errors.Is(err, ErrNoStoredJWKS) {
		t.Fatalf("want ErrNoStoredJWKS, got %v", err)
	}
}

func TestKeyFallsBackToStoreOnFetchFailure(t *testing.T) {
	store := &memoryStore{}
	if err := store.Save(context.Background(), testJWKS(t)); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	cache := newKeyCache(
		func(context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			return nil, nil, errors.New("identity unreachable")
		},
		rate.NewLimiter(rate.Every(time.Minute), 1),
		store,
		slog.Default(),
	)

	// Unknown kid + dead identity: the persisted document keeps verification
	// alive instead of failing the request.
	k, err := cache.key(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("key with store fallback: %v", err)
	}
	if k == nil || k.N == nil {
		t.Fatal("expected the persisted key to be returned")
	}
}

func TestKeyWithoutStoreStillFailsWhenFetchFails(t *testing.T) {
	cache := newKeyCache(
		func(context.Context) (map[string]*rsa.PublicKey, []byte, error) {
			return nil, nil, errors.New("identity unreachable")
		},
		rate.NewLimiter(rate.Every(time.Minute), 1),
		nil,
		slog.Default(),
	)

	if _, err := cache.key(context.Background(), "test-key"); err == nil {
		t.Fatal("without a store a failed fetch must surface")
	}
}
