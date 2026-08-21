package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/store"
	modsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrClosed) {
		return fmt.Errorf("store sqlite %s: %w", op, err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store sqlite %s: %w", op, store.ErrNotFound)
	}
	if isConstraint(err) {
		return fmt.Errorf("store sqlite %s: %w", op, store.ErrConflict)
	}
	return fmt.Errorf("store sqlite %s: %w", op, err)
}

func isConstraint(err error) bool {
	var se *modsqlite.Error
	if errors.As(err, &se) {
		code := se.Code()
		return code == sqlite3.SQLITE_CONSTRAINT || code&0xff == sqlite3.SQLITE_CONSTRAINT
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "constraint") || strings.Contains(msg, "unique")
}

func nano(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().UTC().UnixNano()
	}
	return t.UTC().UnixNano()
}

// unixNano stores a timestamp that may be absent. Zero time is 0, unlike
// [nano], which fills in now for created/updated columns.
func unixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func fromNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func nullNano(n sql.NullInt64) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return fromNano(n.Int64)
}

func toNullNano(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UTC().UnixNano(), Valid: true}
}

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(b) == 0 || string(b) == "null" {
		return "[]", nil
	}
	return string(b), nil
}

func unmarshalSlice[T any](s string) ([]T, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return []T{}, nil
	}
	var out []T
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

type workspaceJSON struct {
	Repo string `json:"repo,omitempty"`
	Ref  string `json:"ref,omitempty"`
}

func marshalWorkspace(w store.WorkspaceSource) (string, error) {
	if w.Repo == "" && w.Ref == "" {
		return "{}", nil
	}
	b, err := json.Marshal(workspaceJSON{Repo: w.Repo, Ref: w.Ref})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalWorkspace(s string) (store.WorkspaceSource, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" || s == "null" {
		return store.WorkspaceSource{}, nil
	}
	var w workspaceJSON
	if err := json.Unmarshal([]byte(s), &w); err != nil {
		return store.WorkspaceSource{}, err
	}
	return store.WorkspaceSource{Repo: w.Repo, Ref: w.Ref}, nil
}

type crossExamJSON struct {
	Verdict       string `json:"verdict"`
	ReviewerModel string `json:"reviewer_model"`
	Reasoning     string `json:"reasoning"`
	AtNano        int64  `json:"at_unix_nano"`
}

