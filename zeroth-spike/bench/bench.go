package bench

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	spike "github.com/avivl/zeroth/zeroth-spike"
	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/avivl/zeroth/zeroth-spike/session"
	"github.com/avivl/zeroth/zeroth-spike/supervisor"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	g1WarmLimit  = 2 * time.Second
	g6StallLimit = 50 * time.Millisecond
)

// Config is the measurement plan. Zero values are replaced by defaults.
type Config struct {
	Dir           string
	Warmup        int
	Samples       int
	Sessions      int
	TokenInterval time.Duration
	ReplayLast    int
	// SkipCold omits G1 cold attach. Tests use this so CI is not 120 process starts.
	SkipCold bool
	// ColdWarmup and ColdSamples default separately from warm/G6. Each cold
	// sample opens a new SQLite file, so 110 of them is a process tax, not a
	// latency signal. The issue asks for a recorded cold number, not 110.
	ColdWarmup  int
	ColdSamples int
}

// DefaultConfig is 10 warm-up runs and 110 samples, matching Linear 42-6.
func DefaultConfig() Config {
	return Config{
		Warmup:        10,
		Samples:       110,
		Sessions:      5,
		TokenInterval: 5 * time.Millisecond,
		ReplayLast:    20,
		ColdWarmup:    2,
		ColdSamples:   10,
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.Warmup == 0 {
		c.Warmup = d.Warmup
	}
	if c.Samples == 0 {
		c.Samples = d.Samples
	}
	if c.Sessions == 0 {
		c.Sessions = d.Sessions
	}
	if c.TokenInterval == 0 {
		c.TokenInterval = d.TokenInterval
	}
	if c.ReplayLast == 0 {
		c.ReplayLast = d.ReplayLast
	}
	if c.ColdWarmup == 0 {
		c.ColdWarmup = d.ColdWarmup
	}
	if c.ColdSamples == 0 {
		c.ColdSamples = d.ColdSamples
	}
	return c
}

// Percentiles is p50/p95/p99 plus extrema over N samples.
type Percentiles struct {
	N   int
	P50 time.Duration
	P95 time.Duration
	P99 time.Duration
	Max time.Duration
	Min time.Duration
}

// Report is the G1/G6 result. G6Batched is set only when unbatched failed.
type Report struct {
	G1Warm      Percentiles
	G1Cold      Percentiles
	G1Pass      bool
	G6Unbatched Percentiles
	G6Pass      bool
	G6Batched   *Percentiles
}

// Run measures attach latency and SQLite write stalls.
func Run(ctx context.Context, cfg Config) (Report, error) {
	cfg = cfg.withDefaults()
	if cfg.Dir == "" {
		return Report{}, fmt.Errorf("bench run: empty dir")
	}

	var report Report
	warm, err := measureG1Warm(ctx, cfg)
	if err != nil {
		return Report{}, err
	}
	report.G1Warm = warm
	report.G1Pass = warm.P99 <= g1WarmLimit && warm.Max <= g1WarmLimit

	if !cfg.SkipCold {
		cold, err := measureG1Cold(ctx, cfg)
		if err != nil {
			return Report{}, err
		}
		report.G1Cold = cold
	}

	unbatched, err := measureG6(ctx, cfg, 1)
	if err != nil {
		return Report{}, err
	}
	report.G6Unbatched = unbatched
	report.G6Pass = unbatched.Max <= g6StallLimit
	if !report.G6Pass {
		batched, err := measureG6(ctx, cfg, 32)
		if err != nil {
			return Report{}, err
		}
		report.G6Batched = &batched
	}
	return report, nil
}

func measureG1Warm(ctx context.Context, cfg Config) (Percentiles, error) {
	srv, hs, err := newServer(cfg.Dir, "g1-warm.db")
	if err != nil {
		return Percentiles{}, err
	}
	defer hs.Close()
	defer srv.Close()

	id, err := startFake(ctx, srv, cfg.TokenInterval)
	if err != nil {
		return Percentiles{}, err
	}
	if err := waitToken(ctx, srv.Log(), id); err != nil {
		return Percentiles{}, err
	}
	samples := make([]time.Duration, 0, cfg.Samples)
	total := cfg.Warmup + cfg.Samples
	for i := 0; i < total; i++ {
		d, err := attachFirstLive(ctx, hs.URL, id.String(), cfg.ReplayLast)
		if err != nil {
			return Percentiles{}, fmt.Errorf("bench g1 warm sample %d: %w", i, err)
		}
		if i == 0 || (i+1)%20 == 0 || i+1 == total {
			fmt.Fprintf(os.Stderr, "bench g1 warm %d/%d last=%s\n", i+1, total, d)
		}
		if i >= cfg.Warmup {
			samples = append(samples, d)
		}
	}
	return summarize(samples), nil
}

func measureG1Cold(ctx context.Context, cfg Config) (Percentiles, error) {
	warmup := cfg.ColdWarmup
	n := cfg.ColdSamples
	samples := make([]time.Duration, 0, n)
	for i := 0; i < warmup+n; i++ {
		dir := filepath.Join(cfg.Dir, "cold-"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Percentiles{}, fmt.Errorf("bench g1 cold mkdir: %w", err)
		}
		srv, hs, err := newServer(dir, "g1-cold.db")
		if err != nil {
			return Percentiles{}, err
		}
		id, err := startFake(ctx, srv, cfg.TokenInterval)
		if err != nil {
			hs.Close()
			_ = srv.Close()
			return Percentiles{}, err
		}
		d, err := attachFirstLive(ctx, hs.URL, id.String(), cfg.ReplayLast)
		hs.Close()
		_ = srv.Close()
		if err != nil {
			return Percentiles{}, fmt.Errorf("bench g1 cold sample %d: %w", i, err)
		}
		if i >= warmup {
			samples = append(samples, d)
		}
	}
	return summarize(samples), nil
}

func measureG6(ctx context.Context, cfg Config, batch int) (Percentiles, error) {
	log, err := eventlog.Open(filepath.Join(cfg.Dir, fmt.Sprintf("g6-b%d.db", batch)))
	if err != nil {
		return Percentiles{}, fmt.Errorf("bench g6 open: %w", err)
	}
	defer log.Close()

	type result struct {
		d   time.Duration
		err error
	}
	out := make(chan result, cfg.Sessions)
	var wg sync.WaitGroup
	for s := 0; s < cfg.Sessions; s++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id, err := session.ParseID("g6-" + strconv.Itoa(n))
			if err != nil {
				out <- result{err: err}
				return
			}
			total := cfg.Warmup + cfg.Samples
			for i := 0; i < total; i++ {
				start := time.Now()
				var werr error
				if batch <= 1 {
					_, werr = log.Append(ctx, id, eventlog.TypeToken, "t")
				} else {
					items := make([]eventlog.Item, batch)
					for j := range items {
						items[j] = eventlog.Item{Type: eventlog.TypeToken, Payload: "t"}
					}
					_, werr = log.AppendBatch(ctx, id, items)
				}
				d := time.Since(start)
				if werr != nil {
					out <- result{err: werr}
					return
				}
				if i >= cfg.Warmup {
					out <- result{d: d}
				}
			}
		}(s)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	samples := make([]time.Duration, 0, cfg.Sessions*cfg.Samples)
	for r := range out {
		if r.err != nil {
			return Percentiles{}, fmt.Errorf("bench g6: %w", r.err)
		}
		samples = append(samples, r.d)
	}
	return summarize(samples), nil
}

