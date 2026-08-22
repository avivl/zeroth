package linear

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// FakeGraphQL is an in-memory Linear GraphQL API. It is a test double for
// the HTTP surface, not a second [tracker.Provider].
type FakeGraphQL struct {
	mu sync.Mutex

	APIKey            string
	AgentUserID       string
	TeamID            string
	lastAuthorization string

	issues       map[string]*fakeIssue
	comments     []FakeComment
	attachments  []FakeAttachment
	states       []workflowState
	next         int
	failAssigned string
}

// FakeComment is one posted or seeded comment.
type FakeComment struct {
	ID, IssueID, Body, URL, UserID, UserName, CreatedAt string
	Bot                                                 bool
}

// FakeAttachment is one linked URL.
type FakeAttachment struct {
	ID, IssueID, URL, Title string
}

type fakeIssue struct {
	ID, Identifier, Title, Description, URL, AssigneeID, DelegateID, Project, TeamID string
	State                                                                            workflowState
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
	ID, Identifier, Title, Description, URL, AssigneeID, DelegateID, Project, TeamID, StateID string
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
		DelegateID:  in.DelegateID,
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

// SetDelegate changes Linear's native agent delegate. Empty userID clears it.
func (f *FakeGraphQL) SetDelegate(key, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if iss := f.issues[key]; iss != nil {
		iss.DelegateID = userID
	}
}

// FailAssigned makes AssignedIssues return a GraphQL error. Empty clears it.
func (f *FakeGraphQL) FailAssigned(msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAssigned = msg
}

// Comments returns a copy of posted comments.
func (f *FakeGraphQL) Comments() []FakeComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeComment, len(f.comments))
	copy(out, f.comments)
	return out
}

// PutComment inserts a comment on an issue. Identifier or vendor id both
// resolve. Empty ID and URL are filled in.
func (f *FakeGraphQL) PutComment(in FakeComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[in.IssueID]
	if iss == nil {
		return
	}
	f.next++
	if in.ID == "" {
		in.ID = fmt.Sprintf("cmt_%d", f.next)
	}
	if in.URL == "" {
		in.URL = "https://linear.app/comment/" + in.ID
	}
	if in.CreatedAt == "" {
		in.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	in.IssueID = iss.ID
	f.comments = append(f.comments, in)
}

// Attachments returns a copy of linked artifacts.
func (f *FakeGraphQL) Attachments() []FakeAttachment {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeAttachment, len(f.attachments))
	copy(out, f.attachments)
	return out
}

// LastAuthorization is the raw Authorization header from the most recent
// GraphQL request. Tests use it to prove personal vs OAuth header format.
func (f *FakeGraphQL) LastAuthorization() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAuthorization
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
	rawAuth := r.Header.Get("Authorization")
	auth := strings.TrimSpace(rawAuth)
	auth = strings.TrimPrefix(auth, "Bearer ")
	f.mu.Lock()
	f.lastAuthorization = rawAuth
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
	case "IssueComments":
		return f.issueComments(strVar(req.Variables, "id"))
	case "AssignedIssues":
		f.mu.Lock()
		fail := f.failAssigned
		f.mu.Unlock()
		if fail != "" {
			return nil, fail
		}
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
	case strings.Contains(q, "comments(") || strings.Contains(q, "IssueComments"):
		return "IssueComments"
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

func (f *FakeGraphQL) issueComments(id string) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := f.issues[id]
	if iss == nil {
		return map[string]any{"issue": nil}, ""
	}
	nodes := make([]any, 0, len(f.comments))
	for _, c := range f.comments {
		if c.IssueID != iss.ID && c.IssueID != iss.Identifier {
			continue
		}
		node := map[string]any{
			"id":        c.ID,
			"body":      c.Body,
			"url":       c.URL,
			"createdAt": c.CreatedAt,
		}
		if c.Bot {
			node["user"] = nil
			name := c.UserName
			if name == "" {
				name = "bot"
			}
			node["botActor"] = map[string]any{"name": name}
		} else if c.UserName != "" || c.UserID != "" {
			node["user"] = map[string]any{"id": c.UserID, "name": c.UserName}
			node["botActor"] = nil
		} else {
			node["user"] = nil
			node["botActor"] = nil
		}
		nodes = append(nodes, node)
	}
	return map[string]any{
		"issue": map[string]any{
			"id": iss.ID,
			"comments": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes":    nodes,
			},
		},
	}, ""
}

func (f *FakeGraphQL) assigned(vars map[string]any) (any, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var filter map[string]any
	if vars != nil {
		filter, _ = vars["filter"].(map[string]any)
	}
	seen := map[*fakeIssue]struct{}{}
	nodes := []any{}
	for _, iss := range f.issues {
		if _, ok := seen[iss]; ok {
			continue
		}
		seen[iss] = struct{}{}
		if !issueMatchesFilter(iss, filter) {
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
	f.comments = append(f.comments, FakeComment{
		ID:        id,
		IssueID:   iss.ID,
		Body:      body,
		URL:       url,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
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
	var delegate any
	if iss.DelegateID != "" {
		delegate = map[string]any{"id": iss.DelegateID}
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
		"delegate":    delegate,
		"state":       map[string]any{"id": iss.State.ID, "name": iss.State.Name, "type": iss.State.Type},
		"project":     project,
		"team":        map[string]any{"id": iss.TeamID},
	}
}

func issueMatchesFilter(iss *fakeIssue, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	if or := asMapSlice(filter["or"]); or != nil {
		ok := false
		for _, sub := range or {
			if issueMatchesFilter(iss, sub) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if and := asMapSlice(filter["and"]); and != nil {
		for _, sub := range and {
			if !issueMatchesFilter(iss, sub) {
				return false
			}
		}
	}
	if id := nestedIDEq(filter["assignee"]); id != "" && iss.AssigneeID != id {
		return false
	}
	if id := nestedIDEq(filter["delegate"]); id != "" && iss.DelegateID != id {
		return false
	}
	return true
}

func nestedIDEq(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	idm, ok := m["id"].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := idm["eq"].(string)
	return s
}

func asMapSlice(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			out = append(out, m)
		}
		return out
	case []map[string]any:
		return t
	default:
		return nil
	}
}

func strVar(vars map[string]any, key string) string {
	if vars == nil {
		return ""
	}
	s, _ := vars[key].(string)
	return s
}
