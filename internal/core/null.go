package core

import (
	"database/sql"
	"time"
)

// TimeNull converts a sql.NullTime value to a time.Time value.
// If the sql.NullTime value is not valid, it returns the zero value of time.Time.
func TimeNull(t sql.NullTime) time.Time {
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

// NullTime converts a time.Time value to a sql.NullTime value.
// If the time.Time value is zero, it returns an invalid sql.NullTime value.
func NullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{
		Time:  t,
		Valid: true,
	}
}

// NullString converts a string value to a sql.NullString value.
// It always returns a valid sql.NullString value.
func NullString(s string) sql.NullString {
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

// StringNull converts a sql.NullString value to a string value.
// If the sql.NullString value is not valid, it returns an empty string.
func StringNull(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// IntNull converts a sql.NullInt64 value to an int64 value.
// If the sql.NullInt64 value is not valid, it returns 0.
func IntNull(i sql.NullInt64) int64 {
	if i.Valid {
		return i.Int64
	}
	return 0
}

// NullInt converts an int64 value to a sql.NullInt64 value.
// It always returns a valid sql.NullInt64 value.
func NullInt(i int64) sql.NullInt64 {
	return sql.NullInt64{
		Int64: i,
		Valid: true,
	}
}

// BoolNull converts a sql.NullBool value to a bool value.
// If the sql.NullBool value is not valid, it returns false.
func BoolNull(b sql.NullBool) bool {
	if b.Valid {
		return b.Bool
	}
	return false
}

// NullBool converts a bool value to a sql.NullBool value.
// It always returns a valid sql.NullBool value.
func NullBool(b bool) sql.NullBool {
	return sql.NullBool{
		Bool:  b,
		Valid: true,
	}
}
