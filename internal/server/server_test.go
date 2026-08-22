package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 5 * time.Millisecond,
		TokenCount:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs
}

func TestHealthAndDefaultAgent(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	res, err := http.Get(hs.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health %d", res.StatusCode)
	}
	res, err = http.Get(hs.URL + "/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list gen.AgentList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || string(list.Items[0].Id) != server.DefaultAgentID {
		t.Fatalf("agents %+v", list.Items)
	}
}

func TestAuditChainAndAgentPatch(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	run := createRun(t, hs.URL, "audited")

	res, err := http.Get(hs.URL + "/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list audit %d", res.StatusCode)
	}
	var list gen.AuditList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) < 2 {
		t.Fatalf("expected agent.create and run.create, got %d", len(list.Items))
	}
	foundRun := false
	for _, rec := range list.Items {
		if rec.Action == "run.create" && rec.RunId != nil && *rec.RunId == run.Id {
			foundRun = true
		}
		vr := postJSON(t, hs.URL+"/audit/"+string(rec.Id)+"/verify", struct{}{})
		defer vr.Body.Close()
		if vr.StatusCode != http.StatusOK {
			t.Fatalf("verify %s: %d", rec.Id, vr.StatusCode)
		}
		var got gen.AuditVerification
		if err := json.NewDecoder(vr.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Valid {
			t.Fatalf("record %s invalid: %v", rec.Id, got.Reason)
		}
	}
	if !foundRun {
		t.Fatal("run.create missing from audit list")
	}

	tier := "t2"
	body, err := json.Marshal(gen.AgentPatch{AutonomyTier: &tier})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPatch, hs.URL+"/agents/"+server.DefaultAgentID, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	patch, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer patch.Body.Close()
	if patch.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(patch.Body)
		t.Fatalf("patch agent %d %s", patch.StatusCode, slurp)
	}
	var agent gen.Agent
	if err := json.NewDecoder(patch.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	if agent.AutonomyTier == nil || *agent.AutonomyTier != "t2" {
		t.Fatalf("tier %+v", agent.AutonomyTier)
	}
}

func TestCreateListSteerBackground(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	run := createRun(t, hs.URL, "hello")
	if run.Status != gen.RunStatusRunning {
		t.Fatalf("status %s", run.Status)
	}
	if run.Prompt == nil || *run.Prompt != "hello" {
		t.Fatalf("prompt %+v", run.Prompt)
	}

	bg := postRun(t, hs.URL, string(run.Id), "/background")
	if bg.Status != gen.RunStatusBackgrounded {
		t.Fatalf("bg %s", bg.Status)
	}
	fg := postRun(t, hs.URL, string(run.Id), "/foreground")
	if fg.Status != gen.RunStatusRunning {
		t.Fatalf("fg %s", fg.Status)
	}

	steer := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/steer", gen.SteerRequest{Message: "nudge"})
	if steer.StatusCode != http.StatusOK {
		t.Fatalf("steer %d", steer.StatusCode)
	}
	steer.Body.Close()

	res, err := http.Get(hs.URL + "/runs")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list gen.RunList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Id != run.Id {
		t.Fatalf("list %+v", list.Items)
	}
}

