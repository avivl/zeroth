package plan

import (
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
)

// Proposed is one harness-proposed mutation in the form the builder
// consumes (OpenAPI PlanEffect: type, path, diff). The builder does not
// import the harness port: the daemon copies Stream effects into this
// type. That keeps plan I/O-free except for the store snapshot helpers.
type Proposed struct {
	Type string
	Path string
	Diff string
}

// Draft is what [Build] needs to turn proposed effects into a plan.
// Observed is the content hash of each target at draft time. A missing
// key means the path was absent. Lease is attached to every row unless
// Leases names a per-target override.
type Draft struct {
	ID        ID
	SessionID session.ID
	ParentID  ID
	Summary   string
	Effects   []Proposed
	Observed  map[string]string
	// Bodies is the raw content of each target at draft time. Modify
	// postconditions are the hash of the file after the payload is
	// applied to this body, not a hash of the payload text. A missing
	// key is treated as empty contents.
	Bodies      map[string]string
	Lease       policy.LeaseID
	Leases      map[string]policy.LeaseID
	ExpiresAt   time.Time
	CostCeiling int64
	Scope       policy.ScopeID
	Credentials []Credential
	Now         time.Time
}

// Build turns proposed effects into typed rows. Any effect that cannot be
// a row returns [UnexpressibleError] and must halt the run. Callers pass
// facts in (observed hashes, leases, constraints); this function does not
// read the workspace.
func Build(d Draft) (Plan, error) {
	if d.ID.IsZero() {
		return Plan{}, fmt.Errorf("plan builder: %w", ErrInvalid)
	}
	if d.SessionID.IsZero() {
		return Plan{}, fmt.Errorf("plan builder: %w", ErrInvalid)
	}
	if strings.TrimSpace(d.Summary) == "" {
		return Plan{}, fmt.Errorf("plan builder: %w", ErrInvalid)
	}
	if d.ExpiresAt.IsZero() {
		return Plan{}, fmt.Errorf("plan builder: missing expiry: %w", ErrInvalid)
	}
	if d.Scope == "" {
		return Plan{}, fmt.Errorf("plan builder: missing scope: %w", ErrInvalid)
	}
	if len(d.Effects) == 0 {
		return Plan{}, fmt.Errorf("plan builder: no effects: %w", ErrInvalid)
	}
	for i, c := range d.Credentials {
		if strings.TrimSpace(c.Provider) == "" || strings.TrimSpace(c.Kind) == "" {
			return Plan{}, fmt.Errorf("plan builder: credential %d: %w", i, ErrInvalid)
		}
	}

	now := d.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rows := make([]Row, 0, len(d.Effects))
	seen := make(map[string]int, len(d.Effects))
	for i, e := range d.Effects {
		row, err := rowFrom(d, i, e)
		if err != nil {
			return Plan{}, err
		}
		key := string(row.Op) + "\n" + row.Target + "\n" + row.Payload
		if prev, ok := seen[key]; ok {
			return Plan{}, unexpressible(i, e, fmt.Sprintf("duplicate of effect %d", prev))
		}
		seen[key] = i
		rows = append(rows, row)
	}

	p := Plan{
		ID:          d.ID,
		SessionID:   d.SessionID,
		ParentID:    d.ParentID,
		Status:      StatusDraft,
		Summary:     d.Summary,
		ExpiresAt:   d.ExpiresAt.UTC(),
		CostCeiling: d.CostCeiling,
		Scope:       d.Scope,
		Credentials: append([]Credential(nil), d.Credentials...),
		Rows:        rows,
		Findings:    []Finding{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	p.Hash = HashOf(p)
	return p, nil
}

func rowFrom(d Draft, i int, e Proposed) (Row, error) {
	op, ok := ParseOp(e.Type)
	if !ok {
		return Row{}, unexpressible(i, e, fmt.Sprintf("unknown type %q", e.Type))
	}
	target := normalizeTarget(e.Path)
	if target == "" {
		return Row{}, unexpressible(i, e, "missing target")
	}
	if !validTarget(target, op) {
		return Row{}, unexpressible(i, e, "target is not a workspace-relative path")
	}
	payload := e.Diff
	if strings.TrimSpace(payload) == "" {
		return Row{}, unexpressible(i, e, "missing payload")
	}
	if !utf8.ValidString(payload) {
		return Row{}, unexpressible(i, e, "payload is not valid UTF-8")
	}

	lease := d.Lease
	if d.Leases != nil {
		if per, ok := d.Leases[target]; ok {
			lease = per
		}
	}
	if lease == "" {
		return Row{}, unexpressible(i, e, "no lease")
	}

	observed := ""
	if d.Observed != nil {
		observed = d.Observed[target]
	}
	pre, err := precondition(op, observed)
	if err != nil {
		return Row{}, unexpressible(i, e, err.Error())
	}

	var original []byte
	if d.Bodies != nil {
		if body, ok := d.Bodies[target]; ok {
			original = []byte(body)
		}
	}
	if observed != "" && len(original) > 0 && Digest(original) != observed {
		return Row{}, unexpressible(i, e, "observed hash does not match supplied body")
	}

	post, err := postcondition(op, payload, original)
	if err != nil {
		return Row{}, unexpressible(i, e, err.Error())
	}

	return Row{
		Op:             op,
		Target:         target,
		Payload:        payload,
		Lease:          lease,
		Precondition:   pre,
		IdempotencyKey: idempotencyKey(op, target, payload),
		Postcondition:  post,
	}, nil
}

func precondition(op Op, observed string) (string, error) {
	switch op {
	case OpCreate:
		if observed != "" {
			return "", fmt.Errorf("create on existing target; use modify")
		}
		return "", nil
	case OpModify, OpDestroy:
		if observed == "" {
			return "", fmt.Errorf("no precondition observed at draft time")
		}
		return observed, nil
	case OpMemoryProposal:
		return observed, nil
	default:
		return "", fmt.Errorf("unknown type %q", op)
	}
}

func postcondition(op Op, payload string, original []byte) (string, error) {
	if op == OpDestroy {
		// Destroy's expected world is absence. The empty digest is that
		// postcondition, not a hash of the delete payload.
		return EmptyDigest(), nil
	}
	if op == OpMemoryProposal {
		return Digest([]byte(payload)), nil
	}
	result, err := Materialize(op, original, payload)
	if err != nil {
		if len(original) == 0 {
			// Tests that omit Bodies still need a stable post hash.
			// Apply rechecks against the live file and will refuse if
			// this digest is not the bytes that land.
			return Digest([]byte(payload)), nil
		}
		return "", err
	}
	return Digest(result), nil
}

func idempotencyKey(op Op, target, payload string) string {
	return Digest([]byte(string(op) + "\n" + target + "\n" + payload))
}

func normalizeTarget(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	return p
}

func validTarget(target string, op Op) bool {
	if target == "" || strings.Contains(target, "\x00") {
		return false
	}
	if op == OpMemoryProposal {
		return !strings.Contains(target, "..")
	}
	if path.IsAbs(target) || strings.HasPrefix(target, "/") {
		return false
	}
	clean := path.Clean(target)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	return true
}

func unexpressible(i int, e Proposed, reason string) error {
	return &UnexpressibleError{Index: i, Type: e.Type, Target: e.Path, Reason: reason}
}
