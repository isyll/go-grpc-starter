package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Conversion helpers between pgx/pgtype values and plain Go types used by the
// domain entities. Repositories use these when mapping sqlc rows to entities.

func Time(t pgtype.Timestamptz) time.Time {
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

func TimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time
	return &tt
}

func TS(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func TSPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// Str dereferences a nullable text column into a plain string ("" when NULL).
func Str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Ptr returns a pointer to s, so an empty string is stored as '' (not NULL).
func Ptr(s string) *string {
	return &s
}

// NullStr maps "" to a NULL text param and any other value to a pointer.
func NullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func Int64(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

func Bool(b *bool) bool {
	return b != nil && *b
}

func UUIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

func ParseUUID(s string) pgtype.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

// TimeOfDay parses "HH:MM" or "HH:MM:SS" into a pgtype.Time; NULL when nil/empty.
func TimeOfDay(s *string) pgtype.Time {
	if s == nil || *s == "" {
		return pgtype.Time{}
	}
	var h, m, sec int
	if n, _ := fmt.Sscanf(*s, "%d:%d:%d", &h, &m, &sec); n < 2 {
		return pgtype.Time{}
	}
	micros := int64(h*3600+m*60+sec) * 1_000_000
	return pgtype.Time{Microseconds: micros, Valid: true}
}

func TimeOfDayStr(t pgtype.Time) *string {
	if !t.Valid {
		return nil
	}
	total := t.Microseconds / 1_000_000
	out := fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	return &out
}
