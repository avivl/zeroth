package server_test

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func TestCheckpointKillRestoreMatchesFilesystem(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	sbx := newFakeSandbox()
	ckDir := t.TempDir()
	srv, err := server.New(server.Config{
		Store:         st,
		Sandbox:       sbx,
		CheckpointDir: ckDir,
		TokenInterval: time.Hour,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "checkpoint fidelity")
	orig := sbx.anyID()
	if orig.IsZero() {
		t.Fatal("create-run did not spawn a sandbox")
	}
	overlay, err := sbx.HostWorkspace(orig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(overlay, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "kept.txt"), []byte("checkpoint-payload-v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlay, "src", "hello.txt"), []byte("nested-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := overlayExportFiles(t, overlay)

	ckRes := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/checkpoints", gen.CreateCheckpointRequest{Label: ptrLabel("mid-run")})
	defer ckRes.Body.Close()
	if ckRes.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(ckRes.Body)
		t.Fatalf("checkpoint %d %s", ckRes.StatusCode, slurp)
	}
	var created gen.Checkpoint
	if err := json.NewDecoder(ckRes.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	cid, err := store.ParseCheckpointID(string(created.Id))
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.GetCheckpoint(t.Context(), cid)
	if err != nil {
		t.Fatal(err)
	}
	if row.Location == "" || row.Location == row.ID.String() {
		t.Fatalf("checkpoint location is still a stub: %q", row.Location)
	}
	if _, err := os.Stat(row.Location); err != nil {
		t.Fatalf("checkpoint archive missing: %v", err)
	}
	archived := tarArchiveFiles(t, row.Location)
	if !maps.Equal(archived, want) {
		t.Fatalf("archive files %v, want overlay %v", archived, want)
	}
	exports, imports := sbx.tarCalls()
	if exports < 1 {
		t.Fatal("ExportTar was not called")
	}
	if imports != 0 {
		t.Fatalf("ImportTar before restore = %d", imports)
	}

	if err := os.WriteFile(filepath.Join(overlay, "lost.txt"), []byte("should-not-survive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sbx.Kill(t.Context(), orig); err != nil {
		t.Fatal(err)
	}
	if err := sbx.Stop(t.Context(), orig); err != nil {
		t.Fatal(err)
	}

	restRes := postJSON(t, hs.URL+"/checkpoints/"+string(created.Id)+"/restore", struct{}{})
	defer restRes.Body.Close()
	if restRes.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(restRes.Body)
		t.Fatalf("restore %d %s", restRes.StatusCode, slurp)
	}
	var forked gen.Run
	if err := json.NewDecoder(restRes.Body).Decode(&forked); err != nil {
		t.Fatal(err)
	}
	if forked.Id == run.Id {
		t.Fatal("restore reused the original run id")
	}
	restoredID := sbx.anyID()
	if restoredID.IsZero() || restoredID == orig {
		t.Fatal("restore did not spawn a fresh sandbox")
	}
	restoredDir, err := sbx.HostWorkspace(restoredID)
	if err != nil {
		t.Fatal(err)
	}
	got := overlayExportFiles(t, restoredDir)
	if !maps.Equal(got, want) {
		t.Fatalf("restored overlay %v, want checkpoint %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(restoredDir, "lost.txt")); err == nil {
		t.Fatal("post-checkpoint write survived restore")
	}
	_, imports = sbx.tarCalls()
	if imports < 1 {
		t.Fatal("ImportTar was not called")
	}
	if sbx.liveCount() != 1 {
		t.Fatalf("live sandboxes = %d, want 1 (killed original, restored fork)", sbx.liveCount())
	}
}

func TestCreateCheckpointFailsWithoutSandbox(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{
		Store:         st,
		TokenInterval: time.Hour,
		TokenCount:    1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	run := createRun(t, hs.URL, "no sandbox")
	ckRes := postJSON(t, hs.URL+"/runs/"+string(run.Id)+"/checkpoints", gen.CreateCheckpointRequest{})
	defer ckRes.Body.Close()
	if ckRes.StatusCode == http.StatusCreated {
		t.Fatal("checkpoint succeeded without a sandbox; that is the silent-success stub")
	}
	page, err := st.ListCheckpoints(t.Context(), store.CheckpointQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("store wrote %d checkpoint rows on failure", len(page.Items))
	}
}

func (f *fakeSandbox) tarCalls() (exports, imports int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exports, f.imports
}

func overlayExportFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if sandbox.ExcludedFromExport(rel) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func tarArchiveFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != 0 {
			continue
		}
		name := filepath.ToSlash(hdr.Name)
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(body)
	}
}

func ptrLabel(s string) *string { return &s }
