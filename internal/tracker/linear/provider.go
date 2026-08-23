package linear

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
	"github.com/failsafe-go/failsafe-go"
	"go.uber.org/zap"
)

// Provider is a Linear tracker.
type Provider struct {
	apiKey        string
	authStyle     AuthStyle
	endpoint      string
	teamID        string
	projectID     string
	poll          time.Duration
	webhookSecret string
	client        *http.Client
	execer        failsafe.Executor[gqlResponse]
	log           *zap.Logger

	mu          sync.Mutex
	agentUserID string
	known       map[string]tracker.Issue
	out         chan tracker.AssignmentEvent
	assigning   bool
	// assignGen bumps on Unassign so a poll that listed issues before
	// the agent was cleared cannot emit Assigned and start a new run.
	assignGen uint64
}

// New returns a Linear tracker provider.
func New(cfg Config) (*Provider, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("tracker linear: empty api key: %w", tracker.ErrInvalid)
	}
	style, err := parseAuthStyle(cfg.AuthStyle)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = defaultPoll
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Provider{
		apiKey:        key,
		authStyle:     style,
		endpoint:      endpoint,
		teamID:        strings.TrimSpace(cfg.TeamID),
		projectID:     strings.TrimSpace(cfg.ProjectID),
		poll:          poll,
		webhookSecret: strings.TrimSpace(cfg.WebhookSecret),
		client:        client,
		execer:        newExecutor(),
		log:           log,
		agentUserID:   strings.TrimSpace(cfg.AgentUserID),
		known:         make(map[string]tracker.Issue),
	}, nil
}

// Name implements [tracker.Provider].
func (*Provider) Name() string { return driverName }

// Capabilities implements [tracker.Provider]. Linear has both optional features.
func (*Provider) Capabilities() tracker.Capabilities {
	return tracker.Capabilities{Cycles: true, Milestones: true}
}

var _ tracker.Provider = (*Provider)(nil)

// GetIssue implements [tracker.Provider].
func (p *Provider) GetIssue(ctx context.Context, key string) (tracker.Issue, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return tracker.Issue{}, fmt.Errorf("tracker linear get issue: %w", tracker.ErrInvalid)
	}
	var data struct {
		Issue *issueNode `json:"issue"`
	}
	if err := p.query(ctx, "Issue", qIssue, map[string]any{"id": key}, &data); err != nil {
		return tracker.Issue{}, fmt.Errorf("tracker linear get issue: %w", err)
	}
	if data.Issue == nil {
		return tracker.Issue{}, fmt.Errorf("tracker linear get issue %s: %w", key, tracker.ErrNotFound)
	}
	return data.Issue.toIssue(), nil
}

