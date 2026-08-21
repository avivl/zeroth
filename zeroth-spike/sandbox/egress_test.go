package sandbox

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAllowlistDenyByDefault(t *testing.T) {
	t.Parallel()
	var empty Allowlist
	if empty.Allows("example.com", 443) {
		t.Fatal("empty allowlist allowed a destination")
	}
	a := Allowlist{Destinations: []Destination{{Host: "example.com", Port: 443}}}
	if a.Allows("example.com", 80) {
		t.Fatal("port 80 matched a 443 lease")
	}
	if a.Allows("EXAMPLE.com", 443) != true {
		t.Fatal("host match should be case-insensitive")
	}
	if a.Allows("93.184.216.34", 443) {
		t.Fatal("IP must not satisfy a hostname lease")
	}
	anyTLS := Allowlist{Destinations: []Destination{{Host: "example.com"}}}
	if !anyTLS.Allows("example.com", 443) || !anyTLS.Allows("example.com", 80) {
		t.Fatal("port 0 should allow 80 and 443")
	}
	if anyTLS.Allows("example.com", 8080) {
		t.Fatal("port 0 should not allow 8080")
	}
}

func TestAllowlistFromLeases(t *testing.T) {
	t.Parallel()
	got, err := AllowlistFromLeases([]EgressLease{
		{Destinations: []string{"example.com:443", "http://127.0.0.1:9"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Allows("example.com", 443) || !got.Allows("127.0.0.1", 9) {
		t.Fatalf("leases not imported: %+v", got)
	}
	if _, err := AllowlistFromLeases([]EgressLease{{Destinations: []string{""}}}); err == nil {
		t.Fatal("expected empty destination error")
	}
}

func TestProxyAllowAndDeny(t *testing.T) {
	t.Parallel()
	allowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "allowed-body")
	}))
	t.Cleanup(allowSrv.Close)
	denySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "denied-body")
	}))
	t.Cleanup(denySrv.Close)

	host, port, err := splitHostPort(strings.TrimPrefix(allowSrv.URL, "http://"), 80)
	if err != nil {
		t.Fatal(err)
	}
	px, err := ListenProxy(Allowlist{Destinations: []Destination{{Host: host, Port: port}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Close() })

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: px.Addr()}),
		},
	}

	resp, err := client.Get(allowSrv.URL)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allow status = %d", resp.StatusCode)
	}
	if string(body) != "allowed-body" {
		t.Fatalf("allow body = %q", body)
	}

	resp, err = client.Get(denySrv.URL)
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("deny status = %d body=%q", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "denied-body") {
		t.Fatal("denied destination reached the upstream")
	}
}

func TestProxyCONNECTAllowAndDeny(t *testing.T) {
	t.Parallel()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tls-ok")
	}))
	t.Cleanup(tlsSrv.Close)
	denySrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tls-no")
	}))
	t.Cleanup(denySrv.Close)

	host, port, err := splitHostPort(strings.TrimPrefix(tlsSrv.URL, "https://"), 443)
	if err != nil {
		t.Fatal(err)
	}
	px, err := ListenProxy(Allowlist{Destinations: []Destination{{Host: host, Port: port}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = px.Close() })

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(&url.URL{Scheme: "http", Host: px.Addr()}),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // local httptest cert
		},
	}

	resp, err := client.Get(tlsSrv.URL)
	if err != nil {
		t.Fatalf("allow connect: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allow connect status = %d", resp.StatusCode)
	}

	resp, err = client.Get(denySrv.URL)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("deny connect status = %d", resp.StatusCode)
		}
	}
}

func TestMeasureEgress(t *testing.T) {
	t.Parallel()
	got, err := MeasureEgress(t.Context(), 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AllowOK {
		t.Fatal("allow did not succeed")
	}
	if !got.DenyOK {
		t.Fatal("denied destination did not fail")
	}
	if got.DeltaP50 >= 20*time.Millisecond {
		t.Fatalf("proxy delta p50 %s exceeds 20ms", got.DeltaP50)
	}
	if !got.Pass {
		t.Fatalf("measure did not pass: %+v", got)
	}
	t.Logf("direct p50=%s proxy p50=%s delta p50=%s", got.DirectP50, got.ProxyP50, got.DeltaP50)
}
