package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/avivl/zeroth/internal/tracker"
)

// ServeHTTP implements the optional Linear webhook (Z1-082). Unsigned or
// unexpected payloads are rejected. The daemon mounts this only when a
// webhook secret is configured; polling remains the source of truth.
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.webhookSecret == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("Linear-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Linear-Signature")
	}
	if !validWebhook(p.webhookSecret, body, sig) {
		http.Error(w, "signature", http.StatusUnauthorized)
		return
	}
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	p.handleWebhook(payload)
	w.WriteHeader(http.StatusAccepted)
}

type webhookPayload struct {
	Action string `json:"action"`
	Type   string `json:"type"`
	Data   struct {
		ID          string `json:"id"`
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
		AssigneeID  string `json:"assigneeId"`
	} `json:"data"`
}

func (p *Provider) handleWebhook(payload webhookPayload) {
	if !strings.EqualFold(payload.Type, "Issue") {
		return
	}
	key := payload.Data.Identifier
	if key == "" {
		key = payload.Data.ID
	}
	if key == "" {
		return
	}
	agent := p.agentID()
	if agent == "" {
		return
	}
	iss := tracker.Issue{
		Key:         key,
		ID:          payload.Data.ID,
		Title:       payload.Data.Title,
		Description: payload.Data.Description,
		URL:         payload.Data.URL,
		AssigneeID:  payload.Data.AssigneeID,
	}
	wantAgent := payload.Action != "remove" && payload.Data.AssigneeID == agent
	p.applyAssignee(iss, wantAgent)
}

func validWebhook(secret string, body []byte, sig string) bool {
	sig = strings.TrimSpace(sig)
	if secret == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(want)), []byte(strings.ToLower(sig)))
}

// SignWebhook is exported for tests that need a Linear-Signature header.
func SignWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
