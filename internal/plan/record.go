package plan

import (
	"fmt"

	"github.com/avivl/zeroth/internal/policy"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
)

// Record is the store snapshot of p. Extra store columns (cross-exam,
// findings, review) round-trip so a later lifecycle step does not strip
// them. Hash is stored, not recomputed by the backend.
func (p Plan) Record() (store.Plan, error) {
	id, err := store.ParsePlanID(p.ID.String())
	if err != nil {
		return store.Plan{}, fmt.Errorf("plan record: %w", err)
	}
	sess, err := store.ParseSessionID(p.SessionID.String())
	if err != nil {
		return store.Plan{}, fmt.Errorf("plan record: %w", err)
	}
	var parent store.PlanID
	if !p.ParentID.IsZero() {
		parent, err = store.ParsePlanID(p.ParentID.String())
		if err != nil {
			return store.Plan{}, fmt.Errorf("plan record: %w", err)
		}
	}
	scope, err := store.ParseScopeID(string(p.Scope))
	if err != nil {
		return store.Plan{}, fmt.Errorf("plan record: %w", err)
	}
	effects := make([]store.PlanEffect, 0, len(p.Rows))
	for _, r := range p.Rows {
		lease, err := store.ParseLeaseID(string(r.Lease))
		if err != nil {
			return store.Plan{}, fmt.Errorf("plan record: %w", err)
		}
		effects = append(effects, store.PlanEffect{
			Type:              string(r.Op),
			Path:              r.Target,
			Diff:              r.Payload,
			PreconditionHash:  r.Precondition,
			PostconditionHash: r.Postcondition,
			IdempotencyKey:    r.IdempotencyKey,
			LeaseID:           lease,
			CostEstimate:      r.CostEstimate,
		})
	}
	creds := make([]store.CredentialConstraint, 0, len(p.Credentials))
	for _, c := range p.Credentials {
		creds = append(creds, store.CredentialConstraint{Provider: c.Provider, Kind: c.Kind})
	}
	out := store.Plan{
		ID:                 id,
		SessionID:          sess,
		ParentPlanID:       parent,
		Status:             string(p.Status),
		Summary:            p.Summary,
		Hash:               string(p.Hash),
		ExpiresAt:          p.ExpiresAt,
		CostCeiling:        p.CostCeiling,
		ScopeID:            scope,
		Credentials:        creds,
		Effects:            effects,
		SecretScanFindings: findingsToStore(p.Findings),
		ReviewComment:      p.ReviewComment,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
	if p.CrossExam != nil {
		cx := *p.CrossExam
		out.CrossExam = &store.CrossExam{
			Verdict:       cx.Verdict,
			ReviewerModel: cx.ReviewerModel,
			Reasoning:     cx.Reasoning,
			At:            cx.At,
		}
	}
	return out, nil
}

// FromRecord rebuilds a [Plan] from a store snapshot. A non-empty stored
// hash that does not match the canonical digest is [ErrHashMismatch]: the
// row set is not the approved bundle.
func FromRecord(rec store.Plan) (Plan, error) {
	id, err := ParseID(rec.ID.String())
	if err != nil {
		return Plan{}, fmt.Errorf("plan from record: %w", err)
	}
	sess, err := session.ParseID(rec.SessionID.String())
	if err != nil {
		return Plan{}, fmt.Errorf("plan from record: %w", err)
	}
	var parent ID
	if !rec.ParentPlanID.IsZero() {
		parent, err = ParseID(rec.ParentPlanID.String())
		if err != nil {
			return Plan{}, fmt.Errorf("plan from record: %w", err)
		}
	}
	rows := make([]Row, 0, len(rec.Effects))
	for i, e := range rec.Effects {
		op, ok := ParseOp(e.Type)
		if !ok {
			return Plan{}, fmt.Errorf("plan from record: effect %d: unknown type %q: %w", i, e.Type, ErrInvalid)
		}
		rows = append(rows, Row{
			Op:             op,
			Target:         e.Path,
			Payload:        e.Diff,
			Lease:          policy.LeaseID(e.LeaseID.String()),
			Precondition:   e.PreconditionHash,
			IdempotencyKey: e.IdempotencyKey,
			Postcondition:  e.PostconditionHash,
			CostEstimate:   e.CostEstimate,
		})
	}
	creds := make([]Credential, 0, len(rec.Credentials))
	for _, c := range rec.Credentials {
		creds = append(creds, Credential{Provider: c.Provider, Kind: c.Kind})
	}
	p := Plan{
		ID:            id,
		SessionID:     sess,
		ParentID:      parent,
		Status:        Status(rec.Status),
		Summary:       rec.Summary,
		Hash:          policy.PlanHash(rec.Hash),
		ExpiresAt:     rec.ExpiresAt.UTC(),
		CostCeiling:   rec.CostCeiling,
		Scope:         policy.ScopeID(rec.ScopeID.String()),
		Credentials:   creds,
		Rows:          rows,
		Findings:      findingsFromStore(rec.SecretScanFindings),
		ReviewComment: rec.ReviewComment,
		CreatedAt:     rec.CreatedAt.UTC(),
		UpdatedAt:     rec.UpdatedAt.UTC(),
	}
	if rec.CrossExam != nil {
		cx := *rec.CrossExam
		p.CrossExam = &CrossExam{
			Verdict:       cx.Verdict,
			ReviewerModel: cx.ReviewerModel,
			Reasoning:     cx.Reasoning,
			At:            cx.At.UTC(),
		}
	}
	if p.Hash == "" {
		return Plan{}, fmt.Errorf("plan from record: missing hash: %w", ErrInvalid)
	}
	if p.Hash != HashOf(p) {
		return Plan{}, fmt.Errorf("plan from record: %w", ErrHashMismatch)
	}
	return p, nil
}

func findingsToStore(in []Finding) []store.SecretScanFinding {
	if in == nil {
		return []store.SecretScanFinding{}
	}
	out := make([]store.SecretScanFinding, 0, len(in))
	for _, f := range in {
		out = append(out, store.SecretScanFinding{Path: f.Path, Rule: f.Rule, Line: f.Line})
	}
	return out
}

func findingsFromStore(in []store.SecretScanFinding) []Finding {
	if in == nil {
		return []Finding{}
	}
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		out = append(out, Finding{Path: f.Path, Rule: f.Rule, Line: f.Line})
	}
	return out
}
