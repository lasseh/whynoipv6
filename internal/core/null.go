package core

import (
	"database/sql"
	"time"
)

// TODO: Pls help, i don't know what i'm doing!

// TimeNull is a wrapper for sql.NullTime.
func TimeNull(t sql.NullTime) time.Time {
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

// NullTime is a wrapper for sql.NullTime.
func NullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{
		Time:  t,
		Valid: true,
	}
}

// NullString is a wrapper for sql.NullString.
func NullString(s string) sql.NullString {
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

// StringNull is a wrapper for sql.NullString.
func StringNull(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// IntNull is a wrapper for sql.NullInt64.
func IntNull(i sql.NullInt64) int64 {
	if i.Valid {
		return i.Int64
	}
	return 0
}

// NullInt is a wrapper for sql.NullInt64.
func NullInt(i int64) sql.NullInt64 {
	return sql.NullInt64{
		Int64: i,
		Valid: true,
	}
}

// BoolNull is a wrapper for sql.NullBool.
func BoolNull(b sql.NullBool) bool {
	if b.Valid {
		return b.Bool
	}
	return false
}

// NullBool is a wrapper for sql.NullBool.
func NullBool(b bool) sql.NullBool {
	return sql.NullBool{
		Bool:  b,
		Valid: true,
	}
}
