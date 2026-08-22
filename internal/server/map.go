package server

import (
	"encoding/json"
	"strconv"

	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// apiStatus projects session lifecycle plus attachment onto the OpenAPI
// RunStatus. Attachment is orthogonal in the kernel; the wire format has
// a single status field, so backgrounded wins only when the run is still
// working. A waiting-approval run stays waiting_approval so the operator
// inbox is not hidden by demotion.
func apiStatus(st session.State) gen.RunStatus {
	switch st.Status {
	case session.StatusPending:
		return gen.RunStatusPending
	case session.StatusDone:
		return gen.RunStatusCompleted
	case session.StatusFailed:
		return gen.RunStatusFailed
	case session.StatusAwaitingApproval:
		return gen.RunStatusWaitingApproval
	}
	if st.Attachment == session.AttachmentBackground {
		return gen.RunStatusBackgrounded
	}
	return gen.RunStatusRunning
}

func runFrom(sess store.Session, st session.State) gen.Run {
	out := gen.Run{
		Id:        gen.RunID(sess.ID.String()),
		AgentId:   gen.AgentID(sess.AgentID.String()),
		Status:    apiStatus(st),
		CreatedAt: sess.CreatedAt.UTC(),
		UpdatedAt: sess.UpdatedAt.UTC(),
	}
	if sess.Prompt != "" {
		p := sess.Prompt
		out.Prompt = &p
	}
	if sess.TrackerRef != "" {
		tr := sess.TrackerRef
		out.TrackerRef = &tr
	}
	if sess.AutonomyTier != "" {
		tier := sess.AutonomyTier
		out.AutonomyTier = &tier
	}
	if !sess.PlanID.IsZero() {
		pid := gen.PlanID(sess.PlanID.String())
		out.PlanId = &pid
	}
	if sess.Workspace.Repo != "" {
		ws := gen.WorkspaceSource{Repo: sess.Workspace.Repo}
		if sess.Workspace.Ref != "" {
			ref := sess.Workspace.Ref
			ws.Ref = &ref
		}
		out.WorkspaceSource = &ws
	}
	if !sess.FinishedAt.IsZero() {
		t := sess.FinishedAt.UTC()
		out.FinishedAt = &t
	} else if st.Status.Terminal() && !sess.UpdatedAt.IsZero() {
		t := sess.UpdatedAt.UTC()
		out.FinishedAt = &t
	}
	return out
}

func apiEvent(ev session.Event) gen.RunEvent {
	msg := eventMessage(ev)
	out := gen.RunEvent{
		Id:        formatEventID(ev.Seq),
		RunId:     gen.RunID(ev.SessionID.String()),
		Type:      apiEventType(ev.Type),
		CreatedAt: ev.At.UTC(),
	}
	if msg != "" {
		out.Message = &msg
	}
	if ev.Type == session.EventCrossExam || ev.Type == session.EventPlanProposed || ev.Type == session.EventApprovalRequested {
		var payload crossExamPayload
		if err := json.Unmarshal([]byte(ev.Payload), &payload); err == nil && payload.PlanID != "" {
			pid := gen.PlanID(payload.PlanID)
			out.PlanId = &pid
		} else if ev.Payload != "" && ev.Type != session.EventCrossExam {
			pid := gen.PlanID(ev.Payload)
			out.PlanId = &pid
		}
	}
	return out
}

func apiEventType(t session.EventType) string {
	switch t {
	case session.EventToken, session.EventSteered:
		return "log"
	case session.EventToolCall:
		return "tool_call"
	case session.EventPlanProposed:
		return "plan_drafted"
	case session.EventCrossExam:
		return "cross_exam_verdict"
	case session.EventCheckpointTaken:
		return "checkpoint_created"
	case session.EventError:
		return "error"
	default:
		return "status_changed"
	}
}

func eventMessage(ev session.Event) string {
	switch ev.Type {
	case session.EventTerminal:
		if ev.Payload == session.PayloadDone {
			return "completed"
		}
		if ev.Payload == session.PayloadFailed {
			return "failed"
		}
		return ev.Payload
	case session.EventCreated:
		return "created"
	case session.EventStarted:
		return "started"
	case session.EventBackgrounded:
		return "backgrounded"
	case session.EventAttached:
		return "attached"
	case session.EventCrossExam:
		var payload crossExamPayload
		if err := json.Unmarshal([]byte(ev.Payload), &payload); err == nil && payload.Verdict != "" {
			if payload.Notes != "" {
				return payload.Verdict + ": " + payload.Notes
			}
			return payload.Verdict
		}
		return ev.Payload
	default:
		return ev.Payload
	}
}

func formatEventID(seq int64) string {
	return strconv.FormatInt(seq, 10)
}

func agentFrom(a store.Agent) gen.Agent {
	out := gen.Agent{
		Id:        gen.AgentID(a.ID.String()),
		Name:      a.Name,
		Harness:   a.Harness,
		Status:    gen.AgentStatus(a.Status),
		CreatedAt: a.CreatedAt.UTC(),
		UpdatedAt: a.UpdatedAt.UTC(),
	}
	if a.Model != "" {
		m := a.Model
		out.Model = &m
	}
	if a.AutonomyTier != "" {
		t := a.AutonomyTier
		out.AutonomyTier = &t
	}
	if len(a.Tools) > 0 {
		tools := append([]string(nil), a.Tools...)
		out.Tools = &tools
	}
	if a.ReviewerModel != "" || a.ReviewerModel2 != "" || a.ReviewerDual || a.BlockOnFail {
		rc := gen.ReviewerConfig{}
		if a.ReviewerModel != "" {
			m := a.ReviewerModel
			rc.Model = &m
		}
		if a.ReviewerModel2 != "" {
			m := a.ReviewerModel2
			rc.SecondModel = &m
		}
		if a.ReviewerDual {
			d := true
			rc.Dual = &d
		}
		if a.BlockOnFail {
			b := true
			rc.BlockOnFail = &b
		}
		out.Reviewer = &rc
	}
	return out
}

func auditFrom(r store.AuditRecord) gen.AuditRecord {
	out := gen.AuditRecord{
		Id:           gen.AuditID(r.ID.String()),
		Action:       r.Action,
		ResourceType: gen.AuditResourceType(r.ResourceType),
		ResourceId:   r.ResourceID,
		Signature:    r.Signature,
		CreatedAt:    r.CreatedAt.UTC(),
	}
	if r.Actor != "" {
		a := r.Actor
		out.Actor = &a
	}
	if r.Target != "" {
		t := r.Target
		out.Target = &t
	}
	if r.PlanHash != "" {
		h := r.PlanHash
		out.PlanHash = &h
	}
	if r.Precondition != "" {
		p := r.Precondition
		out.Precondition = &p
	}
	if r.Postcondition != "" {
		p := r.Postcondition
		out.Postcondition = &p
	}
	if !r.LeaseID.IsZero() {
		id := gen.LeaseID(r.LeaseID.String())
		out.LeaseId = &id
	}
	if r.Approver != "" {
		a := r.Approver
		out.Approver = &a
	}
	if !r.AgentID.IsZero() {
		id := gen.AgentID(r.AgentID.String())
		out.AgentId = &id
	}
	if !r.SessionID.IsZero() {
		id := gen.RunID(r.SessionID.String())
		out.RunId = &id
	}
	if r.AgentPubKey != "" {
		k := r.AgentPubKey
		out.AgentPubkey = &k
	}
	if r.PrevHash != "" {
		h := r.PrevHash
		out.PrevHash = &h
	}
	if r.Hash != "" {
		h := r.Hash
		out.Hash = &h
	}
	return out
}

func planFrom(p store.Plan) gen.Plan {
	out := gen.Plan{
		Id:        gen.PlanID(p.ID.String()),
		RunId:     gen.RunID(p.SessionID.String()),
		Status:    gen.PlanStatus(p.Status),
		Summary:   p.Summary,
		Effects:   planEffectsFrom(p.Effects),
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	}
	if !p.ParentPlanID.IsZero() {
		id := gen.PlanID(p.ParentPlanID.String())
		out.ParentPlanId = &id
	}
	if p.Hash != "" {
		h := p.Hash
		out.Hash = &h
	}
	if !p.ExpiresAt.IsZero() {
		t := p.ExpiresAt.UTC()
		out.ExpiresAt = &t
	}
	if p.CostCeiling != 0 {
		c := p.CostCeiling
		out.CostCeiling = &c
	}
	if !p.ScopeID.IsZero() {
		id := gen.ScopeID(p.ScopeID.String())
		out.ScopeId = &id
	}
	if len(p.Credentials) > 0 {
		creds := make([]gen.CredentialConstraint, 0, len(p.Credentials))
		for _, c := range p.Credentials {
			creds = append(creds, gen.CredentialConstraint{Provider: c.Provider, Kind: c.Kind})
		}
		out.Credentials = &creds
	}
	if p.CrossExam != nil {
		out.CrossExam = &gen.CrossExam{
			Verdict:       p.CrossExam.Verdict,
			ReviewerModel: p.CrossExam.ReviewerModel,
			Reasoning:     p.CrossExam.Reasoning,
			At:            p.CrossExam.At.UTC(),
		}
	}
	if p.SecretScanFindings != nil {
		findings := make([]gen.SecretScanFinding, 0, len(p.SecretScanFindings))
		for _, f := range p.SecretScanFindings {
			item := gen.SecretScanFinding{Path: f.Path, Rule: f.Rule}
			if f.Line > 0 {
				line := f.Line
				item.Line = &line
			}
			findings = append(findings, item)
		}
		out.SecretScanFindings = &findings
	}
	if p.ReviewComment != "" {
		c := p.ReviewComment
		out.ReviewComment = &c
	}
	return out
}

func planEffectsFrom(in []store.PlanEffect) []gen.PlanEffect {
	out := make([]gen.PlanEffect, 0, len(in))
	for _, e := range in {
		item := gen.PlanEffect{Type: gen.PlanEffectType(e.Type)}
		if e.Path != "" {
			p := e.Path
			item.Path = &p
		}
		if e.Diff != "" {
			d := e.Diff
			item.Diff = &d
		}
		if e.PreconditionHash != "" {
			h := e.PreconditionHash
			item.PreconditionHash = &h
		}
		if e.PostconditionHash != "" {
			h := e.PostconditionHash
			item.PostconditionHash = &h
		}
		if e.IdempotencyKey != "" {
			k := e.IdempotencyKey
			item.IdempotencyKey = &k
		}
		if !e.LeaseID.IsZero() {
			id := gen.LeaseID(e.LeaseID.String())
			item.LeaseId = &id
		}
		if e.CostEstimate != "" {
			c := e.CostEstimate
			item.CostEstimate = &c
		}
		out = append(out, item)
	}
	return out
}
