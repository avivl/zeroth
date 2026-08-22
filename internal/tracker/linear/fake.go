package linear

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// FakeGraphQL is an in-memory Linear GraphQL API. It is a test double for
// the HTTP surface, not a second [tracker.Provider].
type FakeGraphQL struct {
	mu sync.Mutex

	APIKey      string
	AgentUserID string
	TeamID      string

	issues      map[string]*fakeIssue
	comments    []FakeComment
	attachments []FakeAttachment
	states      []workflowState
	next        int
}

// FakeComment is one posted comment.
type FakeComment struct {
	ID, IssueID, Body, URL string
}

// FakeAttachment is one linked URL.
type FakeAttachment struct {
	ID, IssueID, URL, Title string
}

type fakeIssue struct {
	ID, Identifier, Title, Description, URL, AssigneeID, Project, TeamID string
	State                                                                workflowState
}

// NewFake returns a GraphQL double with one team workflow and a seeded issue.
func NewFake() *FakeGraphQL {
	f := &FakeGraphQL{
		APIKey:      "test-linear-key",
		AgentUserID: "user_agent",
		TeamID:      "team_zeroth",
		issues:      make(map[string]*fakeIssue),
		states: []workflowState{
			{ID: "st_backlog", Name: "Backlog", Type: "backlog"},
			{ID: "st_unstarted", Name: "Todo", Type: "unstarted"},
			{ID: "st_started", Name: "In Progress", Type: "started"},
			{ID: "st_completed", Name: "Done", Type: "completed"},
			{ID: "st_canceled", Name: "Canceled", Type: "canceled"},
		},
	}
	f.PutIssue(FakeIssue{
		ID:          "iss_42_1",
		Identifier:  "42-1",
		Title:       "tracker.Provider and Linear",
		Description: "Assign to Zeroth",
		URL:         "https://linear.app/42-golems/issue/42-1",
		Project:     "Zeroth",
		TeamID:      f.TeamID,
		StateID:     "st_unstarted",
	})
	return f
}

// FakeIssue is the input to PutIssue.
type FakeIssue struct {
	ID, Identifier, Title, Description, URL, AssigneeID, Project, TeamID, StateID string
}

// PutIssue inserts or replaces an issue. Identifier and ID both index it.
func (f *FakeGraphQL) PutIssue(in FakeIssue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := workflowState{ID: "st_unstarted", Name: "Todo", Type: "unstarted"}
	for _, s := range f.states {
		if s.ID == in.StateID || strings.EqualFold(s.Type, in.StateID) || strings.EqualFold(s.Name, in.StateID) {
			st = s
			break
		}
	}
	iss := &fakeIssue{
		ID:          in.ID,
		Identifier:  in.Identifier,
		Title:       in.Title,
		Description: in.Description,
		URL:         in.URL,
		AssigneeID:  in.AssigneeID,
		Project:     in.Project,
		TeamID:      in.TeamID,
		State:       st,
	}
	if iss.TeamID == "" {
		iss.TeamID = f.TeamID
	}
	f.indexLocked(iss)
}

func (f *FakeGraphQL) indexLocked(iss *fakeIssue) {
	if f.issues == nil {
		f.issues = make(map[string]*fakeIssue)
	}
	if iss.ID != "" {
		f.issues[iss.ID] = iss
	}
	if iss.Identifier != "" {
		f.issues[iss.Identifier] = iss
	}
}

// SetAssignee changes the assignee. Empty userID un-assigns.
func (f *FakeGraphQL) SetAssignee(key, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if iss := f.issues[key]; iss != nil {
		iss.AssigneeID = userID
	}
}

// Comments returns a copy of posted comments.
func (f *FakeGraphQL) Comments() []FakeComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeComment, len(f.comments))
	copy(out, f.comments)
	return out
}

// Attachments returns a copy of linked artifacts.
func (f *FakeGraphQL) Attachments() []FakeAttachment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeAttachment, len(f.attachments))
	copy(out, f.attachments)
	return out
}

// IssueState returns the workflow type for key.
func (f *FakeGraphQL) IssueState(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if iss := f.issues[key]; iss != nil {
		return iss.State.Type
	}
	return ""
}

// ServeHTTP implements Linear's GraphQL endpoint.
func (f *FakeGraphQL) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	auth = strings.TrimPrefix(auth, "Bearer ")
	f.mu.Lock()
	want := f.APIKey
	f.mu.Unlock()
	if auth != want {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	var req gqlRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	data, gqlErr := f.dispatch(req)
	out := map[string]any{}
	if gqlErr != "" {
		out["errors"] = []map[string]string{{"message": gqlErr}}
	} else {
		out["data"] = data
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *FakeGraphQL) dispatch(req gqlRequest) (any, string) {
	op := req.OperationName
	if op == "" {
		op = guessOp(req.Query)
	}
	switch op {
	case "Viewer":
		return map[string]any{"viewer": map[string]any{"id": f.AgentUserID, "name": "zeroth-agent"}}, ""
	case "Issue":
		return f.issueData(strVar(req.Variables, "id"))
	case "IssueStates":
		return f.issueStates(strVar(req.Variables, "id"))
	case "AssignedIssues":
		return f.assigned(req.Variables)
	case "CommentCreate":
		return f.commentCreate(req.Variables)
	case "IssueUpdate":
		return f.issueUpdate(req.Variables)
	case "AttachmentCreate":
		return f.attachmentCreate(req.Variables)
	default:
		return nil, "unknown operation " + op
	}
}

func guessOp(q string) string {
	switch {
	case strings.Contains(q, "commentCreate"):
		return "CommentCreate"
	case strings.Contains(q, "attachmentCreate"):
		return "AttachmentCreate"
	case strings.Contains(q, "issueUpdate"):
		return "IssueUpdate"
	case strings.Contains(q, "AssignedIssues") || strings.Contains(q, "issues("):
		return "AssignedIssues"
	case strings.Contains(q, "viewer"):
		return "Viewer"
	case strings.Contains(q, "states"):
		return "IssueStates"
	default:
		return "Issue"
	}
}

func (f *FakeGraphQL) issueData(id string) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[id]
	if iss == nil {
		return map[string]any{"issue": nil}, ""
	}
	return map[string]any{"issue": f.issueJSON(iss)}, ""
}

