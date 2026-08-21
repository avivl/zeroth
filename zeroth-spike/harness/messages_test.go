package harness_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avivl/zeroth/zeroth-spike/harness"
)

func TestParseEffectsWithAgentSecondPass(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-not-a-real-key")
	const extracted = `{"effects":[{"op":"modify","target":"README.md","diff":"+Version: 2"},{"op":"modify","target":"greet.go","diff":"+Greet"},{"op":"modify","target":"main.go","diff":"+Greet"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing api key header")
		}
		type contentBlock struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		type apiResp struct {
			Content []contentBlock `json:"content"`
		}
		_ = json.NewEncoder(w).Encode(apiResp{
			Content: []contentBlock{{Type: "text", Text: extracted}},
		})
	}))
	t.Cleanup(srv.Close)

	client := &harness.MessagesClient{HTTP: srv.Client(), BaseURL: srv.URL, Model: "test"}
	effects, usedAgent, err := harness.ParseEffectsWithAgent(t.Context(), "I would change README.md, greet.go, and main.go but here is prose.", client)
	if err != nil {
		t.Fatal(err)
	}
	if !usedAgent {
		t.Fatal("expected parser agent")
	}
	if !harness.ThreeFileOK(effects) {
		t.Fatalf("agent parse: %+v", effects)
	}
}

func TestMessagesClientDoesNotLogKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-not-a-real-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"type":"auth","message":"bad key"}}`)
	}))
	t.Cleanup(srv.Close)
	client := &harness.MessagesClient{HTTP: srv.Client(), BaseURL: srv.URL, Model: "test"}
	_, _, err := harness.ParseEffectsWithAgent(t.Context(), "not json", client)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "test-not-a-real-key") {
		t.Fatalf("error leaked key: %v", err)
	}
}
