package spike_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	spike "github.com/avivl/zeroth/zeroth-spike"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(spike.NewMux(spike.ServerConfig{FixturesDir: t.TempDir()}))
	t.Cleanup(srv.Close)
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
	srv := httptest.NewServer(spike.NewMux(spike.ServerConfig{FixturesDir: t.TempDir()}))
	t.Cleanup(srv.Close)
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
	srv := httptest.NewServer(spike.NewMux(spike.ServerConfig{FixturesDir: dir}))
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
