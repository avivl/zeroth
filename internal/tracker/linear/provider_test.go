package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewEmptyAPIKey(t *testing.T) {
	t.Parallel()
	_, err := New(Config{})
	if !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("New = %v, want ErrInvalid", err)
	}
}

func TestNewUnknownAuthStyle(t *testing.T) {
	t.Parallel()
	_, err := New(Config{APIKey: "k", AuthStyle: "basic"})
	if !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("New = %v, want ErrInvalid", err)
	}
}

func TestAuthorizationHeaderStyles(t *testing.T) {
	t.Parallel()
	key := "test-linear-token"
	cases := []struct {
		name  string
		style AuthStyle
		want  string
	}{
		{name: "personal_default", style: "", want: key},
		{name: "personal", style: AuthPersonal, want: key},
		{name: "oauth", style: AuthOAuth, want: "Bearer " + key},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := NewFake()
			fake.APIKey = key
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)
			p, err := New(Config{
				APIKey:      key,
				Endpoint:    srv.URL,
				AgentUserID: fake.AgentUserID,
				AuthStyle:   tc.style,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.GetIssue(t.Context(), "42-1"); err != nil {
				t.Fatal(err)
			}
			if got := fake.LastAuthorization(); got != tc.want {
				t.Fatalf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnassignClearsAgentWithoutEmitting(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	p, _ := testProvider(t, fake)
	if err := p.Unassign(t.Context(), "  "); !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("empty key = %v, want ErrInvalid", err)
	}
	if err := p.Unassign(t.Context(), "no-such-issue"); !errors.Is(err, tracker.ErrNotFound) {
		t.Fatalf("missing = %v, want ErrNotFound", err)
	}
	if err := p.Unassign(t.Context(), "42-1"); err != nil {
		t.Fatalf("already clear: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	fake.SetDelegate("42-1", fake.AgentUserID)
	waitKind(t, ch, tracker.Assigned)

	if err := p.Unassign(t.Context(), "42-1"); err != nil {
		t.Fatalf("Unassign: %v", err)
	}
	iss, err := p.GetIssue(t.Context(), "42-1")
	if err != nil {
		t.Fatal(err)
	}
	if iss.AssigneeID != "" || iss.DelegateID != "" {
		t.Fatalf("still claimed: assignee=%q delegate=%q", iss.AssigneeID, iss.DelegateID)
	}
	waitNoEvent(t, ch, 80*time.Millisecond)

	fake.SetAssignee("42-1", fake.AgentUserID)
	waitKind(t, ch, tracker.Assigned)
}

func TestDiffAndEmitDropsStaleSnapshot(t *testing.T) {
	t.Parallel()
	out := make(chan tracker.AssignmentEvent, 4)
	p := &Provider{
		known:     map[string]tracker.Issue{},
		out:       out,
		assignGen: 2,
	}
	p.diffAndEmit(map[string]tracker.Issue{"42-1": {Key: "42-1", Title: "stale"}}, 1)
	select {
	case ev := <-out:
		t.Fatalf("stale snapshot emitted %+v", ev)
	default:
	}
}

func TestNameAndCapabilities(t *testing.T) {
	t.Parallel()
	p, _ := testProvider(t, NewFake())
	if p.Name() != "linear" {
		t.Fatalf("Name = %q", p.Name())
	}
	caps := p.Capabilities()
	if !caps.Cycles || !caps.Milestones {
		t.Fatalf("Linear capabilities = %+v", caps)
	}
}

func TestUnauthorizedDoesNotLeakKey(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	key := "test-linear-secret-value-xyz"
	p, err := New(Config{
		APIKey:      key,
		Endpoint:    srv.URL,
		AgentUserID: fake.AgentUserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetIssue(t.Context(), "42-1")
	if err == nil {
		t.Fatal("expected unauthorized")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("api key leaked in error: %v", err)
	}
	if !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestSetStateByName(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	p, _ := testProvider(t, fake)
	err := p.SetState(t.Context(), "42-1", tracker.State{Kind: tracker.StateCompleted, Name: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.IssueState("42-1") != "completed" {
		t.Fatalf("state = %s", fake.IssueState("42-1"))
	}
}

func TestAssignmentsAlreadyRunning(t *testing.T) {
	t.Parallel()
	p, _ := testProvider(t, NewFake())
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Assignments(ctx)
	if !errors.Is(err, tracker.ErrInvalid) {
		t.Fatalf("second Assignments = %v", err)
	}
	// drain on test cleanup via t.Context cancel
	_ = ch
}

func TestWebhookAssignUnassign(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	secret := "whsec_test"
	p, srv := testProviderSecret(t, fake, secret)
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake.SetAssignee("42-1", fake.AgentUserID)
	body, _ := json.Marshal(map[string]any{
		"action": "update",
		"type":   "Issue",
		"data": map[string]any{
			"id":         "iss_42_1",
			"identifier": "42-1",
			"title":      "tracker.Provider",
			"assigneeId": fake.AgentUserID,
		},
	})
	postWebhook(t, p, secret, body)
	waitKind(t, ch, tracker.Assigned)

	fake.SetAssignee("42-1", "")
	body, _ = json.Marshal(map[string]any{
		"action": "update",
		"type":   "Issue",
		"data": map[string]any{
			"id":         "iss_42_1",
			"identifier": "42-1",
			"title":      "tracker.Provider",
			"assigneeId": "",
		},
	})
	postWebhook(t, p, secret, body)
	waitKind(t, ch, tracker.Unassigned)
	_ = srv
}

func TestPollDelegateAssignUnassign(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	human := "user_human"
	fake.SetAssignee("42-1", human)
	fake.SetDelegate("42-1", fake.AgentUserID)
	p, _ := testProvider(t, fake)
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitKind(t, ch, tracker.Assigned)

	fake.SetDelegate("42-1", "")
	waitKind(t, ch, tracker.Unassigned)
}

func TestWebhookDelegateAssignUnassign(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	secret := "whsec_test"
	p, _ := testProviderSecret(t, fake, secret)
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	human := "user_human"
	fake.SetAssignee("42-1", human)
	fake.SetDelegate("42-1", fake.AgentUserID)
	body, _ := json.Marshal(map[string]any{
		"action": "update",
		"type":   "Issue",
		"data": map[string]any{
			"id":         "iss_42_1",
			"identifier": "42-1",
			"title":      "tracker.Provider",
			"assigneeId": human,
			"delegateId": fake.AgentUserID,
		},
	})
	postWebhook(t, p, secret, body)
	waitKind(t, ch, tracker.Assigned)

	fake.SetDelegate("42-1", "")
	body, _ = json.Marshal(map[string]any{
		"action": "update",
		"type":   "Issue",
		"data": map[string]any{
			"id":         "iss_42_1",
			"identifier": "42-1",
			"title":      "tracker.Provider",
			"assigneeId": human,
			"delegateId": "",
		},
	})
	postWebhook(t, p, secret, body)
	waitKind(t, ch, tracker.Unassigned)
}

func TestWebhookAgentSessionCreatedDelegated(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	secret := "whsec_test"
	p, _ := testProviderSecret(t, fake, secret)
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fake.SetAssignee("42-1", "user_human")
	fake.SetDelegate("42-1", fake.AgentUserID)
	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"type":   "AgentSessionEvent",
		"agentSession": map[string]any{
			"id": "ags_1",
			"issue": map[string]any{
				"id":         "iss_42_1",
				"identifier": "42-1",
				"title":      "tracker.Provider",
			},
		},
	})
	postWebhook(t, p, secret, body)
	waitKind(t, ch, tracker.Assigned)
}

func TestWebhookAgentSessionCreatedWithoutClaimIsIgnored(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	secret := "whsec_test"
	p, _ := testProviderSecret(t, fake, secret)
	ctx := t.Context()
	ch, err := p.Assignments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"type":   "AgentSessionEvent",
		"agentSession": map[string]any{
			"id": "ags_mention",
			"issue": map[string]any{
				"id":         "iss_42_1",
				"identifier": "42-1",
				"title":      "tracker.Provider",
			},
		},
	})
	postWebhook(t, p, secret, body)
	waitNoEvent(t, ch, 200*time.Millisecond)
}

func TestAgentClaimed(t *testing.T) {
	t.Parallel()
	agent := "user_agent"
	cases := []struct {
		name                       string
		action, assignee, delegate string
		want                       bool
	}{
		{name: "classic_assignee", action: "update", assignee: agent, want: true},
		{name: "delegate_human_assignee", action: "update", assignee: "user_human", delegate: agent, want: true},
		{name: "neither", action: "update", assignee: "user_human", want: false},
		{name: "remove_even_if_assignee", action: "remove", assignee: agent, want: false},
		{name: "empty_agent", action: "update", assignee: agent, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := agent
			if tc.name == "empty_agent" {
				id = ""
			}
			if got := agentClaimed(tc.action, tc.assignee, tc.delegate, id); got != tc.want {
				t.Fatalf("agentClaimed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	p, _ := testProviderSecret(t, fake, "whsec_test")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tracker", bytes.NewReader([]byte(`{"type":"Issue"}`)))
	req.Header.Set("Linear-Signature", "deadbeef")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestWebhookWithoutSecretIsNotFound(t *testing.T) {
	t.Parallel()
	p, _ := testProvider(t, NewFake())
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tracker", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestTickLogsPollErrorAtErrorLevel(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	fake.FailAssigned("Unknown argument \"delegate\" on field \"IssueFilter\".")
	p, logs := testProviderLog(t, fake, zap.InfoLevel)
	p.tick(t.Context())
	ents := logs.FilterMessage("tracker linear poll").FilterLevelExact(zap.ErrorLevel).All()
	if len(ents) != 1 {
		t.Fatalf("error logs = %d (%v), want 1", len(ents), logs.All())
	}
	if !strings.Contains(ents[0].Message, "tracker linear poll") {
		t.Fatalf("msg = %q", ents[0].Message)
	}
	errField := errFrom(ents[0])
	if !strings.Contains(errField, "delegate") {
		t.Fatalf("error field = %q, want the GraphQL message", errField)
	}
	if logs.FilterLevelExact(zap.DebugLevel).Len() != 0 {
		t.Fatalf("unexpected debug logs: %v", logs.All())
	}
}

func TestTickLogsUnauthorizedAtErrorLevel(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	core, logs := observer.New(zap.InfoLevel)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p, err := New(Config{
		APIKey:       "not-the-key",
		Endpoint:     srv.URL,
		AgentUserID:  fake.AgentUserID,
		PollInterval: time.Hour,
		Log:          zap.New(core),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.tick(t.Context())
	ents := logs.FilterMessage("tracker linear poll").FilterLevelExact(zap.ErrorLevel).All()
	if len(ents) != 1 {
		t.Fatalf("error logs = %d (%v), want 1", len(ents), logs.All())
	}
	if !strings.Contains(errFrom(ents[0]), "unauthorized") {
		t.Fatalf("error field = %q, want unauthorized", errFrom(ents[0]))
	}
}

func TestTickLogsViewerErrorAtErrorLevel(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	core, logs := observer.New(zap.InfoLevel)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p, err := New(Config{
		APIKey:       "not-the-key",
		Endpoint:     srv.URL,
		PollInterval: time.Hour,
		Log:          zap.New(core),
	})
	if err != nil {
		t.Fatal(err)
	}
	p.tick(t.Context())
	ents := logs.FilterMessage("tracker linear viewer").FilterLevelExact(zap.ErrorLevel).All()
	if len(ents) != 1 {
		t.Fatalf("viewer error logs = %d (%v), want 1", len(ents), logs.All())
	}
}

func TestTickLogsMatchedCountAtDebug(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	fake.SetAssignee("42-1", fake.AgentUserID)
	p, logs := testProviderLog(t, fake, zap.DebugLevel)
	p.tick(t.Context())
	ents := logs.FilterMessage("tracker linear poll").FilterLevelExact(zap.DebugLevel).All()
	if len(ents) != 1 {
		t.Fatalf("debug logs = %d (%v), want 1", len(ents), logs.All())
	}
	got := ents[0].ContextMap()["issues"]
	if got != int64(1) && got != 1 {
		t.Fatalf("issues = %v (%T), want 1", got, got)
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
		t.Fatalf("unexpected error logs: %v", logs.All())
	}
}

func TestTickSuccessIsSilentAtInfo(t *testing.T) {
	t.Parallel()
	fake := NewFake()
	p, logs := testProviderLog(t, fake, zap.InfoLevel)
	p.tick(t.Context())
	if logs.Len() != 0 {
		t.Fatalf("info+ logs on a healthy empty poll: %v", logs.All())
	}
}

func TestAssignedFilterOrAssigneeAndDelegate(t *testing.T) {
	t.Parallel()
	p, _ := testProvider(t, NewFake())
	filter := p.assignedFilter()
	or, ok := filter["or"].([]map[string]any)
	if !ok || len(or) != 2 {
		t.Fatalf("or = %#v, want two user filters", filter["or"])
	}
	if nestedIDEq(or[0]["assignee"]) != p.agentID() {
		t.Fatalf("assignee filter = %#v", or[0])
	}
	if nestedIDEq(or[1]["delegate"]) != p.agentID() {
		t.Fatalf("delegate filter = %#v", or[1])
	}
}

func errFrom(ent observer.LoggedEntry) string {
	for _, f := range ent.Context {
		if f.Key == "error" {
			if f.Type == zapcore.ErrorType {
				if err, ok := f.Interface.(error); ok {
					return err.Error()
				}
			}
			return f.String
		}
	}
	return ""
}

func testProviderLog(t *testing.T, fake *FakeGraphQL, level zapcore.Level) (*Provider, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(level)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p, err := New(Config{
		APIKey:       fake.APIKey,
		Endpoint:     srv.URL,
		AgentUserID:  fake.AgentUserID,
		PollInterval: time.Hour,
		Log:          zap.New(core),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, logs
}

func testProvider(t *testing.T, fake *FakeGraphQL) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p, err := New(Config{
		APIKey:       fake.APIKey,
		Endpoint:     srv.URL,
		AgentUserID:  fake.AgentUserID,
		PollInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func testProviderSecret(t *testing.T, fake *FakeGraphQL, secret string) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	p, err := New(Config{
		APIKey:        fake.APIKey,
		Endpoint:      srv.URL,
		AgentUserID:   fake.AgentUserID,
		PollInterval:  time.Hour,
		WebhookSecret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, srv
}

func postWebhook(t *testing.T, p *Provider, secret string, body []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tracker", bytes.NewReader(body))
	req.Header.Set("Linear-Signature", SignWebhook(secret, body))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook status %d body %s", rec.Code, rec.Body.String())
	}
}

func waitKind(t *testing.T, ch <-chan tracker.AssignmentEvent, kind tracker.AssignmentKind) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("closed")
			}
			if ev.Kind == kind {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s", kind)
		}
	}
}

func waitNoEvent(t *testing.T, ch <-chan tracker.AssignmentEvent, d time.Duration) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("closed")
		}
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(d):
	}
}
