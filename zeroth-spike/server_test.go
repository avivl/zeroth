package spike_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	spike "github.com/avivl/zeroth/zeroth-spike"
	"github.com/avivl/zeroth/zeroth-spike/eventlog"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	res, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" {
		t.Fatalf("status = %q", body.Status)
	}
}

func TestAuthDoesNotEchoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-not-a-real-key")
	srv, _ := testServer(t)
	res, err := http.Get(srv.URL + "/auth")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	if got := string(raw); strings.Contains(got, "test-not-a-real-key") {
		t.Fatal("auth response leaked API key")
	}
	var configured bool
	if err := json.Unmarshal(body["api_key_configured"], &configured); err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("expected api_key_configured true")
	}
}

func TestFixturesListsSizes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "S.tar"), []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := spike.NewServer(spike.ServerConfig{
		FixturesDir: dir,
		DBPath:      filepath.Join(t.TempDir(), "events.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/fixtures")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Items []struct {
			Name string `json:"name"`
			Size int64  `json:"size_bytes"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(body.Items))
	}
	if body.Items[0].Name != "S.tar" || body.Items[0].Size != 4 {
		t.Fatalf("S.tar row = %+v", body.Items[0])
	}
	if body.Items[1].Size != 0 || body.Items[2].Size != 0 {
		t.Fatalf("missing tars should report 0, got M=%d L=%d", body.Items[1].Size, body.Items[2].Size)
	}
}

func TestCreateReplayAndLiveTail(t *testing.T) {
	t.Parallel()
	hs, _ := testServer(t)
	id := createFakeSession(t, hs.URL, 5)

	res, err := http.Get(hs.URL + "/sessions/" + id + "/events?last=10")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("replay status = %d", res.StatusCode)
	}
	var body struct {
		Items []spike.Frame `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) == 0 {
		t.Fatal("expected replay items from SQLite")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/sessions/" + id + "/events?last=5"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	caught := false
	live := 0
	for live < 1 {
		var f spike.Frame
		if err := wsjson.Read(ctx, c, &f); err != nil {
			t.Fatalf("ws read: %v", err)
		}
		if f.Type == spike.FrameCaughtUp {
			caught = true
			continue
		}
		if caught && f.Type == eventlog.TypeToken && !f.Replay {
			live++
		}
	}
	if !caught {
		t.Fatal("expected caught_up before live tail")
	}
}

func TestBackgroundEndpoint(t *testing.T) {
	t.Parallel()
	hs, _ := testServer(t)
	id := createFakeSession(t, hs.URL, 20)
	res, err := http.Post(hs.URL+"/sessions/"+id+"/bg", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("bg status = %d", res.StatusCode)
	}
	var body struct {
		Foreground bool `json:"foreground"`
		Running    bool `json:"running"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Foreground || !body.Running {
		t.Fatalf("bg body = %+v", body)
	}
}

func testServer(t *testing.T) (*httptest.Server, *spike.Server) {
	t.Helper()
	s, err := spike.NewServer(spike.ServerConfig{
		FixturesDir: t.TempDir(),
		DBPath:      filepath.Join(t.TempDir(), "events.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(s)
	t.Cleanup(func() {
		hs.Close()
		_ = s.Close()
	})
	return hs, s
}

func createFakeSession(t *testing.T, base string, intervalMs int) string {
	t.Helper()
	body := []byte(`{"agent":"fake","interval_ms":` + strconv.Itoa(intervalMs) + `}`)
	res, err := http.Post(base+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", res.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" {
		t.Fatal("empty session id")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/sessions/" + out.ID + "/events?last=20")
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Items []spike.Frame `json:"items"`
		}
		_ = json.NewDecoder(res.Body).Decode(&payload)
		res.Body.Close()
		for _, f := range payload.Items {
			if f.Type == eventlog.TypeToken {
				return out.ID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session produced no tokens")
	return out.ID
}
