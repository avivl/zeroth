package docker

import (
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
