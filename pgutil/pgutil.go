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

func TextFromString(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func Int4(v int32) pgtype.Int4 {
	if v == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: v, Valid: true}
}

func Int4Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	val := v.Int32
	return &val
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

func StringPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func TimeValue(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
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
