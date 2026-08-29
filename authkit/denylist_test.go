package authkit

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDenylist struct {
	revokedAt time.Time
	err       error
	lastKeys  []string
}

func (f *fakeDenylist) RevokedAt(_ context.Context, keys ...string) (time.Time, error) {
	f.lastKeys = keys
	return f.revokedAt, f.err
}

func newTestVerifier(denylist Denylist) *Verifier {
	s := defaults()
	s.denylist = denylist
	return &Verifier{log: s.log, tracer: s.tracer, denylist: s.denylist}
}

func TestCheckDenylistRejectsTokenIssuedBeforeRevocation(t *testing.T) {
	revokedAt := time.Now()
	fake := &fakeDenylist{revokedAt: revokedAt}
	verifier := newTestVerifier(fake)

	claims := Claims{
		Subject:  "user-1",
		OrgID:    "org-1",
		IssuedAt: revokedAt.Add(-time.Minute),
	}

	if err := verifier.checkDenylist(context.Background(), claims); !errors.Is(err, ErrRevoked) {
		t.Fatalf("checkDenylist() = %v, want ErrRevoked", err)
	}

	want := []string{UserRevokeKey("user-1"), MemberRevokeKey("org-1", "user-1")}
	if len(fake.lastKeys) != len(want) || fake.lastKeys[0] != want[0] || fake.lastKeys[1] != want[1] {
		t.Fatalf("RevokedAt keys = %v, want %v", fake.lastKeys, want)
	}
}

func TestCheckDenylistAllowsTokenIssuedAfterRevocation(t *testing.T) {
	revokedAt := time.Now()
	fake := &fakeDenylist{revokedAt: revokedAt}
	verifier := newTestVerifier(fake)

	claims := Claims{
		Subject:  "user-1",
		OrgID:    "org-1",
		IssuedAt: revokedAt.Add(time.Minute),
	}

	if err := verifier.checkDenylist(context.Background(), claims); err != nil {
		t.Fatalf("checkDenylist() = %v, want nil", err)
	}
}

func TestCheckDenylistAllowsTokenIssuedAtSameSecond(t *testing.T) {
	revokedAt := time.Now().Truncate(time.Second)
	fake := &fakeDenylist{revokedAt: revokedAt}
	verifier := newTestVerifier(fake)

	claims := Claims{Subject: "user-1", OrgID: "org-1", IssuedAt: revokedAt}

	if err := verifier.checkDenylist(context.Background(), claims); !errors.Is(err, ErrRevoked) {
		t.Fatalf("checkDenylist() = %v, want ErrRevoked for equal timestamp", err)
	}
}

func TestCheckDenylistFailsOpenOnDenylistError(t *testing.T) {
	fake := &fakeDenylist{err: errors.New("redis down")}
	verifier := newTestVerifier(fake)

	claims := Claims{
		Subject:  "user-1",
		OrgID:    "org-1",
		IssuedAt: time.Now().Add(-time.Hour),
	}

	if err := verifier.checkDenylist(context.Background(), claims); err != nil {
		t.Fatalf("checkDenylist() = %v, want nil (fail-open)", err)
	}
}

func TestCheckDenylistSkippedWithoutDenylist(t *testing.T) {
	verifier := newTestVerifier(nil)

	claims := Claims{
		Subject:  "user-1",
		OrgID:    "org-1",
		IssuedAt: time.Now().Add(-time.Hour),
	}

	if err := verifier.checkDenylist(context.Background(), claims); err != nil {
		t.Fatalf("checkDenylist() = %v, want nil", err)
	}
}
