package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

// Spike G1 attach-warm p50 from docs/spike/RESULTS.md (Linear 42-6):
// 10 warm-up + 110 samples, in-process WebSocket, 5ms token interval.
const spikeG1WarmP50 = 5403 * time.Microsecond

const (
	// Design-doc NFR-1 ceiling. Kept as a coarse hang detector.
	cliAttachMaxLimit = 2 * time.Second
	// Tighter than 2s so a 500ms-1.5s regression cannot hide. Sized for
	// race-instrumented CI; the recorded comparison is an uninstrumented run.
	cliAttachP50Limit = 250 * time.Millisecond
	cliAttachP95Limit = 500 * time.Millisecond

	cliAttachWarmup  = 10
	cliAttachSamples = 110
	cliAttachLast    = 20
)

func TestCLIAttachLatencyWarm(t *testing.T) {
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 5 * time.Millisecond,
		TokenCount:    4000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	host := strings.TrimPrefix(hs.URL, "http://")

	var out bytes.Buffer
	run := newRoot()
	run.SetOut(&out)
	run.SetErr(io.Discard)
	run.SetArgs([]string{"--addr", host, "--log-level", "error", "run", "attach-latency"})
	if err := run.Execute(); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		t.Fatal("empty run id")
	}
	waitCLITokens(t, hs.URL, id, 3)

	total := cliAttachWarmup + cliAttachSamples
	samples := make([]time.Duration, 0, cliAttachSamples)
	for i := 0; i < total; i++ {
		afterSeq := maxEventSeq(t, hs.URL, id)
		d, err := attachFirstLive(host, id, afterSeq)
		if err != nil {
			t.Fatalf("sample %d/%d: %v", i+1, total, err)
		}
		if i == 0 || (i+1)%20 == 0 || i+1 == total {
			t.Logf("cli attach warm %d/%d last=%s", i+1, total, d)
		}
		if i >= cliAttachWarmup {
			samples = append(samples, d)
		}
	}

	p := summarizeDurations(samples)
	if p.N != cliAttachSamples {
		t.Fatalf("n=%d, want %d", p.N, cliAttachSamples)
	}
	ratio := float64(p.P50) / float64(spikeG1WarmP50)
	t.Logf("CLI attach warm n=%d p50=%s p95=%s p99=%s max=%s min=%s",
		p.N, p.P50, p.P95, p.P99, p.Max, p.Min)
	t.Logf("spike G1 warm p50=%s; CLI/spike p50 ratio=%.2fx", spikeG1WarmP50, ratio)
	t.Logf("\n%s", cliAttachMarkdown(p, ratio))

	if p.Max > cliAttachMaxLimit {
		t.Fatalf("CLI attach warm max %s exceeds G1 2s bar", p.Max)
	}
	if p.P50 > cliAttachP50Limit {
		t.Fatalf("CLI attach warm p50 %s exceeds %s (spike p50 %s, %.2fx)",
			p.P50, cliAttachP50Limit, spikeG1WarmP50, ratio)
	}
	if p.P95 > cliAttachP95Limit {
		t.Fatalf("CLI attach warm p95 %s exceeds %s (spike p50 %s)",
			p.P95, cliAttachP95Limit, spikeG1WarmP50)
	}
}

func TestParseAttachLine(t *testing.T) {
	t.Parallel()
	ev := gen.RunEvent{Id: "12", Type: "log"}
	msg := "token-3 attach-latency"
	ev.Message = &msg
	line := formatEvent(ev)
	id, typ, got := parseAttachLine(line)
	if id != "12" || typ != "log" || got != msg {
		t.Fatalf("parseAttachLine(%q) = %q %q %q", line, id, typ, got)
	}
}

func TestSummarizeDurations(t *testing.T) {
	t.Parallel()
	samples := make([]time.Duration, 110)
	for i := range samples {
		samples[i] = time.Duration(i+1) * time.Millisecond
	}
	p := summarizeDurations(samples)
	if p.N != 110 || p.Min != time.Millisecond || p.Max != 110*time.Millisecond {
		t.Fatalf("extrema n=%d min=%s max=%s", p.N, p.Min, p.Max)
	}
	// Linear interpolation, same as zeroth-spike/bench.
	if p.P50 < 55*time.Millisecond || p.P50 > 56*time.Millisecond {
		t.Fatalf("p50 %s", p.P50)
	}
}