// ListComments implements [tracker.Provider].
func (p *Provider) ListComments(ctx context.Context, key string) ([]tracker.Comment, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("tracker linear list comments: %w", tracker.ErrInvalid)
	}
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	var out []tracker.Comment
	var after *string
	for {
		vars := map[string]any{"id": issue.ID}
		if after != nil {
			vars["after"] = *after
		}
		var data struct {
			Issue *struct {
				Comments *struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []commentNode `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		}
		if err := p.query(ctx, "IssueComments", qIssueComments, vars, &data); err != nil {
			return nil, fmt.Errorf("tracker linear list comments: %w", err)
		}
		if data.Issue == nil {
			return nil, fmt.Errorf("tracker linear list comments %s: %w", key, tracker.ErrNotFound)
		}
		if data.Issue.Comments == nil {
			break
		}
		for _, n := range data.Issue.Comments.Nodes {
			out = append(out, n.toComment())
		}
		if !data.Issue.Comments.PageInfo.HasNextPage || data.Issue.Comments.PageInfo.EndCursor == "" {
			break
		}
		c := data.Issue.Comments.PageInfo.EndCursor
		after = &c
	}
	return out, nil
}

// Comment implements [tracker.Provider].
func (p *Provider) Comment(ctx context.Context, key, body string) (tracker.CommentRef, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.TrimSpace(body) == "" {
		return tracker.CommentRef{}, fmt.Errorf("tracker linear comment: %w", tracker.ErrInvalid)
	}
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return tracker.CommentRef{}, err
	}
	var data struct {
		CommentCreate struct {
			Success bool `json:"success"`
			Comment *struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	err = p.query(ctx, "CommentCreate", qCommentCreate, map[string]any{
		"input": map[string]any{"issueId": issue.ID, "body": body},
	}, &data)
	if err != nil {
		return tracker.CommentRef{}, fmt.Errorf("tracker linear comment: %w", err)
	}
	if !data.CommentCreate.Success || data.CommentCreate.Comment == nil {
		return tracker.CommentRef{}, fmt.Errorf("tracker linear comment: %w", tracker.ErrUnavailable)
	}
	return tracker.CommentRef{
		ID:  data.CommentCreate.Comment.ID,
		URL: data.CommentCreate.Comment.URL,
	}, nil
}

// SetState implements [tracker.Provider].
func (p *Provider) SetState(ctx context.Context, key string, state tracker.State) error {
	key = strings.TrimSpace(key)
	if key == "" || state.Kind == "" {
		return fmt.Errorf("tracker linear set state: %w", tracker.ErrInvalid)
	}
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	stateID, err := p.lookupStateID(ctx, issue.ID, state)
	if err != nil {
		return err
	}
	var data struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	err = p.query(ctx, "IssueUpdate", qIssueUpdate, map[string]any{
		"id":    issue.ID,
		"input": map[string]any{"stateId": stateID},
	}, &data)
	if err != nil {
		return fmt.Errorf("tracker linear set state: %w", err)
	}
	if !data.IssueUpdate.Success {
		return fmt.Errorf("tracker linear set state: %w", tracker.ErrUnavailable)
	}
	return nil
}

// Unassign implements [tracker.Provider].
func (p *Provider) Unassign(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("tracker linear unassign: %w", tracker.ErrInvalid)
	}
	if err := p.ensureAgent(ctx); err != nil {
		return fmt.Errorf("tracker linear unassign: %w", err)
	}
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	agent := p.agentID()
	input := map[string]any{}
	if agent != "" && issue.AssigneeID == agent {
		input["assigneeId"] = nil
	}
	if agent != "" && issue.DelegateID == agent {
		input["delegateId"] = nil
	}
	if len(input) > 0 {
		var data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		}
		err = p.query(ctx, "IssueUpdate", qIssueUpdate, map[string]any{
			"id":    issue.ID,
			"input": input,
		}, &data)
		if err != nil {
			return fmt.Errorf("tracker linear unassign: %w", err)
		}
		if !data.IssueUpdate.Success {
			return fmt.Errorf("tracker linear unassign: %w", tracker.ErrUnavailable)
		}
	}
	p.mu.Lock()
	p.assignGen++
	delete(p.known, issue.Key)
	if issue.ID != "" {
		delete(p.known, issue.ID)
	}
	p.mu.Unlock()
	return nil
}

func (p *Provider) lookupStateID(ctx context.Context, issueID string, want tracker.State) (string, error) {
	var data struct {
		Issue *struct {
			Team *struct {
				States struct {
					Nodes []workflowState `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	if err := p.query(ctx, "IssueStates", qIssueStates, map[string]any{"id": issueID}, &data); err != nil {
		return "", fmt.Errorf("tracker linear set state: %w", err)
	}
	if data.Issue == nil || data.Issue.Team == nil {
		return "", fmt.Errorf("tracker linear set state: %w", tracker.ErrNotFound)
	}
	name := strings.TrimSpace(want.Name)
	kind := strings.ToLower(string(want.Kind))
	var byKind string
	for _, st := range data.Issue.Team.States.Nodes {
		if name != "" && strings.EqualFold(st.Name, name) {
			return st.ID, nil
		}
		if byKind == "" && strings.EqualFold(st.Type, kind) {
			byKind = st.ID
		}
	}
	if byKind != "" {
		return byKind, nil
	}
	return "", fmt.Errorf("tracker linear set state: no %s state: %w", want.Kind, tracker.ErrInvalid)
}

// LinkArtifact implements [tracker.Provider].
func (p *Provider) LinkArtifact(ctx context.Context, key string, a tracker.Artifact) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.TrimSpace(a.URL) == "" || a.Kind == "" {
		return fmt.Errorf("tracker linear link artifact: %w", tracker.ErrInvalid)
	}
	issue, err := p.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = string(a.Kind)
	}
	var data struct {
		AttachmentCreate struct {
			Success bool `json:"success"`
		} `json:"attachmentCreate"`
	}
	err = p.query(ctx, "AttachmentCreate", qAttachmentCreate, map[string]any{
		"input": map[string]any{
			"issueId": issue.ID,
			"url":     a.URL,
			"title":   title,
		},
	}, &data)
	if err != nil {
		return fmt.Errorf("tracker linear link artifact: %w", err)
	}
	if !data.AttachmentCreate.Success {
		return fmt.Errorf("tracker linear link artifact: %w", tracker.ErrUnavailable)
	}
	return nil
}

