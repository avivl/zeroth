package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/avivl/zeroth/internal/resilience"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/failsafe-go/failsafe-go"
)

type apiClient struct {
	base string
	http *http.Client
	exec failsafe.Executor[*http.Response]
}

func newAPIClient(addr string) *apiClient {
	opts := resilience.Defaults()
	opts.Timeout = 10 * time.Second
	return &apiClient{
		base: httpBase(addr),
		http: &http.Client{Timeout: 15 * time.Second},
		exec: resilience.NewExecutor[*http.Response](opts),
	}
}

func httpBase(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = defaultDaemonAddr
	}
	if !strings.Contains(addr, "://") {
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/")
}

func wsBase(httpOrigin string) string {
	return strings.Replace(httpOrigin, "http", "ws", 1)
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	if e == nil {
		return "zeroth api: error"
	}
	if e.Code != "" {
		return fmt.Sprintf("status %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("status %d: %s", e.Status, e.Message)
}

func (c *apiClient) do(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("zeroth api marshal: %w", err)
		}
	}
	res, err := resilience.Get(ctx, c.exec, func(ctx context.Context) (*http.Response, error) {
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
		if err != nil {
			return nil, fmt.Errorf("zeroth api request: %w", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("zeroth api %s %s: %w", method, path, err)
		}
		return res, nil
	})
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("zeroth api read: %w", err)
	}
	if res.StatusCode >= 400 {
		var ae gen.Error
		if json.Unmarshal(raw, &ae) == nil && ae.Message != "" {
			return raw, res.StatusCode, &apiError{Status: res.StatusCode, Code: ae.Code, Message: ae.Message}
		}
		return raw, res.StatusCode, &apiError{Status: res.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	return raw, res.StatusCode, nil
}

func (c *apiClient) createRun(ctx context.Context, req gen.CreateRunRequest) (gen.Run, error) {
	raw, status, err := c.do(ctx, http.MethodPost, "/runs", req)
	if err != nil {
		return gen.Run{}, fmt.Errorf("zeroth run: %w", err)
	}
	if status != http.StatusCreated {
		return gen.Run{}, fmt.Errorf("zeroth run: status %d", status)
	}
	var out gen.Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.Run{}, fmt.Errorf("zeroth run: %w", err)
	}
	return out, nil
}

func (c *apiClient) getRun(ctx context.Context, id string) (gen.Run, error) {
	raw, _, err := c.do(ctx, http.MethodGet, "/runs/"+id, nil)
	if err != nil {
		return gen.Run{}, fmt.Errorf("zeroth get run: %w", err)
	}
	var out gen.Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.Run{}, fmt.Errorf("zeroth get run: %w", err)
	}
	return out, nil
}

func (c *apiClient) listRuns(ctx context.Context) (gen.RunList, error) {
	raw, _, err := c.do(ctx, http.MethodGet, "/runs", nil)
	if err != nil {
		return gen.RunList{}, fmt.Errorf("zeroth runs: %w", err)
	}
	var out gen.RunList
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.RunList{}, fmt.Errorf("zeroth runs: %w", err)
	}
	return out, nil
}

func (c *apiClient) listAgents(ctx context.Context) (gen.AgentList, error) {
	raw, _, err := c.do(ctx, http.MethodGet, "/agents", nil)
	if err != nil {
		return gen.AgentList{}, fmt.Errorf("zeroth agents: %w", err)
	}
	var out gen.AgentList
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.AgentList{}, fmt.Errorf("zeroth agents: %w", err)
	}
	return out, nil
}

func (c *apiClient) background(ctx context.Context, id string) (gen.Run, error) {
	return c.postRun(ctx, id, "/background")
}

func (c *apiClient) foreground(ctx context.Context, id string) (gen.Run, error) {
	return c.postRun(ctx, id, "/foreground")
}

func (c *apiClient) steer(ctx context.Context, id, message string) (gen.Run, error) {
	raw, _, err := c.do(ctx, http.MethodPost, "/runs/"+id+"/steer", gen.SteerRequest{Message: message})
	if err != nil {
		return gen.Run{}, fmt.Errorf("zeroth steer: %w", err)
	}
	var out gen.Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.Run{}, fmt.Errorf("zeroth steer: %w", err)
	}
	return out, nil
}

func (c *apiClient) postRun(ctx context.Context, id, suffix string) (gen.Run, error) {
	raw, _, err := c.do(ctx, http.MethodPost, "/runs/"+id+suffix, nil)
	if err != nil {
		return gen.Run{}, err
	}
	var out gen.Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.Run{}, err
	}
	return out, nil
}

func (c *apiClient) retract(ctx context.Context, id, reason string) (gen.Run, error) {
	raw, _, err := c.do(ctx, http.MethodPost, "/runs/"+id+"/retract", gen.RetractRequest{Reason: reason})
	if err != nil {
		return gen.Run{}, fmt.Errorf("zeroth retract: %w", err)
	}
	var out gen.Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return gen.Run{}, fmt.Errorf("zeroth retract: %w", err)
	}
	return out, nil
}

func isConflict(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusConflict
}
