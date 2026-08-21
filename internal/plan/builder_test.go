package plan

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
)

func TestBuildRows(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "modify", Path: "README.md", Diff: "+hi"},
			{Type: "create", Path: "new.go", Diff: "package new"},
		},
		Observed: map[string]string{"README.md": "pre-readme"},
	})
	if p.Status != StatusDraft {
		t.Fatalf("status %s", p.Status)
	}
	if p.Hash == "" || p.Hash != HashOf(p) {
		t.Fatalf("hash %q", p.Hash)
	}
	if len(p.Rows) != 2 {
		t.Fatalf("rows %d", len(p.Rows))
	}
	if p.Rows[0].Op != OpModify || p.Rows[0].Precondition != "pre-readme" || p.Rows[0].Lease != "lease-1" {
		t.Fatalf("modify row %+v", p.Rows[0])
	}
	if p.Rows[1].Op != OpCreate || p.Rows[1].Precondition != "" {
		t.Fatalf("create row %+v", p.Rows[1])
	}
	if p.Rows[0].IdempotencyKey == "" || p.Rows[0].Postcondition == "" {
		t.Fatal("expected idempotency key and postcondition")
	}
}

func TestBuildDestroyAndMemory(t *testing.T) {
	t.Parallel()
	p := mustBuild(t, Draft{
		Effects: []Proposed{
			{Type: "destroy", Path: "gone.md", Diff: "-all"},
			{Type: "memory_proposal", Path: "session/style", Diff: "prefer table tests"},
		},
		Observed: map[string]string{"gone.md": "old"},
	})
	if p.Rows[0].Op != OpDestroy || p.Rows[0].Precondition != "old" {
		t.Fatalf("destroy %+v", p.Rows[0])
	}
	if p.Rows[0].Postcondition == p.Rows[1].Postcondition {
		t.Fatal("destroy postcondition is the empty digest, not a payload hash")
	}
	if p.Rows[1].Op != OpMemoryProposal || p.Rows[1].Target != "session/style" {
		t.Fatalf("memory %+v", p.Rows[1])
	}
}

func TestBuildUnexpressible(t *testing.T) {
	t.Parallel()
	base := validDraft()
	tests := []struct {
		name   string
		mutate func(*Draft)
		reason string
	}{
		{
			name: "unknown type",
			mutate: func(d *Draft) {
				d.Effects = []Proposed{{Type: "shell", Path: "x.sh", Diff: "rm -rf /"}}
			},
			reason: "unknown type",
		},
		{
			name: "missing lease",
			mutate: func(d *Draft) {
				d.Lease = ""
				d.Leases = nil
			},
			reason: "no lease",
		},
		{
			name: "create on existing",
			mutate: func(d *Draft) {
				d.Effects = []Proposed{{Type: "create", Path: "README.md", Diff: "x"}}
				d.Observed = map[string]string{"README.md": "exists"}
			},
			reason: "create on existing",
		},
		{
			name: "modify without precondition",
			mutate: func(d *Draft) {
				d.Effects = []Proposed{{Type: "modify", Path: "README.md", Diff: "x"}}
				d.Observed = map[string]string{}
			},
			reason: "no precondition",
		},
		{
			name: "path traversal",
			mutate: func(d *Draft) {
				d.Effects = []Proposed{{Type: "modify", Path: "../secret", Diff: "x"}}
				d.Observed = map[string]string{"../secret": "h"}
			},
			reason: "workspace-relative",
		},
		{
			name: "empty payload",
			mutate: func(d *Draft) {
				d.Effects = []Proposed{{Type: "modify", Path: "README.md", Diff: "  "}}
			},
			reason: "missing payload",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := base
			d.Effects = append([]Proposed(nil), base.Effects...)
			tt.mutate(&d)
			_, err := Build(d)
			if !errors.Is(err, ErrUnexpressible) {
				t.Fatalf("err %v, want Unexpressible", err)
			}
			var u *UnexpressibleError
			if !errors.As(err, &u) {
				t.Fatalf("want UnexpressibleError, got %T", err)
			}
			if !strings.Contains(u.Reason, tt.reason) && !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("reason %q error %q want substring %q", u.Reason, err, tt.reason)
			}
		})
	}
}

func TestBuildRejectsInvalidDraft(t *testing.T) {
	t.Parallel()
	d := validDraft()
	d.Summary = ""
	if _, err := Build(d); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty summary: %v", err)
	}
	d = validDraft()
	d.ExpiresAt = time.Time{}
	if _, err := Build(d); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing expiry: %v", err)
	}
	d = validDraft()
	d.Effects = nil
	if _, err := Build(d); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no effects: %v", err)
	}
}

func TestParseOp(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{OpCreate, OpModify, OpDestroy, OpMemoryProposal} {
		got, ok := ParseOp(string(op))
		if !ok || got != op {
			t.Fatalf("%s: got %s ok=%v", op, got, ok)
		}
		if op.Symbol() == "" || op.Kind() != policy.EffectKind(op) {
			t.Fatalf("symbol/kind %s", op)
		}
	}
	if _, ok := ParseOp("shell"); ok {
		t.Fatal("shell must not parse")
	}
}

func validDraft() Draft {
	id, _ := ParseID("plan-1")
	sess, _ := session.ParseID("sess-1")
	return Draft{
		ID:        id,
		SessionID: sess,
		Summary:   "touch files",
		Effects:   []Proposed{{Type: "modify", Path: "README.md", Diff: "+hi"}},
		Observed:  map[string]string{"README.md": "abc"},
		Lease:     "lease-1",
		ExpiresAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Scope:     "scope-a",
		Now:       time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	}
}

func mustBuild(t *testing.T, extra Draft) Plan {
	t.Helper()
	d := validDraft()
	if extra.Effects != nil {
		d.Effects = extra.Effects
	}
	if extra.Observed != nil {
		d.Observed = extra.Observed
	}
	if extra.Lease != "" {
		d.Lease = extra.Lease
	}
	if extra.Leases != nil {
		d.Leases = extra.Leases
	}
	if extra.Credentials != nil {
		d.Credentials = extra.Credentials
	}
	if extra.CostCeiling != 0 {
		d.CostCeiling = extra.CostCeiling
	}
	p, err := Build(d)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
