package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/lasseh/whynoipv6/internal/postgres/db"
)

// The pgtype null conversions — one conversion, one home, so no two
// consumers can drift on the null rule (the keysetquery.go treatment
// applied to scalar plumbing).

// TS wraps a time as a pgtype.Timestamptz.
func TS(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// TSPtr wraps an optional time as a pgtype.Timestamptz, invalid when nil.
func TSPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// Date wraps a time as a pgtype.Date.
func Date(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// TimePtr unwraps a nullable timestamptz, normalized to UTC.
func TimePtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	u := ts.Time.UTC()
	return &u
}

// StatusPtr renders a nullable status enum as its wire string.
func StatusPtr(v *db.Ipv6Status) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}
