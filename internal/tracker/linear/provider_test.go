package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
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
