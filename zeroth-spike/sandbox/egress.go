package sandbox

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
	"sync"
	"time"
)

// Destination is one host:port a lease may allow. Host matching is
// case-insensitive and literal: an IP does not satisfy a hostname.
type Destination struct {
	Host string
	Port int // 0 means 80 or 443
}

// Allowlist is deny-by-default egress. Missing, empty, or unknown
// destinations are a deny. Callers pass facts in (lease destinations);
// the proxy does not read policy or the store.
type Allowlist struct {
	Destinations []Destination
}

// Empty reports whether no destination is allowed.
func (a Allowlist) Empty() bool {
	return len(a.Destinations) == 0
}

// Allows reports whether host:port is on the list.
func (a Allowlist) Allows(host string, port int) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, d := range a.Destinations {
		if strings.ToLower(d.Host) != host {
			continue
		}
		if d.Port == 0 {
			if port == 80 || port == 443 {
				return true
			}
			continue
		}
		if d.Port == port {
			return true
		}
	}
	return false
}

// EgressLease is a spike stand-in for a policy lease's network grant.
// The kernel stays I/O-free; the sandbox is handed destinations.
type EgressLease struct {
	Destinations []string
}

// AllowlistFromLeases builds a deny-by-default list from active leases.
func AllowlistFromLeases(leases []EgressLease) (Allowlist, error) {
	var out Allowlist
	for _, lease := range leases {
		for _, raw := range lease.Destinations {
			d, err := ParseDestination(raw)
			if err != nil {
				return Allowlist{}, err
			}
			out.Destinations = append(out.Destinations, d)
		}
	}
	return out, nil
}

// ParseDestination accepts host, host:port, or an http(s) URL.
func ParseDestination(raw string) (Destination, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Destination{}, fmt.Errorf("sandbox egress destination: empty")
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return Destination{}, fmt.Errorf("sandbox egress destination: %w", err)
		}
		raw = u.Host
		if raw == "" {
			return Destination{}, fmt.Errorf("sandbox egress destination: missing host in %q", u.String())
		}
	}
	host := raw
	port := 0
	if strings.Contains(raw, ":") {
		h, p, err := net.SplitHostPort(raw)
		if err != nil {
			return Destination{}, fmt.Errorf("sandbox egress destination: %w", err)
		}
		host = h
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return Destination{}, fmt.Errorf("sandbox egress destination: bad port %q", p)
		}
		port = n
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return Destination{}, fmt.Errorf("sandbox egress destination: missing host")
	}
	return Destination{Host: host, Port: port}, nil
}

// Proxy is a deny-by-default HTTP forward / HTTPS CONNECT proxy.
// Enforcement is here: a destination not on the allowlist returns 403.
type Proxy struct {
	allow Allowlist
	ln    net.Listener
	srv   *http.Server

	mu        sync.Mutex
	latencies []time.Duration
	denied    int
	allowed   int
}

// ListenProxy binds 127.0.0.1:0 and serves until Close.
func ListenProxy(allow Allowlist) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("sandbox egress listen: %w", err)
	}
	p := &Proxy{allow: allow, ln: ln}
	p.srv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// Addr is host:port of the listening socket.
func (p *Proxy) Addr() string {
	if p == nil || p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

// Port is the listening TCP port.
func (p *Proxy) Port() int {
	if p == nil || p.ln == nil {
		return 0
	}
	if ta, ok := p.ln.Addr().(*net.TCPAddr); ok {
		return ta.Port
	}
	return 0
}

// URL is the HTTP proxy URL callers put in HTTP_PROXY.
func (p *Proxy) URL() string {
	return "http://" + p.Addr()
}

// Close stops the listener.
func (p *Proxy) Close() error {
	if p == nil || p.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.srv.Shutdown(ctx); err != nil {
		_ = p.srv.Close()
		return fmt.Errorf("sandbox egress close: %w", err)
	}
	return nil
}

// Stats are counters for measurements. Latencies are copied.
func (p *Proxy) Stats() (allowed, denied int, latencies []time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	latencies = append([]time.Duration(nil), p.latencies...)
	return p.allowed, p.denied, latencies
}

func (p *Proxy) record(allowed bool, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if allowed {
		p.allowed++
		p.latencies = append(p.latencies, d)
	} else {
		p.denied++
	}
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, port, err := requestDest(r)
	if err != nil {
		http.Error(w, "egress denied", http.StatusBadRequest)
		p.record(false, 0)
		return
	}
	if !p.allow.Allows(host, port) {
		http.Error(w, "egress denied", http.StatusForbidden)
		p.record(false, 0)
		return
	}
	start := time.Now()
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r, host, port)
		p.record(true, time.Since(start))
		return
	}
	p.handleForward(w, r)
	p.record(true, time.Since(start))
}