func TestHTTPReplayAndWSLiveTail(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	run := createRun(t, hs.URL, "tail-me")
	waitTokens(t, hs.URL, string(run.Id), 1)

	res, err := http.Get(hs.URL + "/runs/" + string(run.Id) + "/events?last=50")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var replay gen.RunEventList
	if err := json.NewDecoder(res.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	if len(replay.Items) < 2 {
		t.Fatalf("replay %d", len(replay.Items))
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(hs.URL, "/runs/"+string(run.Id)+"/events?last=5"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	sawLive := false
	for i := 0; i < 20; i++ {
		var ev gen.RunEvent
		if err := wsjson.Read(ctx, c, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.RunId != run.Id {
			t.Fatalf("run_id %s", ev.RunId)
		}
		if ev.Message != nil && strings.HasPrefix(*ev.Message, "token-") {
			sawLive = true
			break
		}
	}
	if !sawLive {
		t.Fatal("no token on websocket")
	}
}

// TestAttachLatencyWarm is the coarse NFR-1 CI gate: a single in-process
// WebSocket attach to a live run must reach a first live token in under 2s.
// That bar is the design-doc ceiling, not the spike's measured 5.403ms p50.
// Percentiles for a real `zeroth attach` (warmup + 110 samples) live in
// cmd/zeroth.TestCLIAttachLatencyWarm and docs/cli/ATTACH_LATENCY.md
// (Linear 42-38).
func TestAttachLatencyWarm(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 5 * time.Millisecond,
		TokenCount:    80,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "latency")
	waitTokens(t, hs.URL, string(run.Id), 3)
	snapshot := replayEvents(t, hs.URL, string(run.Id), 50)
	var maxSeq int64
	for _, ev := range snapshot {
		n, _ := strconv.ParseInt(ev.Id, 10, 64)
		if n > maxSeq {
			maxSeq = n
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	c, _, err := websocket.Dial(ctx, wsURL(hs.URL, "/runs/"+string(run.Id)+"/events?last=50"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	for {
		var ev gen.RunEvent
		if err := wsjson.Read(ctx, c, &ev); err != nil {
			t.Fatal(err)
		}
		n, _ := strconv.ParseInt(ev.Id, 10, 64)
		if n > maxSeq && ev.Message != nil && strings.HasPrefix(*ev.Message, "token-") {
			d := time.Since(start)
			if d > 2*time.Second {
				t.Fatalf("attach latency %s exceeds G1 2s bar", d)
			}
			t.Logf("G1 attach warm (in-process WS, single sample): %s; CLI percentiles vs spike 5.403ms p50: TestCLIAttachLatencyWarm", d)
			return
		}
	}
}

func TestWSCancelDoesNotKillRun(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 10 * time.Millisecond,
		TokenCount:    40,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "survive")
	ctx, cancel := context.WithCancel(t.Context())
	c, _, err := websocket.Dial(ctx, wsURL(hs.URL, "/runs/"+string(run.Id)+"/events?last=10"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ev gen.RunEvent
	if err := wsjson.Read(ctx, c, &ev); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = c.Close(websocket.StatusNormalClosure, "")
	time.Sleep(30 * time.Millisecond)

	got := getRun(t, hs.URL, string(run.Id))
	if got.Status == gen.RunStatusCompleted || got.Status == gen.RunStatusFailed || got.Status == gen.RunStatusCancelled {
		t.Fatalf("run terminated after detach: %s", got.Status)
	}
}

func TestReconnectSkipsDuplicates(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: 8 * time.Millisecond,
		TokenCount:    40,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "reconnect")
	var mu sync.Mutex
	seen := map[string]int{}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- followAndCount(ctx, hs, string(run.Id), seen, &mu)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for uniqueCount(&mu, seen) < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if uniqueCount(&mu, seen) < 4 {
		t.Fatal("did not receive initial events")
	}
	beforeDrop := uniqueCount(&mu, seen)
	hs.CloseClientConnections()

	deadline = time.Now().Add(3 * time.Second)
	for uniqueCount(&mu, seen) <= beforeDrop && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if uniqueCount(&mu, seen) <= beforeDrop {
		t.Fatal("did not resume after drop")
	}
	mu.Lock()
	for id, n := range seen {
		if n > 1 {
			mu.Unlock()
			t.Fatalf("duplicate event %s x%d", id, n)
		}
	}
	mu.Unlock()
	cancel()
	<-errc
}

func TestDemoTenTimes(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10; i++ {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			hs := testServer(t)
			run := createRun(t, hs.URL, "demo")
			postRun(t, hs.URL, string(run.Id), "/background")
			waitTokens(t, hs.URL, string(run.Id), 1)
			postRun(t, hs.URL, string(run.Id), "/foreground")

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, wsURL(hs.URL, "/runs/"+string(run.Id)+"/events?last=20"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close(websocket.StatusNormalClosure, "")

			steer := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/steer", gen.SteerRequest{Message: "keep going"})
			if steer.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(steer.Body)
				t.Fatalf("steer %d %s", steer.StatusCode, body)
			}
			steer.Body.Close()
			bgRes, err := http.Post(hs.URL+"/runs/"+string(run.Id)+"/background", "application/json", bytes.NewReader(nil))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(bgRes.Body)
			bgRes.Body.Close()
			if bgRes.StatusCode != http.StatusOK && bgRes.StatusCode != http.StatusConflict {
				t.Fatalf("background %d %s", bgRes.StatusCode, body)
			}

			finished := false
			for ctx.Err() == nil {
				var ev gen.RunEvent
				if err := wsjson.Read(ctx, c, &ev); err != nil {
					break
				}
				if ev.Type == "status_changed" && ev.Message != nil && *ev.Message == "failed" {
					finished = true
					break
				}
			}
			if !finished {
				got := getRun(t, hs.URL, string(run.Id))
				if got.Status != gen.RunStatusFailed {
					t.Fatalf("demo %d: status %s", i, got.Status)
				}
			}
		})
	}
}

func TestMissingRunAndAgent(t *testing.T) {
	t.Parallel()
	hs := testServer(t)
	res, err := http.Get(hs.URL + "/runs/s_missing")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing run %d", res.StatusCode)
	}
	body := []byte(`{"agent_id":"a_nope","prompt":"x"}`)
	res, err = http.Post(hs.URL+"/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing agent %d", res.StatusCode)
	}
}

func followAndCount(ctx context.Context, hs *httptest.Server, id string, seen map[string]int, mu *sync.Mutex) error {
	lastSeen := ""
	backoff := 20 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return nil
		}
		c, _, err := websocket.Dial(ctx, wsURL(hs.URL, "/runs/"+id+"/events?last=50"), nil)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			continue
		}
		for {
			var ev gen.RunEvent
			if err := wsjson.Read(ctx, c, &ev); err != nil {
				_ = c.Close(websocket.StatusNormalClosure, "")
				break
			}
			n, _ := strconv.ParseInt(lastSeen, 10, 64)
			m, mErr := strconv.ParseInt(ev.Id, 10, 64)
			if lastSeen != "" && mErr == nil && m <= n {
				continue
			}
			lastSeen = ev.Id
			mu.Lock()
			seen[ev.Id]++
			mu.Unlock()
		}
	}
}

