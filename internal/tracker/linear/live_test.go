package linear

import (
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Live Linear GraphQL is opt-in. CI uses FakeGraphQL. Schema introspection
// does not need a workspace key; listAssigned does.
func TestLiveIssueFilterSchema(t *testing.T) {
	if os.Getenv("ZEROTH_LIVE_LINEAR") != "1" {
		t.Skip("set ZEROTH_LIVE_LINEAR=1 to query the real Linear GraphQL API")
	}
	p, err := New(Config{
		APIKey:    "introspection-only",
		AuthStyle: AuthPersonal,
		Endpoint:  defaultEndpoint,
		Log:       zap.NewNop(),
	})
	if err != nil {
		t.Fatal(err)
	}
	names := liveIssueFilterFields(t, p)
	for _, want := range []string{"delegate", "assignee", "or"} {
		if !names[want] {
			t.Errorf("live IssueFilter missing %q", want)
		}
	}
}

func TestLiveListAssigned(t *testing.T) {
	if os.Getenv("ZEROTH_LIVE_LINEAR") != "1" {
		t.Skip("set ZEROTH_LIVE_LINEAR=1 to query the real Linear GraphQL API")
	}
	key := strings.TrimSpace(os.Getenv("ZEROTH_LINEAR_API_KEY"))
	if key == "" {
		t.Skip("ZEROTH_LINEAR_API_KEY not set")
	}

	core, logs := observer.New(zap.DebugLevel)
	p, err := New(Config{
		APIKey:      key,
		AuthStyle:   AuthStyle(os.Getenv("ZEROTH_LINEAR_AUTH_STYLE")),
		AgentUserID: strings.TrimSpace(os.Getenv("ZEROTH_LINEAR_AGENT_USER")),
		TeamID:      strings.TrimSpace(os.Getenv("ZEROTH_LINEAR_TEAM_ID")),
		ProjectID:   strings.TrimSpace(os.Getenv("ZEROTH_LINEAR_PROJECT_ID")),
		Log:         zap.New(core),
	})
	if err != nil {
		t.Fatal(err)
	}

	current, err := p.listAssigned(t.Context())
	if err != nil {
		t.Fatalf("listAssigned against live Linear: %v", err)
	}
	t.Logf("live poll matched %d issues", len(current))
	if n := logs.FilterMessage("tracker linear poll").FilterLevelExact(zapcore.ErrorLevel).Len(); n != 0 {
		t.Fatalf("unexpected poll error logs: %v", logs.All())
	}
}

func liveIssueFilterFields(t *testing.T, p *Provider) map[string]bool {
	t.Helper()
	var schema struct {
		Type *struct {
			InputFields []struct {
				Name string `json:"name"`
			} `json:"inputFields"`
		} `json:"__type"`
	}
	const q = `query IssueFilterFields { __type(name: "IssueFilter") { inputFields { name } } }`
	if err := p.query(t.Context(), "IssueFilterFields", q, nil, &schema); err != nil {
		t.Fatalf("introspect IssueFilter: %v", err)
	}
	if schema.Type == nil {
		t.Fatal("IssueFilter type missing from live schema")
	}
	names := map[string]bool{}
	for _, f := range schema.Type.InputFields {
		names[f.Name] = true
	}
	return names
}
