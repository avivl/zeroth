package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/server"
	"github.com/avivl/zeroth/internal/store"
	"github.com/avivl/zeroth/internal/store/sqlite"
	gen "github.com/avivl/zeroth/pkg/api/gen/go"
)

func memoryHTTP(t *testing.T) (base string, st store.Store) {
	t.Helper()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := server.New(server.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs.URL, st
}

func TestMemoryWriteListAndProposalQueue(t *testing.T) {
	t.Parallel()
	base, st := memoryHTTP(t)

	body := `{"kind":"operator","content":"key: style.tests\n\nprefer table tests"}`
	res, err := http.Post(base+"/memory", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		slurp, _ := io.ReadAll(res.Body)
		t.Fatalf("create %d %s", res.StatusCode, slurp)
	}
	var created gen.MemoryEntry
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Content != "prefer table tests" || created.Kind != gen.MemoryKindOperator {
		t.Fatalf("created %+v", created)
	}

	list, err := http.Get(base + "/memory?kind=operator")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Body.Close()
	var page gen.MemoryList
	if err := json.NewDecoder(list.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Content != "prefer table tests" {
		t.Fatalf("list %+v", page.Items)
	}

	nb, err := memory.NewNotebook(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := nb.Propose(t.Context(), memory.AgentActor("claude"), memory.KindOperator, "", "kernel.rule", "deny by default", "run", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := http.Get(base + "/memory/proposals?status=pending")
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Body.Close()
	var props gen.MemoryProposalList
	if err := json.NewDecoder(pending.Body).Decode(&props); err != nil {
		t.Fatal(err)
	}
	if len(props.Items) != 1 || string(props.Items[0].Id) != p.ID.String() {
		t.Fatalf("proposals %+v", props.Items)
	}

	acc, err := http.Post(base+"/memory/proposals/"+p.ID.String()+"/accept", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Body.Close()
	if acc.StatusCode != http.StatusOK {
		slurp, _ := io.ReadAll(acc.Body)
		t.Fatalf("accept %d %s", acc.StatusCode, slurp)
	}
	var accepted gen.MemoryProposal
	if err := json.NewDecoder(acc.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Status != gen.MemoryProposalStatusAccepted || accepted.MemoryId == nil {
		t.Fatalf("accepted %+v", accepted)
	}

	again, err := http.Post(base+"/memory/proposals/"+p.ID.String()+"/accept", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("second accept %d", again.StatusCode)
	}

	p2, err := nb.Propose(t.Context(), memory.AgentActor("claude"), memory.KindOperator, "", "noise", "nope", "", store.SessionID{})
	if err != nil {
		t.Fatal(err)
	}
	rej, err := http.Post(base+"/memory/proposals/"+p2.ID.String()+"/reject", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rej.Body.Close()
	if rej.StatusCode != http.StatusOK {
		t.Fatalf("reject %d", rej.StatusCode)
	}

	listed, err := http.Get(base + "/memory?kind=operator")
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Body.Close()
	var after gen.MemoryList
	if err := json.NewDecoder(listed.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if len(after.Items) != 2 {
		t.Fatalf("want human write + accepted proposal, got %+v", after.Items)
	}
	auditRes, err := http.Get(base + "/audit?resource_type=memory")
	if err != nil {
		t.Fatal(err)
	}
	defer auditRes.Body.Close()
	var trail gen.AuditList
	if err := json.NewDecoder(auditRes.Body).Decode(&trail); err != nil {
		t.Fatal(err)
	}
	var sawAccept, sawReject bool
	for _, rec := range trail.Items {
		if rec.Action == "memory.propose.accept" {
			sawAccept = true
		}
		if rec.Action == "memory.propose.reject" {
			sawReject = true
		}
	}
	if !sawAccept || !sawReject {
		t.Fatalf("audit %+v", trail.Items)
	}
}

func TestCreateMemoryRequiresRefForSession(t *testing.T) {
	t.Parallel()
	base, _ := memoryHTTP(t)
	res, err := http.Post(base+"/memory", "application/json", strings.NewReader(`{"kind":"session","content":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}
