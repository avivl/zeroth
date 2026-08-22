package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/avivl/zeroth/internal/resilience"
	"github.com/avivl/zeroth/internal/tracker"
	"github.com/failsafe-go/failsafe-go"
)

type gqlRequest struct {
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables,omitempty"`
	OperationName string         `json:"operationName,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

func (p *Provider) query(ctx context.Context, op, q string, vars map[string]any, dest any) error {
	body, err := json.Marshal(gqlRequest{Query: q, Variables: vars, OperationName: op})
	if err != nil {
		return fmt.Errorf("tracker linear %s: marshal: %w", op, err)
	}
	res, err := resilience.Get(ctx, p.execer, func(ctx context.Context) (gqlResponse, error) {
		return p.roundTrip(ctx, body)
	})
	if err != nil {
		return fmt.Errorf("tracker linear %s: %w", op, err)
	}
	if len(res.Errors) > 0 {
		msg := res.Errors[0].Message
		if isNotFound(msg) {
			return fmt.Errorf("tracker linear %s: %w", op, tracker.ErrNotFound)
		}
		return fmt.Errorf("tracker linear %s: %s", op, msg)
	}
	if dest == nil {
		return nil
	}
	if len(res.Data) == 0 || string(res.Data) == "null" {
		return fmt.Errorf("tracker linear %s: %w", op, tracker.ErrNotFound)
	}
	if err := json.Unmarshal(res.Data, dest); err != nil {
		return fmt.Errorf("tracker linear %s: decode: %w", op, err)
	}
	return nil
}

func (p *Provider) roundTrip(ctx context.Context, body []byte) (gqlResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return gqlResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", p.authorizationHeader())
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return gqlResponse{}, fmt.Errorf("%w: %v", tracker.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return gqlResponse{}, fmt.Errorf("%w: read: %v", tracker.ErrUnavailable, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return gqlResponse{}, fmt.Errorf("tracker linear: unauthorized: %w", tracker.ErrInvalid)
	}
	if resp.StatusCode >= 500 {
		return gqlResponse{}, fmt.Errorf("tracker linear: http %d: %w", resp.StatusCode, tracker.ErrUnavailable)
	}
	if resp.StatusCode >= 400 {
		return gqlResponse{}, fmt.Errorf("tracker linear: http %d: %w", resp.StatusCode, tracker.ErrInvalid)
	}
	var out gqlResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return gqlResponse{}, fmt.Errorf("tracker linear: decode http: %w", err)
	}
	return out, nil
}

func (p *Provider) authorizationHeader() string {
	if p.authStyle == AuthOAuth {
		return "Bearer " + p.apiKey
	}
	return p.apiKey
}

func parseAuthStyle(s AuthStyle) (AuthStyle, error) {
	switch AuthStyle(strings.ToLower(strings.TrimSpace(string(s)))) {
	case "", AuthPersonal:
		return AuthPersonal, nil
	case AuthOAuth:
		return AuthOAuth, nil
	default:
		return "", fmt.Errorf("tracker linear: unknown auth style %q (want personal or oauth): %w", s, tracker.ErrInvalid)
	}
}

func isNotFound(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "not found") || strings.Contains(m, "entity not found")
}

func newExecutor() failsafe.Executor[gqlResponse] {
	opts := resilience.Defaults()
	breaker := resilience.NewBreaker[gqlResponse](opts)
	return resilience.NewExecutor[gqlResponse](opts, breaker)
}
