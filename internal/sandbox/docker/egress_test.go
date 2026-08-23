package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/sandbox"
)

func TestProxyAllowDeny(t *testing.T) {
	t.Parallel()
	allowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(allowSrv.Close)
	denySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should-not-see")
	}))
	t.Cleanup(denySrv.Close)

	allowHost, allowPort := mustHostPort(t, allowSrv.URL)
	px, err := listenProxy([]sandbox.EgressRule{{Host: allowHost, Port: allowPort}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Close() })

	proxyURL, err := url.Parse("http://" + px.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}

	allowResp, err := client.Get(allowSrv.URL)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	defer allowResp.Body.Close()
	if allowResp.StatusCode != http.StatusOK {
		t.Fatalf("allow status %d", allowResp.StatusCode)
	}

	denyResp, err := client.Get(denySrv.URL)
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	defer denyResp.Body.Close()
	if denyResp.StatusCode != http.StatusForbidden {
		t.Fatalf("deny status %d, want 403", denyResp.StatusCode)
	}
}

func TestAllows(t *testing.T) {
	t.Parallel()
	rules := []sandbox.EgressRule{
		{Host: "Example.COM", Port: 443},
		{Host: "127.0.0.1", Port: 0},
	}
	if !allows(rules, "example.com", 443) {
		t.Fatal("hostname should match case-insensitively")
	}
	if allows(rules, "example.com", 80) {
		t.Fatal("port 80 is not 443")
	}
	if allows(rules, "93.184.216.34", 443) {
		t.Fatal("IP must not satisfy a hostname")
	}
	if !allows(rules, "127.0.0.1", 80) || !allows(rules, "127.0.0.1", 443) {
		t.Fatal("port 0 means 80 and 443")
	}
	if allows(rules, "127.0.0.1", 9) {
		t.Fatal("port 0 does not mean 9")
	}
}

func TestAllowEgressDenyAllSetupFailure(t *testing.T) {
	t.Parallel()

	disconnectFail := func(_ context.Context, args []string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "network" && args[1] == "disconnect" {
			return nil, fmt.Errorf("simulated network disconnect failure")
		}
		return nil, nil
	}
	connectFail := func(_ context.Context, args []string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "network" && args[1] == "connect" {
			return nil, fmt.Errorf("simulated network connect failure")
		}
		return nil, nil
	}
	okDocker := func(_ context.Context, args []string) ([]byte, error) {
		return nil, nil
	}

	cases := []struct {
		name        string
		runDocker   func(context.Context, []string) ([]byte, error)
		closeErr    error
		wantSubstr  string
		wantBridged bool
	}{
		{
			name:        "disconnect_bridge",
			runDocker:   disconnectFail,
			wantSubstr:  "disconnect bridge",
			wantBridged: true,
		},
		{
			name:        "connect_none",
			runDocker:   connectFail,
			wantSubstr:  "connect none",
			wantBridged: false,
		},
		{
			name:        "close_proxy",
			runDocker:   okDocker,
			closeErr:    fmt.Errorf("simulated proxy close failure"),
			wantSubstr:  "close proxy",
			wantBridged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, id, inst := denyAllTestInstance(t, tc.runDocker, tc.closeErr)

			err := d.AllowEgress(t.Context(), id, nil)
			if err == nil {
				t.Fatal("AllowEgress deny-all succeeded; caller would start a session with unrestricted egress")
			}
			if !strings.Contains(err.Error(), "deny-all") {
				t.Fatalf("error %q should name the deny-all path", err)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q should contain %q", err, tc.wantSubstr)
			}

			inst.mu.Lock()
			defer inst.mu.Unlock()
			if inst.bridged != tc.wantBridged {
				t.Fatalf("bridged=%v, want %v (must not claim deny-all while isolation failed)", inst.bridged, tc.wantBridged)
			}
			if inst.proxy == nil {
				t.Fatal("proxy cleared after a failed deny-all; allowlist should remain until isolation succeeds")
			}
		})
	}
}

func TestAllowEgressDenyAllSucceedsWhenIsolationEstablished(t *testing.T) {
	t.Parallel()
	d, id, inst := denyAllTestInstance(t, func(context.Context, []string) ([]byte, error) {
		return nil, nil
	}, nil)

	if err := d.AllowEgress(t.Context(), id, nil); err != nil {
		t.Fatalf("AllowEgress deny-all: %v", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.bridged {
		t.Fatal("bridged still set after successful deny-all")
	}
	if inst.proxy != nil {
		t.Fatal("proxy still set after successful deny-all")
	}
}

func TestAllowEgressDenyAllLiveDisconnectFailure(t *testing.T) {
	t.Parallel()
	if err := Available(); err != nil {
		t.Skipf("docker sandbox unavailable: %v", err)
	}
	d := New()
	id, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst := &instance{
		id:        id,
		container: "zeroth-sbx-missing-" + id.String(),
		bridged:   true,
		proxy:     &egressProxy{},
	}
	d.mu.Lock()
	d.inst[id.String()] = inst
	d.mu.Unlock()

	err = d.AllowEgress(t.Context(), id, nil)
	if err == nil {
		t.Fatal("AllowEgress deny-all succeeded against a missing container")
	}
	if !strings.Contains(err.Error(), "deny-all") {
		t.Fatalf("error %q should name the deny-all path", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.bridged {
		t.Fatal("bridged cleared after a real docker disconnect failure")
	}
}

func denyAllTestInstance(t *testing.T, runDocker func(context.Context, []string) ([]byte, error), closeErr error) (*Driver, sandbox.ID, *instance) {
	t.Helper()
	d := New()
	d.runDocker = runDocker
	id, err := sandbox.NewID()
	if err != nil {
		t.Fatal(err)
	}
	inst := &instance{
		id:        id,
		container: "zeroth-sbx-test-" + id.String(),
		bridged:   true,
		proxy:     &egressProxy{closeErr: closeErr},
	}
	d.mu.Lock()
	d.inst[id.String()] = inst
	d.mu.Unlock()
	return d, id, inst
}

func mustHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(host), n
}