func marshalCrossExam(c *store.CrossExam) (string, error) {
	if c == nil {
		return "", nil
	}
	b, err := json.Marshal(crossExamJSON{
		Verdict:       c.Verdict,
		ReviewerModel: c.ReviewerModel,
		Reasoning:     c.Reasoning,
		AtNano:        nano(c.At),
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalCrossExam(s string) (*store.CrossExam, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return nil, nil
	}
	var c crossExamJSON
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, err
	}
	return &store.CrossExam{
		Verdict:       c.Verdict,
		ReviewerModel: c.ReviewerModel,
		Reasoning:     c.Reasoning,
		At:            fromNano(c.AtNano),
	}, nil
}

type effectJSON struct {
	Type              string `json:"type"`
	Path              string `json:"path,omitempty"`
	Diff              string `json:"diff,omitempty"`
	PreconditionHash  string `json:"precondition_hash,omitempty"`
	PostconditionHash string `json:"postcondition_hash,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	LeaseID           string `json:"lease_id,omitempty"`
	CostEstimate      string `json:"cost_estimate,omitempty"`
}

func marshalEffects(effects []store.PlanEffect) (string, error) {
	if effects == nil {
		effects = []store.PlanEffect{}
	}
	js := make([]effectJSON, 0, len(effects))
	for _, e := range effects {
		js = append(js, effectJSON{
			Type:              e.Type,
			Path:              e.Path,
			Diff:              e.Diff,
			PreconditionHash:  e.PreconditionHash,
			PostconditionHash: e.PostconditionHash,
			IdempotencyKey:    e.IdempotencyKey,
			LeaseID:           e.LeaseID.String(),
			CostEstimate:      e.CostEstimate,
		})
	}
	return marshalJSON(js)
}

func unmarshalEffects(s string) ([]store.PlanEffect, error) {
	raw, err := unmarshalSlice[effectJSON](s)
	if err != nil {
		return nil, err
	}
	out := make([]store.PlanEffect, 0, len(raw))
	for _, e := range raw {
		var lease store.LeaseID
		if e.LeaseID != "" {
			lease, err = store.ParseLeaseID(e.LeaseID)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, store.PlanEffect{
			Type:              e.Type,
			Path:              e.Path,
			Diff:              e.Diff,
			PreconditionHash:  e.PreconditionHash,
			PostconditionHash: e.PostconditionHash,
			IdempotencyKey:    e.IdempotencyKey,
			LeaseID:           lease,
			CostEstimate:      e.CostEstimate,
		})
	}
	return out, nil
}

type credentialJSON struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
}

func marshalCredentials(creds []store.CredentialConstraint) (string, error) {
	if creds == nil {
		creds = []store.CredentialConstraint{}
	}
	js := make([]credentialJSON, 0, len(creds))
	for _, c := range creds {
		js = append(js, credentialJSON{Provider: c.Provider, Kind: c.Kind})
	}
	return marshalJSON(js)
}

func unmarshalCredentials(s string) ([]store.CredentialConstraint, error) {
	raw, err := unmarshalSlice[credentialJSON](s)
	if err != nil {
		return nil, err
	}
	out := make([]store.CredentialConstraint, 0, len(raw))
	for _, c := range raw {
		out = append(out, store.CredentialConstraint{Provider: c.Provider, Kind: c.Kind})
	}
	return out, nil
}

type findingJSON struct {
	Path string `json:"path"`
	Rule string `json:"rule"`
	Line int    `json:"line,omitempty"`
}

func marshalFindings(findings []store.SecretScanFinding) (string, error) {
	if findings == nil {
		findings = []store.SecretScanFinding{}
	}
	js := make([]findingJSON, 0, len(findings))
	for _, f := range findings {
		js = append(js, findingJSON{Path: f.Path, Rule: f.Rule, Line: f.Line})
	}
	return marshalJSON(js)
}

func unmarshalFindings(s string) ([]store.SecretScanFinding, error) {
	raw, err := unmarshalSlice[findingJSON](s)
	if err != nil {
		return nil, err
	}
	out := make([]store.SecretScanFinding, 0, len(raw))
	for _, f := range raw {
		out = append(out, store.SecretScanFinding{Path: f.Path, Rule: f.Rule, Line: f.Line})
	}
	return out, nil
}

func affectedOne(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return wrap(op, err)
	}
	if n == 0 {
		return wrap(op, store.ErrNotFound)
	}
	return nil
}

type listKind int

const (
	listNewestFirst listKind = iota
	listOldestFirst
)

func cursorWhere(tsCol string, kind listKind, cursor string) (clause string, args []any, err error) {
	if strings.TrimSpace(cursor) == "" {
		return "", nil, nil
	}
	ts, id, err := store.DecodeCursor(cursor)
	if err != nil {
		return "", nil, err
	}
	n := ts.UnixNano()
	if kind == listOldestFirst {
		return `(` + tsCol + ` > ? OR (` + tsCol + ` = ? AND id > ?))`, []any{n, n, id}, nil
	}
	return `(` + tsCol + ` < ? OR (` + tsCol + ` = ? AND id < ?))`, []any{n, n, id}, nil
}

func pageOf[T any](items []T, limit int, created func(T) time.Time, id func(T) string) store.Page[T] {
	page := store.Page[T]{Items: items}
	if page.Items == nil {
		page.Items = []T{}
	}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.Next = store.EncodeCursor(created(last), id(last))
	}
	return page
}
