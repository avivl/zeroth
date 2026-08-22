package tracker_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/internal/tracker"
)

type stubProvider struct {
	ch chan tracker.AssignmentEvent
}

func (s *stubProvider) Name() string { return "stub" }
func (s *stubProvider) Capabilities() tracker.Capabilities {
	return tracker.Capabilities{}
}
func (s *stubProvider) GetIssue(context.Context, string) (tracker.Issue, error) {
	return tracker.Issue{}, tracker.ErrNotFound
}
func (s *stubProvider) Comment(context.Context, string, string) (tracker.CommentRef, error) {
	return tracker.CommentRef{}, tracker.ErrInvalid
}
func (s *stubProvider) ListComments(context.Context, string) ([]tracker.IssueComment, error) {
	return nil, tracker.ErrNotFound
}
func (s *stubProvider) SetState(context.Context, string, tracker.State) error {
	return tracker.ErrInvalid
}
func (s *stubProvider) Assignments(context.Context) (<-chan tracker.AssignmentEvent, error) {
	return s.ch, nil
}
func (s *stubProvider) LinkArtifact(context.Context, string, tracker.Artifact) error {
	return tracker.ErrInvalid
}

type recordingHandler struct {
	assigned   []string
	unassigned []string
	err        error
}

func (h *recordingHandler) OnAssigned(_ context.Context, ev tracker.AssignmentEvent) error {
	h.assigned = append(h.assigned, ev.Key)
	return h.err
}
func (h *recordingHandler) OnUnassigned(_ context.Context, ev tracker.AssignmentEvent) error {
	h.unassigned = append(h.unassigned, ev.Key)
	return h.err
}

func TestWatchDispatchesAndStops(t *testing.T) {
	t.Parallel()
	ch := make(chan tracker.AssignmentEvent, 2)
	p := &stubProvider{ch: ch}
	h := &recordingHandler{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tracker.Watch(ctx, p, h) }()

	ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-1"}
	ch <- tracker.AssignmentEvent{Kind: tracker.Unassigned, Key: "42-1"}
	close(ch)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after channel close")
	}
	if len(h.assigned) != 1 || h.assigned[0] != "42-1" {
		t.Fatalf("assigned = %v", h.assigned)
	}
	if len(h.unassigned) != 1 || h.unassigned[0] != "42-1" {
		t.Fatalf("unassigned = %v", h.unassigned)
	}
}

func TestWatchNilInputs(t *testing.T) {
	t.Parallel()
	if err := tracker.Watch(t.Context(), nil, &recordingHandler{}); err == nil {
		t.Fatal("expected nil provider error")
	}
	if err := tracker.Watch(t.Context(), &stubProvider{}, nil); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestWatchHandlerError(t *testing.T) {
	t.Parallel()
	ch := make(chan tracker.AssignmentEvent, 1)
	p := &stubProvider{ch: ch}
	h := &recordingHandler{err: errors.New("boom")}
	done := make(chan error, 1)
	go func() { done <- tracker.Watch(t.Context(), p, h) }()
	ch <- tracker.AssignmentEvent{Kind: tracker.Assigned, Key: "42-2"}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "42-2") {
			t.Fatalf("Watch error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return handler error")
	}
}

func TestWatchUnknownKind(t *testing.T) {
	t.Parallel()
	ch := make(chan tracker.AssignmentEvent, 1)
	p := &stubProvider{ch: ch}
	h := &recordingHandler{}
	done := make(chan error, 1)
	go func() { done <- tracker.Watch(t.Context(), p, h) }()
	ch <- tracker.AssignmentEvent{Kind: "nope", Key: "42-3"}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "unknown kind") {
			t.Fatalf("Watch error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return unknown kind")
	}
}

func TestWatchCancel(t *testing.T) {
	t.Parallel()
	ch := make(chan tracker.AssignmentEvent)
	p := &stubProvider{ch: ch}
	h := &recordingHandler{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- tracker.Watch(ctx, p, h) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Watch = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return on cancel")
	}
}