func attachFirstLive(host, id string, afterSeq int64) (time.Duration, error) {
	pr, pw := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), cliAttachMaxLimit+time.Second)
	defer cancel()

	cmd := newRoot()
	cmd.SetOut(pw)
	cmd.SetErr(io.Discard)
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--addr", host,
		"--log-level", "error",
		"attach",
		"--last", strconv.Itoa(cliAttachLast),
		id,
	})

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- cmd.ExecuteContext(ctx)
		_ = pw.Close()
	}()

	type result struct {
		d   time.Duration
		err error
	}
	hit := make(chan result, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		sent := false
		for sc.Scan() {
			if sent {
				continue
			}
			seq, typ, msg := parseAttachLine(sc.Text())
			n, err := strconv.ParseInt(seq, 10, 64)
			if err != nil {
				continue
			}
			if n <= afterSeq {
				continue
			}
			if typ == "log" && strings.HasPrefix(msg, "token-") {
				hit <- result{d: time.Since(start)}
				sent = true
			}
		}
		if !sent {
			err := sc.Err()
			if err == nil {
				err = fmt.Errorf("attach stdout closed before live token")
			}
			hit <- result{err: err}
		}
	}()

	select {
	case r := <-hit:
		cancel()
		<-done
		if r.err != nil {
			return 0, r.err
		}
		return r.d, nil
	case err := <-done:
		if err == nil {
			err = fmt.Errorf("attach exited before live token")
		}
		return 0, err
	}
}

func parseAttachLine(line string) (id, typ, msg string) {
	line = strings.TrimSpace(line)
	id, rest, ok := strings.Cut(line, "  ")
	if !ok {
		return line, "", ""
	}
	typ, msg, ok = strings.Cut(rest, "  ")
	if !ok {
		return id, rest, ""
	}
	return id, typ, msg
}

func maxEventSeq(t *testing.T, base, id string) int64 {
	t.Helper()
	items := replayCLIEvents(t, base, id, 50)
	var max int64
	for _, ev := range items {
		n, err := strconv.ParseInt(ev.Id, 10, 64)
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}

func waitCLITokens(t *testing.T, base, id string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items := replayCLIEvents(t, base, id, 50)
		count := 0
		for _, ev := range items {
			if ev.Type == "log" && ev.Message != nil && strings.HasPrefix(*ev.Message, "token-") {
				count++
			}
		}
		if count >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d tokens", n)
}

func replayCLIEvents(t *testing.T, base, id string, last int) []gen.RunEvent {
	t.Helper()
	res, err := http.Get(base + "/runs/" + id + "/events?last=" + strconv.Itoa(last))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("replay %d %s", res.StatusCode, slurp)
	}
	var list gen.RunEventList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	return list.Items
}

type durationPercentiles struct {
	N   int
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	Max time.Duration
	Min time.Duration
}

func summarizeDurations(samples []time.Duration) durationPercentiles {
	if len(samples) == 0 {
		return durationPercentiles{}
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return durationPercentiles{
		N:   len(sorted),
		P50: durationPercentile(sorted, 50),
		P95: durationPercentile(sorted, 95),
		P99: durationPercentile(sorted, 99),
		Max: sorted[len(sorted)-1],
		Min: sorted[0],
	}
}

func durationPercentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100 * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return time.Duration(float64(sorted[lo])*(1-frac) + float64(sorted[hi])*frac)
}

func cliAttachMarkdown(p durationPercentiles, ratio float64) string {
	return fmt.Sprintf(
		"| Path | Pass bar | n | p50 | p95 | p99 | max | vs spike p50 |\n"+
			"| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |\n"+
			"| Spike G1 attach (warm, in-process WS) | < 2 s | 110 | 5.403 ms | 6.137 ms | 6.214 ms | 6.237 ms | 1x |\n"+
			"| CLI `zeroth attach` (warm) | p50 < %s, p95 < %s, max < 2s | %d | %s | %s | %s | %s | %.2fx |\n",
		cliAttachP50Limit, cliAttachP95Limit, p.N,
		formatMS(p.P50), formatMS(p.P95), formatMS(p.P99), formatMS(p.Max), ratio,
	)
}

func formatMS(d time.Duration) string {
	return fmt.Sprintf("%.3f ms", float64(d)/float64(time.Millisecond))
}
