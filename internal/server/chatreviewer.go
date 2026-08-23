package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/plan"
	"github.com/avivl/zeroth/internal/resilience"
	"github.com/failsafe-go/failsafe-go"
)

const (
	// ChatReviewerVendor is the independent reviewer vendor. The
	// producer is Claude Code (Anthropic). Same-vendor second pass
	// is not this implementation (Linear 42-53, ADR-Z-0011).
	ChatReviewerVendor = "openai"

	defaultChatModel    = "gpt-4o"
	defaultChatBaseURL  = "https://api.openai.com/v1"
	chatCompletionsPath = "/chat/completions"
)

// ChatReviewerConfig is how zerothd constructs the OpenAI reviewer.
// APIKey is required. Model defaults to gpt-4o. BaseURL defaults to
// the public OpenAI Chat Completions root.
type ChatReviewerConfig struct {
	Model      string
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// ChatReviewer is a plan.Reviewer that calls an OpenAI-compatible
// Chat Completions endpoint. The packet encoding is the whole prompt;
// the producer's transcript is never an argument.
type ChatReviewer struct {
	model  string
	url    string
	apiKey string
	client *http.Client
	execer failsafe.Executor[plan.Review]
}

var _ plan.Reviewer = (*ChatReviewer)(nil)

// NewChatReviewer returns an independent OpenAI reviewer. Empty API
// key is a deny, not a silent pass-through.
func NewChatReviewer(cfg ChatReviewerConfig) (*ChatReviewer, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("server chat reviewer: empty api key: %w", plan.ErrNoReviewer)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultChatModel
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	opts := resilience.Defaults()
	opts.Timeout = 45 * time.Second
	opts.MaxRetries = 2
	breaker := resilience.NewBreaker[plan.Review](opts)
	return &ChatReviewer{
		model:  model,
		url:    completionsURL(cfg.BaseURL),
		apiKey: key,
		client: client,
		execer: resilience.NewExecutor(opts, breaker),
	}, nil
}

// Model is the configured reviewer model id.
func (r *ChatReviewer) Model() string {
	if r == nil {
		return ""
	}
	return r.model
}

// Vendor is the independent reviewer vendor id.
func (r *ChatReviewer) Vendor() string { return ChatReviewerVendor }

// Review implements [plan.Reviewer].
func (r *ChatReviewer) Review(ctx context.Context, model string, packet plan.Packet) (plan.Review, error) {
	if r == nil {
		return plan.Review{}, fmt.Errorf("server chat reviewer: %w", plan.ErrNoReviewer)
	}
	if err := ctx.Err(); err != nil {
		return plan.Review{}, fmt.Errorf("server chat reviewer: %w", err)
	}
	if strings.TrimSpace(model) == "" {
		model = r.model
	}
	out, err := resilience.Get(ctx, r.execer, func(ctx context.Context) (plan.Review, error) {
		return r.roundTrip(ctx, model, packet)
	})
	if err != nil {
		return plan.Review{}, fmt.Errorf("server chat reviewer: %w", err)
	}
	out.Model = model
	return out, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r *ChatReviewer) roundTrip(ctx context.Context, model string, packet plan.Packet) (plan.Review, error) {
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: packet.Encode()}},
	})
	if err != nil {
		return plan.Review{}, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return plan.Review{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	resp, err := r.client.Do(req)
	if err != nil {
		return plan.Review{}, fmt.Errorf("http: %s", redact(err.Error(), r.apiKey))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return plan.Review{}, fmt.Errorf("read: %s", redact(err.Error(), r.apiKey))
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return plan.Review{}, fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return plan.Review{}, fmt.Errorf("openai: %s", redact(parsed.Error.Message, r.apiKey))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return plan.Review{}, fmt.Errorf("http %d", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return plan.Review{}, fmt.Errorf("empty choices")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	return plan.ParseReview(model, text), nil
}

func completionsURL(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		b = defaultChatBaseURL
	}
	if strings.HasSuffix(b, chatCompletionsPath) {
		return b
	}
	return b + chatCompletionsPath
}

func redact(s, key string) string {
	if key == "" || len(key) < 8 || s == "" || !strings.Contains(s, key) {
		return s
	}
	return strings.ReplaceAll(s, key, "[redacted]")
}
