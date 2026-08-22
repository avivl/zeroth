package server

import (
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
