package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/avivl/zeroth/internal/sandbox"
)

// AllowEgress implements [sandbox.Driver].
func (d *Driver) AllowEgress(ctx context.Context, id sandbox.ID, rules []sandbox.EgressRule) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker egress: %w", err)
	}
	for _, r := range rules {
		if strings.TrimSpace(r.Host) == "" {
			return fmt.Errorf("sandbox docker egress: %w", sandbox.ErrInvalid)
		}
		if r.Port < 0 || r.Port > 65535 {
			return fmt.Errorf("sandbox docker egress: %w", sandbox.ErrInvalid)
		}
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker egress: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.stopped {
		return fmt.Errorf("sandbox docker egress: %w", sandbox.ErrStopped)
	}
	if inst.killed {
		return fmt.Errorf("sandbox docker egress: %w", sandbox.ErrKilled)
	}

	if len(rules) == 0 {
		if inst.proxy != nil {
			_ = inst.proxy.Close()
			inst.proxy = nil
		}
		if inst.bridged && inst.container != "" {
			_ = exec.CommandContext(ctx, "docker", "network", "disconnect", "bridge", inst.container).Run()
			inst.bridged = false
		}
		return nil
	}

	if inst.proxy == nil {
		px, err := listenProxy(rules)
		if err != nil {
			return fmt.Errorf("sandbox docker egress: %w", err)
		}
		inst.proxy = px
	} else {
		inst.proxy.SetRules(rules)
	}
	if !inst.bridged && inst.container != "" {
		if _, err := d.docker(ctx, "network", "connect", "bridge", inst.container); err != nil {
			return fmt.Errorf("sandbox docker egress: connect: %w", err)
		}
		inst.bridged = true
	}
	return nil
}

type egressProxy struct {
	ln  net.Listener
	srv *http.Server

	mu    sync.Mutex
	rules []sandbox.EgressRule
}

func listenProxy(rules []sandbox.EgressRule) (*egressProxy, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	p := &egressProxy{ln: ln, rules: append([]sandbox.EgressRule(nil), rules...)}
	p.srv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

func (p *egressProxy) SetRules(rules []sandbox.EgressRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = append([]sandbox.EgressRule(nil), rules...)
}

func (p *egressProxy) URL() string {
	if p == nil || p.ln == nil {
		return ""
	}
	port := 0
	if ta, ok := p.ln.Addr().(*net.TCPAddr); ok {
		port = ta.Port
	}
	return fmt.Sprintf("http://host.docker.internal:%d", port)
}

func (p *egressProxy) Close() error {
	if p == nil || p.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.srv.Shutdown(ctx); err != nil {
		_ = p.srv.Close()
		return fmt.Errorf("proxy close: %w", err)
	}
	return nil
}

func (p *egressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, port, err := requestDest(r)
	if err != nil {
		http.Error(w, "egress denied", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	ok := allows(p.rules, host, port)
	p.mu.Unlock()
	if !ok {
		http.Error(w, "egress denied", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r, host, port)
		return
	}
	p.handleForward(w, r)
}

func allows(rules []sandbox.EgressRule, host string, port int) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, r := range rules {
		if strings.ToLower(strings.TrimSpace(r.Host)) != host {
			continue
		}
		if r.Port == 0 {
			if port == 80 || port == 443 {
				return true
			}
			continue
		}
		if r.Port == port {
			return true
		}
	}
	return false
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
	return splitHostPort(raw, def)
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

func (p *egressProxy) handleForward(w http.ResponseWriter, r *http.Request) {
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

func (p *egressProxy) handleConnect(w http.ResponseWriter, r *http.Request, host string, port int) {
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