func requestDest(r *http.Request) (string, int, error) {
	raw := r.Host
	if r.Method != http.MethodConnect && r.URL != nil && r.URL.Host != "" {
		raw = r.URL.Host
	}
	if raw == "" {
		return "", 0, fmt.Errorf("missing host")
	}
	def := 80
	if r.Method == http.MethodConnect || (r.URL != nil && r.URL.Scheme == "https") {
		def = 443
	}
	host, port, err := splitHostPort(raw, def)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func splitHostPort(raw string, def int) (string, int, error) {
	if _, _, err := net.SplitHostPort(raw); err == nil {
		host, p, err := net.SplitHostPort(raw)
		if err != nil {
			return "", 0, err
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", 0, err
		}
		return host, n, nil
	}
	return raw, def, nil
}

func (p *Proxy) handleForward(w http.ResponseWriter, r *http.Request) {
	out := r.Clone(r.Context())
	out.RequestURI = ""
	resp, err := http.DefaultTransport.RoundTrip(out)
	if err != nil {
		http.Error(w, "egress upstream", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request, host string, port int) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "egress connect unsupported", http.StatusInternalServerError)
		return
	}
	up, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 10*time.Second)
	if err != nil {
		http.Error(w, "egress upstream", http.StatusBadGateway)
		return
	}
	defer up.Close()
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	errc := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, client)
		errc <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, up)
		errc <- struct{}{}
	}()
	<-errc
}

// EgressMeasure is G5: allow, deny, and proxy cost.
type EgressMeasure struct {
	AllowOK   bool
	DenyOK    bool
	DirectP50 time.Duration
	ProxyP50  time.Duration
	DeltaP50  time.Duration
	Samples   int
	Pass      bool
}

const egressDeltaLimit = 20 * time.Millisecond

// MeasureEgress proves per-destination allow, that a denied
// destination fails, and records the added latency through the proxy.
func MeasureEgress(ctx context.Context, warmup, samples int) (EgressMeasure, error) {
	var out EgressMeasure
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if warmup < 0 || samples < 1 {
		return out, fmt.Errorf("sandbox egress measure: bad sample counts")
	}

	allowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer allowSrv.Close()
	denySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "should-not-see")
	}))
	defer denySrv.Close()

	allowHost, allowPort, err := splitHostPort(strings.TrimPrefix(allowSrv.URL, "http://"), 80)
	if err != nil {
		return out, fmt.Errorf("sandbox egress measure: allow addr: %w", err)
	}
	px, err := ListenProxy(Allowlist{Destinations: []Destination{{Host: allowHost, Port: allowPort}}})
	if err != nil {
		return out, err
	}
	defer px.Close()

	proxyClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: px.Addr()}),
		},
	}
	directClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(nil),
		},
	}

	allowResp, err := proxyClient.Get(allowSrv.URL)
	if err != nil {
		return out, fmt.Errorf("sandbox egress measure: allow: %w", err)
	}
	_ = allowResp.Body.Close()
	out.AllowOK = allowResp.StatusCode == http.StatusOK

	denyResp, err := proxyClient.Get(denySrv.URL)
	if err != nil {
		return out, fmt.Errorf("sandbox egress measure: deny request: %w", err)
	}
	_ = denyResp.Body.Close()
	out.DenyOK = denyResp.StatusCode == http.StatusForbidden

	var direct, viaProxy []time.Duration
	total := warmup + samples
	for i := 0; i < total; i++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		d, err := timeGET(directClient, allowSrv.URL)
		if err != nil {
			return out, fmt.Errorf("sandbox egress measure: direct: %w", err)
		}
		pr, err := timeGET(proxyClient, allowSrv.URL)
		if err != nil {
			return out, fmt.Errorf("sandbox egress measure: proxy: %w", err)
		}
		if i >= warmup {
			direct = append(direct, d)
			viaProxy = append(viaProxy, pr)
		}
	}
	out.Samples = samples
	out.DirectP50 = Percentile(direct, 0.50)
	out.ProxyP50 = Percentile(viaProxy, 0.50)
	if out.ProxyP50 > out.DirectP50 {
		out.DeltaP50 = out.ProxyP50 - out.DirectP50
	}
	out.Pass = out.AllowOK && out.DenyOK && out.DeltaP50 < egressDeltaLimit
	return out, nil
}

func timeGET(c *http.Client, rawURL string) (time.Duration, error) {
	t0 := time.Now()
	resp, err := c.Get(rawURL)
	d := time.Since(t0)
	if err != nil {
		return 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return d, fmt.Errorf("status %d", resp.StatusCode)
	}
	return d, nil
}
