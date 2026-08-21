// Command spike is the throwaway BA-6 confirmation-spike process.
//
// With no subcommand it serves HTTP on :8421 (compose). Subcommands
// talk to that process: run (headless), attach (replay then live tail),
// bg (demote), bench (G1/G6).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	spike "github.com/avivl/zeroth/zeroth-spike"
	"github.com/avivl/zeroth/zeroth-spike/bench"
	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		os.Exit(serveCmd(args))
	}
	switch args[0] {
	case "serve":
		os.Exit(serveCmd(args[1:]))
	case "run":
		os.Exit(runCmd(args[1:]))
	case "attach":
		os.Exit(attachCmd(args[1:]))
	case "bg":
		os.Exit(bgCmd(args[1:]))
	case "bench":
		os.Exit(benchCmd(args[1:]))
	default:
		fmt.Fprintf(os.Stderr, "spike: unknown command %q (serve|run|attach|bg|bench)\n", args[0])
		os.Exit(2)
	}
}

func serveCmd(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", envOr("SPIKE_ADDR", ":8421"), "listen address")
	fixtures := fs.String("fixtures", envOr("SPIKE_FIXTURES", "./fixtures"), "fixture tar directory")
	dbPath := fs.String("db", envOr("SPIKE_DB", "spike.db"), "SQLite event log path")
	check := fs.Bool("check", false, "GET /health on -addr and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *check {
		if err := checkHealth(*addr); err != nil {
			fmt.Fprintf(os.Stderr, "spike check: %v\n", err)
			return 1
		}
		return 0
	}
	srv, err := spike.NewServer(spike.ServerConfig{FixturesDir: *fixtures, DBPath: *dbPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike serve: %v\n", err)
		return 1
	}
	defer srv.Close()
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "spike listening on %s (fixtures %s db %s)\n", *addr, *fixtures, *dbPath)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "spike listen: %v\n", err)
		return 1
	}
	return 0
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", envOr("SPIKE_ADDR", "127.0.0.1:8421"), "spike server address")
	agent := fs.String("agent", "fake", "agent: fake, cmd, or claude")
	interval := fs.Int("interval-ms", 20, "fake agent token interval")
	cmdPath := fs.String("cmd", "", "binary for -agent cmd")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	req := struct {
		Agent      string   `json:"agent"`
		IntervalMs int      `json:"interval_ms,omitempty"`
		Cmd        string   `json:"cmd,omitempty"`
		Args       []string `json:"args,omitempty"`
	}{
		Agent:      *agent,
		IntervalMs: *interval,
		Cmd:        *cmdPath,
		Args:       fs.Args(),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike run: %v\n", err)
		return 1
	}
	res, err := http.Post(httpURL(*addr, "/sessions"), "application/json", bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike run: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(res.Body)
		fmt.Fprintf(os.Stderr, "spike run: status %d %s\n", res.StatusCode, slurp)
		return 1
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		fmt.Fprintf(os.Stderr, "spike run: %v\n", err)
		return 1
	}
	fmt.Println(out.ID)
	return 0
}

func attachCmd(args []string) int {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", envOr("SPIKE_ADDR", "127.0.0.1:8421"), "spike server address")
	last := fs.Int("last", 50, "replay this many recent events")
	maxLive := fs.Int("max-events", 0, "exit after this many live tokens (0 = until interrupt)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: spike attach [flags] <session-id>\n")
		return 2
	}
	id := fs.Arg(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws := wsURL(*addr, "/sessions/"+id+"/events?last="+strconv.Itoa(*last))
	c, _, err := websocket.Dial(ctx, ws, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike attach: %v\n", err)
		return 1
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	caught := false
	live := 0
	for {
		var f spike.Frame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			if ctx.Err() != nil {
				return 0
			}
			fmt.Fprintf(os.Stderr, "spike attach: %v\n", err)
			return 1
		}
		prefix := "live"
		if f.Replay {
			prefix = "replay"
		}
		if f.Type == spike.FrameCaughtUp {
			fmt.Fprintf(os.Stdout, "caught_up seq=%d\n", f.Seq)
			caught = true
			continue
		}
		fmt.Fprintf(os.Stdout, "%s seq=%d type=%s payload=%s\n", prefix, f.Seq, f.Type, f.Payload)
		if caught && f.Type == eventlog.TypeToken && *maxLive > 0 {
			live++
			if live >= *maxLive {
				return 0
			}
		}
	}
}

func bgCmd(args []string) int {
	fs := flag.NewFlagSet("bg", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", envOr("SPIKE_ADDR", "127.0.0.1:8421"), "spike server address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: spike bg [flags] <session-id>\n")
		return 2
	}
	id := fs.Arg(0)
	res, err := http.Post(httpURL(*addr, "/sessions/"+id+"/bg"), "application/json", bytes.NewReader(nil))
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike bg: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		fmt.Fprintf(os.Stderr, "spike bg: status %d %s\n", res.StatusCode, slurp)
		return 1
	}
	return 0
}

func benchCmd(args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	warmup := fs.Int("warmup", 10, "discarded runs before sampling")
	samples := fs.Int("samples", 110, "measured samples")
	sessions := fs.Int("sessions", 5, "concurrent sessions for G6")
	dir := fs.String("dir", "", "working directory (default: temp)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	work := *dir
	if work == "" {
		var err error
		work, err = os.MkdirTemp("", "spike-bench-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "spike bench: %v\n", err)
			return 1
		}
		defer os.RemoveAll(work)
	}
	cfg := bench.Config{
		Dir:      work,
		Warmup:   *warmup,
		Samples:  *samples,
		Sessions: *sessions,
	}
	fmt.Fprintf(os.Stderr, "spike bench: G1 warm (warmup=%d samples=%d)\n", cfg.Warmup, cfg.Samples)
	report, err := bench.Run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike bench: %v\n", err)
		return 1
	}
	fmt.Print(bench.Markdown(report))
	if !report.G1Pass || !report.G6Pass {
		return 1
	}
	return 0
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func checkHealth(addr string) error {
	url := httpURL(addr, "/health")
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, res.StatusCode)
	}
	return nil
}

func httpURL(addr, path string) string {
	host := addr
	if !strings.Contains(host, "://") {
		if strings.HasPrefix(host, ":") {
			host = "127.0.0.1" + host
		}
		host = "http://" + host
	}
	return strings.TrimRight(host, "/") + path
}

func wsURL(addr, path string) string {
	u := httpURL(addr, path)
	return strings.Replace(u, "http", "ws", 1)
}
