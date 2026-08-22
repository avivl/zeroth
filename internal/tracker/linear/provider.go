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
)

// Provider is a Linear tracker.
type Provider struct {
	apiKey        string
	endpoint      string
	teamID        string
	projectID     string
	poll          time.Duration
	webhookSecret string
	client        *http.Client
	execer        failsafe.Executor[gqlResponse]

	mu          sync.Mutex
	agentUserID string
	known       map[string]tracker.Issue
	out         chan tracker.AssignmentEvent
	assigning   bool
}

// New returns a Linear tracker provider.
func New(cfg Config) (*Provider, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("tracker linear: empty api key: %w", tracker.ErrInvalid)
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
	return &Provider{
		apiKey:        key,
		endpoint:      endpoint,
		teamID:        strings.TrimSpace(cfg.TeamID),
		projectID:     strings.TrimSpace(cfg.ProjectID),
		poll:          poll,
		webhookSecret: strings.TrimSpace(cfg.WebhookSecret),
		client:        client,
		execer:        newExecutor(),
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
    state { id name type }
    project { name }
    team { id }
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
      state { id name type }
      project { name }
    }
  }
}`
)
