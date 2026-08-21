package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	apiKeyHeader = "x-api-key"
	apiURL       = "https://api.anthropic.com/v1/messages"
	apiVersion   = "2023-06-01"
	defaultModel = "claude-sonnet-4-5"
)

// MessagesClient is a tiny Anthropic Messages caller. The API key is
// read from the environment and never logged or returned.
type MessagesClient struct {
	HTTP    *http.Client
	BaseURL string
	Model   string
}

func messagesClient() *MessagesClient {
	model := strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
	if model == "" {
		model = defaultModel
	}
	return &MessagesClient{
		HTTP:    &http.Client{Timeout: 2 * time.Minute},
		BaseURL: apiURL,
		Model:   model,
	}
}

type messagesReq struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system,omitempty"`
	Messages  []messagesTurn `json:"messages"`
}

type messagesTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *MessagesClient) complete(ctx context.Context, system, user string) (string, error) {
	if err := APIKeyConfigured(); err != nil {
		return "", err
	}
	key := strings.TrimSpace(os.Getenv(apiKeyEnv))
	body, err := json.Marshal(messagesReq{
		Model:     c.Model,
		MaxTokens: 4096,
		System:    system,
		Messages:  []messagesTurn{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("harness messages: %w", err)
	}
	base := c.BaseURL
	if base == "" {
		base = apiURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("harness messages: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set(apiKeyHeader, key)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("harness messages: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("harness messages: read: %w", err)
	}
	var parsed messagesResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("harness messages: decode: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("harness messages: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("harness messages: status %d", resp.StatusCode)
	}
	var b strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", fmt.Errorf("harness messages: empty text")
	}
	return out, nil
}

const parserAgentPrompt = `Extract the proposed file changes from the transcript.
Reply with exactly one JSON object and nothing else:
{"effects":[{"op":"create|modify|destroy","target":"path","diff":"..."}]}
Each effect needs op, target, and diff or payload. No markdown fences.`

// ParseEffectsWithAgent tries the deterministic parser, then a second
// model pass over the raw transcript if that fails.
func ParseEffectsWithAgent(ctx context.Context, transcript string, client *MessagesClient) (effects []Effect, agent bool, err error) {
	effects, err = ParseEffects(transcript)
	if err == nil {
		return effects, false, nil
	}
	if client == nil {
		return nil, false, err
	}
	if ctx.Err() != nil {
		return nil, false, err
	}
	extracted, agentErr := client.complete(ctx, parserAgentPrompt, transcript)
	if agentErr != nil {
		return nil, true, fmt.Errorf("harness parser agent: %v (first parse: %w)", agentErr, err)
	}
	effects, parseErr := ParseEffects(extracted)
	if parseErr != nil {
		return nil, true, fmt.Errorf("harness parser agent: %v (first parse: %w)", parseErr, err)
	}
	return effects, true, nil
}