func newServer(dir, name string) (*spike.Server, *httptest.Server, error) {
	srv, err := spike.NewServer(spike.ServerConfig{
		FixturesDir: dir,
		DBPath:      filepath.Join(dir, name),
	})
	if err != nil {
		return nil, nil, err
	}
	return srv, httptest.NewServer(srv), nil
}

func startFake(ctx context.Context, srv *spike.Server, interval time.Duration) (session.ID, error) {
	_ = ctx
	return srv.Supervisor().Start(&supervisor.FakeAgent{Interval: interval})
}

func waitToken(ctx context.Context, log *eventlog.Log, id session.ID) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		got, err := log.ReplayLast(ctx, id, 50)
		if err != nil {
			return err
		}
		for _, ev := range got {
			if ev.Type == eventlog.TypeToken {
				return nil
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("bench wait token: timeout")
}

func attachFirstLive(ctx context.Context, httpURL, sessionID string, last int) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, g1WarmLimit+time.Second)
	defer cancel()
	wsURL := strings.Replace(httpURL, "http", "ws", 1) + "/sessions/" + sessionID + "/events?last=" + strconv.Itoa(last)
	start := time.Now()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return 0, fmt.Errorf("bench attach dial: %w", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	caught := false
	for {
		var f spike.Frame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			return 0, fmt.Errorf("bench attach read: %w", err)
		}
		if f.Type == spike.FrameCaughtUp {
			caught = true
			continue
		}
		if caught && f.Type == eventlog.TypeToken && !f.Replay {
			return time.Since(start), nil
		}
	}
}

func summarize(samples []time.Duration) Percentiles {
	if len(samples) == 0 {
		return Percentiles{}
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Percentiles{
		N:   len(sorted),
		P50: percentile(sorted, 50),
		P95: percentile(sorted, 95),
		P99: percentile(sorted, 99),
		Max: sorted[len(sorted)-1],
		Min: sorted[0],
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
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

// Markdown renders a RESULTS.md fragment.
func Markdown(r Report) string {
	var b strings.Builder
	b.WriteString("| Gate | Pass bar | n | p50 | p95 | p99 | max | Result |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |\n")
	fmt.Fprintf(&b, "| G1 Attach (warm) | < 2 s to first live token | %d | %s | %s | %s | %s | %s |\n",
		r.G1Warm.N, ms(r.G1Warm.P50), ms(r.G1Warm.P95), ms(r.G1Warm.P99), ms(r.G1Warm.Max), pass(r.G1Pass))
	fmt.Fprintf(&b, "| G1 Attach (cold) | recorded | %d | %s | %s | %s | %s | recorded |\n",
		r.G1Cold.N, ms(r.G1Cold.P50), ms(r.G1Cold.P95), ms(r.G1Cold.P99), ms(r.G1Cold.Max))
	fmt.Fprintf(&b, "| G6 write stall (5 sessions, unbatched) | no stall > 50 ms | %d | %s | %s | %s | %s | %s |\n",
		r.G6Unbatched.N, ms(r.G6Unbatched.P50), ms(r.G6Unbatched.P95), ms(r.G6Unbatched.P99), ms(r.G6Unbatched.Max), pass(r.G6Pass))
	if r.G6Batched != nil {
		fmt.Fprintf(&b, "| G6 write stall (batched 32) | no stall > 50 ms | %d | %s | %s | %s | %s | %s |\n",
			r.G6Batched.N, ms(r.G6Batched.P50), ms(r.G6Batched.P95), ms(r.G6Batched.P99), ms(r.G6Batched.Max),
			pass(r.G6Batched.Max <= g6StallLimit))
	}
	return b.String()
}

func ms(d time.Duration) string {
	return fmt.Sprintf("%.3f ms", float64(d)/float64(time.Millisecond))
}

func pass(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}
