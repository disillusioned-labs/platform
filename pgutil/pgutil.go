// Package pgutil centralizes conversions between Go pointer types and
// pgtype nullable types used by sqlc-generated code.
package pgutil

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func Text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func String(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func TextPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func TextValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func Time(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func Now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
}

func TimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
