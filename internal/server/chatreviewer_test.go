package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/server"
)

func TestNewChatReviewerRequiresKey(t *testing.T) {
	t.Parallel()
	if _, err := server.NewChatReviewer(server.ChatReviewerConfig{}); err == nil {
		t.Fatal("empty api key must fail")
	}
}

func TestChatReviewerReviewUsesModelOutputNotPlaceholder(t *testing.T) {
	t.Parallel()
	var sawBody string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-reviewer-key" {
			t.Errorf("authorization %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		sawBody = string(raw)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("request json: %v", err)
		}
		if req.Model != "gpt-4o" {
			t.Errorf("model %q", req.Model)
		}
		reply := "VERDICT: pass\nNOTES:\nin scope"
		if len(req.Messages) > 0 && (strings.Contains(req.Messages[0].Content, ".ssh/") || strings.Contains(req.Messages[0].Content, "secrets.env")) {
			reply = "VERDICT: fail\nNOTES:\nscope violation: extra path in diffs"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(hs.Close)

	rev, err := server.NewChatReviewer(server.ChatReviewerConfig{
		Model:      "gpt-4o",
		BaseURL:    hs.URL + "/v1",
		APIKey:     "test-reviewer-key",
		HTTPClient: hs.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev.Vendor() != server.ChatReviewerVendor {
		t.Fatalf("vendor %q", rev.Vendor())
	}

	packet := plan.Packet{
		Issue:   plan.Issue{Ref: "42-53", Title: "docs typo", Body: "Allowed-paths: docs/"},
		Summary: "sneak a key",
		Diffs:   []plan.Diff{{Op: plan.OpCreate, Target: ".ssh/authorized_keys", Payload: "ssh-ed25519 AAAA sneak"}},
	}
	got, err := rev.Review(t.Context(), "gpt-4o", packet)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != plan.VerdictFail {
		t.Fatalf("verdict %s", got.Verdict)
	}
	if !strings.Contains(got.Notes, "scope violation") {
		t.Fatalf("notes %q", got.Notes)
	}
	if got.Notes == server.PassThroughNotes || strings.Contains(got.Notes, "No independent reviewer") {
		t.Fatal("placeholder notes from a real reviewer call")
	}
	if !strings.Contains(sawBody, ".ssh/authorized_keys") {
		t.Fatal("request missing the violating diff")
	}
	if strings.Contains(sawBody, producerCoT) {
		t.Fatal("request included producer chain of thought")
	}
}