func uniqueCount(mu *sync.Mutex, seen map[string]int) int {
	mu.Lock()
	defer mu.Unlock()
	return len(seen)
}

func createRun(t *testing.T, base, prompt string) gen.Run {
	t.Helper()
	body, _ := json.Marshal(gen.CreateRunRequest{
		AgentId: gen.AgentID(server.DefaultAgentID),
		Prompt:  &prompt,
	})
	res, err := http.Post(base+"/runs", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, slurp)
	}
	var run gen.Run
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	return run
}

func getRun(t *testing.T, base, id string) gen.Run {
	t.Helper()
	res, err := http.Get(base + "/runs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get %d", res.StatusCode)
	}
	var run gen.Run
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	return run
}

func postRun(t *testing.T, base, id, suffix string) gen.Run {
	t.Helper()
	res, err := http.Post(base+"/runs/"+id+suffix, "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("%s %d %s", suffix, res.StatusCode, slurp)
	}
	var run gen.Run
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	return run
}

func postJSON(t *testing.T, url string, v any) *http.Response {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func replayEvents(t *testing.T, base, id string, last int) []gen.RunEvent {
	t.Helper()
	res, err := http.Get(base + "/runs/" + id + "/events?last=" + strconv.Itoa(last))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list gen.RunEventList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	return list.Items
}

func waitTokens(t *testing.T, base, id string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items := replayEvents(t, base, id, 50)
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

func wsURL(httpOrigin, path string) string {
	return strings.Replace(strings.TrimRight(httpOrigin, "/"), "http", "ws", 1) + path
}