type issueNode struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Assignee    *struct {
		ID string `json:"id"`
	} `json:"assignee"`
	Delegate *struct {
		ID string `json:"id"`
	} `json:"delegate"`
	State *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Project *struct {
		Name string `json:"name"`
	} `json:"project"`
	Team *struct {
		ID string `json:"id"`
	} `json:"team"`
}

type commentNode struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	User      *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
	BotActor *struct {
		Name string `json:"name"`
	} `json:"botActor"`
}

func (n commentNode) toComment() tracker.Comment {
	c := tracker.Comment{
		ID:   n.ID,
		Body: n.Body,
		URL:  n.URL,
		Bot:  n.User == nil && n.BotActor != nil,
	}
	if n.User != nil {
		c.Author = n.User.Name
	} else if n.BotActor != nil {
		c.Author = n.BotActor.Name
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(n.CreatedAt)); err == nil {
		c.At = t.UTC()
	}
	return c
}

func (n issueNode) toIssue() tracker.Issue {
	iss := tracker.Issue{
		Key:         n.Identifier,
		ID:          n.ID,
		Title:       n.Title,
		Description: n.Description,
		URL:         n.URL,
	}
	if iss.Key == "" {
		iss.Key = n.ID
	}
	if n.Assignee != nil {
		iss.AssigneeID = n.Assignee.ID
	}
	if n.Delegate != nil {
		iss.DelegateID = n.Delegate.ID
	}
	if n.State != nil {
		iss.State = tracker.State{
			Kind: tracker.StateKind(strings.ToLower(n.State.Type)),
			Name: n.State.Name,
		}
	}
	if n.Project != nil {
		iss.Project = n.Project.Name
	}
	return iss
}

type workflowState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

const (
	qIssue = `query Issue($id: String!) {
  issue(id: $id) {
    id identifier title description url
    assignee { id }
    delegate { id }
    state { id name type }
    project { name }
    team { id }
  }
}`
	qIssueComments = `query IssueComments($id: String!, $after: String) {
  issue(id: $id) {
    id
    comments(first: 50, after: $after, orderBy: createdAt) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id body url createdAt
        user { id name }
        botActor { name }
      }
    }
  }
}`
	qCommentCreate = `mutation CommentCreate($input: CommentCreateInput!) {
  commentCreate(input: $input) { success comment { id url } }
}`
	qIssueUpdate = `mutation IssueUpdate($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) { success issue { id identifier } }
}`
	qIssueStates = `query IssueStates($id: String!) {
  issue(id: $id) {
    id
    team { states { nodes { id name type } } }
  }
}`
	qAttachmentCreate = `mutation AttachmentCreate($input: AttachmentCreateInput!) {
  attachmentCreate(input: $input) { success attachment { id url } }
}`
	qViewer         = `query Viewer { viewer { id name } }`
	qAssignedIssues = `query AssignedIssues($filter: IssueFilter, $after: String) {
  issues(filter: $filter, first: 50, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes {
      id identifier title description url
      assignee { id }
      delegate { id }
      state { id name type }
      project { name }
    }
  }
}`
)
