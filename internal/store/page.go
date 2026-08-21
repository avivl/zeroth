package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultLimit is the page size when List callers omit Limit.
	DefaultLimit = 50
	// MaxLimit matches the OpenAPI Limit parameter maximum.
	MaxLimit = 200
)

// Page is one list result. Next is empty when there is no further page.
type Page[T any] struct {
	Items []T
	Next  string
}

// PageQuery is the cursor and limit shared by list methods.
type PageQuery struct {
	Limit  int
	Cursor string
}

// ClampLimit returns DefaultLimit when n is zero or negative, and caps at
// MaxLimit. Shared so SQLite and a later Postgres driver page the same way.
func ClampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

// EncodeCursor builds an opaque list cursor from a row's timestamp and id.
func EncodeCursor(t time.Time, id string) string {
	return strconv.FormatInt(t.UTC().UnixNano(), 10) + "|" + id
}

// DecodeCursor parses a cursor produced by EncodeCursor.
func DecodeCursor(raw string) (time.Time, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", fmt.Errorf("store cursor: empty: %w", ErrInvalid)
	}
	nanoStr, id, ok := strings.Cut(raw, "|")
	if !ok || id == "" {
		return time.Time{}, "", fmt.Errorf("store cursor %q: %w", raw, ErrInvalid)
	}
	nano, err := strconv.ParseInt(nanoStr, 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("store cursor %q: %w", raw, ErrInvalid)
	}
	return time.Unix(0, nano).UTC(), id, nil
}
