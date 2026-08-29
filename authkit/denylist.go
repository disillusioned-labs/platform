package authkit

import (
	"context"
	"time"
)

// Denylist answers when a revoke key was written, so the Verifier can reject
// access tokens issued before a revocation even though their signature and
// expiry are still valid. Implementations must be safe for concurrent use.
//
// The zero time means "nothing revoked for these keys". An implementation
// error is reported as err; the Verifier fails open on it - denylist is an
// acceleration layer, never a gate.
type Denylist interface {
	RevokedAt(ctx context.Context, keys ...string) (time.Time, error)
}

// Revoke keys are owned here so the verifier that reads them and the
// identity service that writes them cannot drift apart. Identity is the only
// writer; every other service reads them through the Verifier.
const (
	revokeUserPrefix   = "revoke:user:"
	revokeMemberPrefix = "revoke:member:"
)

// UserRevokeKey denies every access token of a user, across organizations.
func UserRevokeKey(subject string) string {
	return revokeUserPrefix + subject
}

// MemberRevokeKey denies a user's access tokens carrying one organization's
// org_id, leaving tokens for their other organizations alone.
func MemberRevokeKey(orgID, subject string) string {
	return revokeMemberPrefix + orgID + ":" + subject
}

// RevokeKeys lists the keys a token with these claims must be checked
// against.
func RevokeKeys(subject, orgID string) []string {
	return []string{
		UserRevokeKey(subject),
		MemberRevokeKey(orgID, subject),
	}
}
