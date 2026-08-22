package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/store/sqlite"
	"github.com/avivl/zeroth/internal/tracker"
)

type commentStub struct {
	issue    tracker.Issue
	comments []tracker.Comment
	getErr   error
	listErr  error
}

func (s *commentStub) Name() string { return "stub" }
func (s *commentStub) Capabilities() tracker.Capabilities {
	return tracker.Capabilities{}
}
func (s *commentStub) GetIssue(context.Context, string) (tracker.Issue, error) {
	if s.getErr != nil {
		return tracker.Issue{}, s.getErr
	}
	return s.issue, nil
}
func (s *commentStub) ListComments(context.Context, string) ([]tracker.Comment, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]tracker.Comment(nil), s.comments...), nil
}
func (s *commentStub) Comment(context.Context, string, string) (tracker.CommentRef, error) {
	return tracker.CommentRef{}, tracker.ErrInvalid
}
func (s *commentStub) SetState(context.Context, string, tracker.State) error {
	return tracker.ErrInvalid
}
func (s *commentStub) Assignments(context.Context) (<-chan tracker.AssignmentEvent, error) {
	return nil, tracker.ErrInvalid
}
func (s *commentStub) LinkArtifact(context.Context, string, tracker.Artifact) error {
	return tracker.ErrInvalid
}

func TestIssuePromptIncludesHumanCommentsAndMemory(t *testing.T) {
	t.Parallel()
	decision := "put the file at docs/linear-setup.md"
	iss := tracker.Issue{Key: "42-43", Title: "document setup", Description: "write the walkthrough"}
	comments := []tracker.Comment{
		{Body: decision, Author: "alice", At: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)},
		{Body: tracker.FormatFailedComment("s_1", "no plan"), Author: "alice"},
	}
	facts := []memory.Fact{{
		Kind: memory.KindOperator,
		Key:  "comment.42-1",
		Body: "prefer table tests",
	}}
	got := issuePrompt("42-43", iss, comments, facts)
	if !strings.Contains(got, "Linear 42-43: document setup") {
		t.Fatalf("title: %s", got)
	}
	if !strings.Contains(got, "write the walkthrough") {
		t.Fatalf("description: %s", got)
	}
	if !strings.Contains(got, decision) || !strings.Contains(got, "## Comment thread") {
		t.Fatalf("comment thread: %s", got)
	}
	if strings.Contains(got, "### Zeroth failed") {
		t.Fatalf("system comment leaked: %s", got)
	}
	if !strings.Contains(got, "## Project memory") || !strings.Contains(got, "prefer table tests") {
		t.Fatalf("project memory: %s", got)
	}
}

func TestCommentFactBodyJoinsOperatorThread(t *testing.T) {
	t.Parallel()
	body := commentFactBody("42-43", []tracker.Comment{
		{Author: "alice", Body: "use docs/linear-setup.md"},
		{Author: "", Body: "not docs/operator/"},
	})
	if !strings.Contains(body, "Linear 42-43") {
		t.Fatalf("issue key: %s", body)
	}
	if !strings.Contains(body, "[alice]") || !strings.Contains(body, "[operator]") {
		t.Fatalf("authors: %s", body)
	}
	if !strings.Contains(body, "use docs/linear-setup.md") || !strings.Contains(body, "not docs/operator/") {
		t.Fatalf("bodies: %s", body)
	}
}

func TestIssuePromptFallbacksAndDroppedFacts(t *testing.T) {
	t.Parallel()
	got := issuePrompt("42-55", tracker.Issue{}, []tracker.Comment{
		{Body: "keep it flat", Author: ""},
	}, []memory.Fact{
		{Key: "gone", Body: "deleted", Deleted: true},
		{Key: "empty", Body: "  "},
		{Key: "keep", Body: "prefer table tests\n"},
	})
	if !strings.Contains(got, "Linear 42-55: 42-55") {
		t.Fatalf("empty title: %s", got)
	}
	if !strings.Contains(got, "**operator**:") {
		t.Fatalf("empty author: %s", got)
	}
	if strings.Contains(got, "deleted") || strings.Contains(got, "### `empty`") {
		t.Fatalf("dropped facts leaked: %s", got)
	}
	if !strings.Contains(got, "prefer table tests") {
		t.Fatalf("live fact missing: %s", got)
	}
}

func TestLoadIssueThreadFetchesAndSurvivesListError(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	tr := &commentStub{
		issue:    tracker.Issue{Key: "42-43", Title: "from get"},
		comments: []tracker.Comment{{Body: "use docs/linear-setup.md", Author: "alice"}},
	}
	srv, err := New(Config{Store: st, Tracker: tr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	iss, comments := srv.loadIssueThread(t.Context(), "42-43", tracker.Issue{})
	if iss.Title != "from get" {
		t.Fatalf("issue %+v", iss)
	}
	if len(comments) != 1 || comments[0].Author != "alice" {
		t.Fatalf("comments %+v", comments)
	}

	tr.getErr = tracker.ErrNotFound
	iss, comments = srv.loadIssueThread(t.Context(), "42-43", tracker.Issue{})
	if iss.Title != "" {
		t.Fatalf("get error should keep empty issue: %+v", iss)
	}
	if len(comments) != 1 {
		t.Fatalf("list still runs after get miss: %+v", comments)
	}

	tr.getErr = nil
	tr.listErr = tracker.ErrUnavailable
	iss, comments = srv.loadIssueThread(t.Context(), "42-43", tracker.Issue{Key: "42-43", Title: "seeded"})
	if iss.Title != "seeded" {
		t.Fatalf("seeded issue lost: %+v", iss)
	}
	if comments != nil {
		t.Fatalf("list error should drop comments: %+v", comments)
	}
}

func TestIngestOperatorCommentsIsIdempotentHumanWrite(t *testing.T) {
	t.Parallel()
	st, err := sqlite.New(filepath.Join(t.TempDir(), "zeroth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv, err := New(Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	srv.ingestOperatorComments(t.Context(), "42-43", nil)
	comments := []tracker.Comment{{
		Body:   "put it at docs/linear-setup.md",
		Author: "",
		URL:    "https://linear.app/comment/cmt_1",
	}}
	srv.ingestOperatorComments(t.Context(), "42-43", comments)
	srv.ingestOperatorComments(t.Context(), "42-43", comments)

	nb := srv.notebook()
	fact, err := nb.Get(t.Context(), memory.KindOperator, "", commentFactKey("42-43"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fact.Body, "docs/linear-setup.md") {
		t.Fatalf("body %s", fact.Body)
	}
	if fact.Provenance.Who.Name != "operator" {
		t.Fatalf("author %+v", fact.Provenance)
	}
	if fact.Provenance.Source != "https://linear.app/comment/cmt_1" {
		t.Fatalf("source %+v", fact.Provenance)
	}
	if len(fact.Versions) != 1 {
		t.Fatalf("second ingest must not rewrite identical body: %+v", fact.Versions)
	}

	facts := srv.sessionFacts(t.Context(), "s_new", DefaultAgentID)
	found := false
	for _, f := range facts {
		if f.Key == commentFactKey("42-43") && strings.Contains(f.Body, "docs/linear-setup.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("session facts missing ingested comment: %+v", facts)
	}
}