func (f *FakeGraphQL) issueStates(id string) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[id]
	if iss == nil {
		return map[string]any{"issue": nil}, ""
	}
	nodes := make([]any, 0, len(f.states))
	for _, st := range f.states {
		nodes = append(nodes, map[string]any{"id": st.ID, "name": st.Name, "type": st.Type})
	}
	return map[string]any{
		"issue": map[string]any{
			"id": iss.ID,
			"team": map[string]any{
				"states": map[string]any{"nodes": nodes},
			},
		},
	}, ""
}

func (f *FakeGraphQL) assigned(vars map[string]any) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	assignee := ""
	if filter, ok := vars["filter"].(map[string]any); ok {
		if a, ok := filter["assignee"].(map[string]any); ok {
			if id, ok := a["id"].(map[string]any); ok {
				assignee, _ = id["eq"].(string)
			}
		}
	}
	seen := map[*fakeIssue]struct{}{}
	nodes := []any{}
	for _, iss := range f.issues {
		if _, ok := seen[iss]; ok {
			continue
		}
		seen[iss] = struct{}{}
		if assignee != "" && iss.AssigneeID != assignee {
			continue
		}
		nodes = append(nodes, f.issueJSON(iss))
	}
	return map[string]any{
		"issues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
			"nodes":    nodes,
		},
	}, ""
}

func (f *FakeGraphQL) commentCreate(vars map[string]any) (any, string) {
	input, _ := vars["input"].(map[string]any)
	issueID, _ := input["issueId"].(string)
	body, _ := input["body"].(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[issueID]
	if iss == nil {
		return nil, "Entity not found: Issue"
	}
	f.next++
	id := fmt.Sprintf("cmt_%d", f.next)
	url := "https://linear.app/comment/" + id
	f.comments = append(f.comments, FakeComment{ID: id, IssueID: iss.ID, Body: body, URL: url})
	return map[string]any{
		"commentCreate": map[string]any{
			"success": true,
			"comment": map[string]any{"id": id, "url": url},
		},
	}, ""
}

func (f *FakeGraphQL) issueUpdate(vars map[string]any) (any, string) {
	id, _ := vars["id"].(string)
	input, _ := vars["input"].(map[string]any)
	stateID, _ := input["stateId"].(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[id]
	if iss == nil {
		return nil, "Entity not found: Issue"
	}
	for _, st := range f.states {
		if st.ID == stateID {
			iss.State = st
			break
		}
	}
	return map[string]any{
		"issueUpdate": map[string]any{
			"success": true,
			"issue":   map[string]any{"id": iss.ID, "identifier": iss.Identifier},
		},
	}, ""
}

func (f *FakeGraphQL) attachmentCreate(vars map[string]any) (any, string) {
	input, _ := vars["input"].(map[string]any)
	issueID, _ := input["issueId"].(string)
	url, _ := input["url"].(string)
	title, _ := input["title"].(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[issueID]
	if iss == nil {
		return nil, "Entity not found: Issue"
	}
	f.next++
	id := fmt.Sprintf("att_%d", f.next)
	f.attachments = append(f.attachments, FakeAttachment{ID: id, IssueID: iss.ID, URL: url, Title: title})
	return map[string]any{
		"attachmentCreate": map[string]any{
			"success":    true,
			"attachment": map[string]any{"id": id, "url": url},
		},
	}, ""
}

func (f *FakeGraphQL) issueJSON(iss *fakeIssue) map[string]any {
	var assignee any
	if iss.AssigneeID != "" {
		assignee = map[string]any{"id": iss.AssigneeID}
	}
	var project any
	if iss.Project != "" {
		project = map[string]any{"name": iss.Project}
	}
	return map[string]any{
		"id":          iss.ID,
		"identifier":  iss.Identifier,
		"title":       iss.Title,
		"description": iss.Description,
		"url":         iss.URL,
		"assignee":    assignee,
		"state":       map[string]any{"id": iss.State.ID, "name": iss.State.Name, "type": iss.State.Type},
		"project":     project,
		"team":        map[string]any{"id": iss.TeamID},
	}
}

func strVar(vars map[string]any, key string) string {
	if vars == nil {
		return ""
	}
	s, _ := vars[key].(string)
	return s
}
