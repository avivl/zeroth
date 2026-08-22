package linear

import (
	"context"
	"fmt"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
)

// Assignments implements [tracker.Provider]. Polling is the stage-1 default.
func (p *Provider) Assignments(ctx context.Context) (<-chan tracker.AssignmentEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("tracker linear assignments: %w", err)
	}
	p.mu.Lock()
	if p.assigning {
		p.mu.Unlock()
		return nil, fmt.Errorf("tracker linear assignments: already running: %w", tracker.ErrInvalid)
	}
	p.assigning = true
	ch := make(chan tracker.AssignmentEvent, 32)
	p.out = ch
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.assigning = false
			p.out = nil
			p.mu.Unlock()
			close(ch)
		}()
		p.pollLoop(ctx)
	}()
	return ch, nil
}

func (p *Provider) pollLoop(ctx context.Context) {
	p.tick(ctx)
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Provider) ensureAgent(ctx context.Context) error {
	p.mu.Lock()
	id := p.agentUserID
	p.mu.Unlock()
	if id != "" {
		return nil
	}
	var data struct {
		Viewer *struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	if err := p.query(ctx, "Viewer", qViewer, nil, &data); err != nil {
		return fmt.Errorf("tracker linear viewer: %w", err)
	}
	if data.Viewer == nil || data.Viewer.ID == "" {
		return fmt.Errorf("tracker linear viewer: %w", tracker.ErrUnavailable)
	}
	p.mu.Lock()
	p.agentUserID = data.Viewer.ID
	p.mu.Unlock()
	return nil
}

func (p *Provider) agentID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agentUserID
}

func (p *Provider) tick(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	if err := p.ensureAgent(ctx); err != nil {
		if ctx.Err() != nil {
			return
		}
		p.log.Error("tracker linear viewer", zap.Error(err))
		return
	}
	current, err := p.listAssigned(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		p.log.Error("tracker linear poll", zap.Error(err))
		return
	}
	p.log.Debug("tracker linear poll", zap.Int("issues", len(current)))
	p.diffAndEmit(current)
}

func (p *Provider) listAssigned(ctx context.Context) (map[string]tracker.Issue, error) {
	filter := p.assignedFilter()
	out := make(map[string]tracker.Issue)
	var after *string
	for {
		vars := map[string]any{"filter": filter}
		if after != nil {
			vars["after"] = *after
		}
		var data struct {
			Issues *struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []issueNode `json:"nodes"`
			} `json:"issues"`
		}
		if err := p.query(ctx, "AssignedIssues", qAssignedIssues, vars, &data); err != nil {
			return nil, err
		}
		if data.Issues == nil {
			return out, nil
		}
		for _, n := range data.Issues.Nodes {
			iss := n.toIssue()
			out[iss.Key] = iss
		}
		if !data.Issues.PageInfo.HasNextPage || data.Issues.PageInfo.EndCursor == "" {
			return out, nil
		}
		c := data.Issues.PageInfo.EndCursor
		after = &c
	}
}

// assignedFilter is issues claimed by the agent as classic assignee or as
// Linear's native delegate (human may remain assignee). Team and project
// constraints still AND with that OR.
func (p *Provider) assignedFilter() map[string]any {
	agent := p.agentID()
	userEq := func(field string) map[string]any {
		return map[string]any{field: map[string]any{"id": map[string]any{"eq": agent}}}
	}
	filter := map[string]any{
		"or": []map[string]any{userEq("assignee"), userEq("delegate")},
	}
	if p.projectID != "" {
		filter["project"] = map[string]any{"id": map[string]any{"eq": p.projectID}}
	}
	if p.teamID != "" {
		filter["team"] = map[string]any{"id": map[string]any{"eq": p.teamID}}
	}
	return filter
}

func (p *Provider) diffAndEmit(current map[string]tracker.Issue) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	for key, iss := range current {
		if _, ok := p.known[key]; ok {
			p.known[key] = iss
			continue
		}
		p.known[key] = iss
		p.emitLocked(tracker.AssignmentEvent{
			Kind:  tracker.Assigned,
			Key:   key,
			Issue: iss,
			At:    now,
		})
	}
	for key, iss := range p.known {
		if _, ok := current[key]; ok {
			continue
		}
		delete(p.known, key)
		p.emitLocked(tracker.AssignmentEvent{
			Kind:  tracker.Unassigned,
			Key:   key,
			Issue: iss,
			At:    now,
		})
	}
}

func (p *Provider) emitLocked(ev tracker.AssignmentEvent) {
	if p.out == nil {
		return
	}
	select {
	case p.out <- ev:
	default:
		// Drop rather than block a poll or webhook on a slow handler.
		// The next poll snapshot still converges.
	}
}

// applyAgent is used by the webhook path so a POST and a poll share
// one known-set. wantAgent true means the issue is now on the agent
// (classic assignee or native delegate).
func (p *Provider) applyAgent(iss tracker.Issue, wantAgent bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	_, known := p.known[iss.Key]
	switch {
	case wantAgent && !known:
		p.known[iss.Key] = iss
		p.emitLocked(tracker.AssignmentEvent{Kind: tracker.Assigned, Key: iss.Key, Issue: iss, At: now})
	case !wantAgent && known:
		prev := p.known[iss.Key]
		delete(p.known, iss.Key)
		if iss.Title == "" {
			iss = prev
		}
		p.emitLocked(tracker.AssignmentEvent{Kind: tracker.Unassigned, Key: iss.Key, Issue: iss, At: now})
	case wantAgent && known:
		p.known[iss.Key] = iss
	}
}
