package server

import (
	"context"
	"fmt"

	"github.com/avivl/zeroth/internal/memory"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/store"
)

// spawnHydratedSandbox creates a sandbox and compiles the notebook slice
// into AGENTS.md (and companion harness files) before the harness starts
// (Z1-118). A nil sandbox driver is a no-op so tests without Docker still
// run.
func (s *Server) spawnHydratedSandbox(ctx context.Context, sess store.Session) (sandbox.ID, error) {
	if s.sandbox == nil {
		return sandbox.ID{}, nil
	}
	handle, err := s.sandbox.Spawn(ctx, sandbox.Spec{})
	if err != nil {
		return sandbox.ID{}, fmt.Errorf("server spawn sandbox: %w", err)
	}
	if err := s.hydrateSandbox(ctx, handle.ID, sess); err != nil {
		_ = s.sandbox.Stop(context.Background(), handle.ID)
		return sandbox.ID{}, err
	}
	return handle.ID, nil
}

func (s *Server) hydrateSandbox(ctx context.Context, id sandbox.ID, sess store.Session) error {
	nb := s.notebook()
	if nb == nil {
		return fmt.Errorf("server hydrate sandbox: notebook unavailable")
	}
	all, err := nb.Slice(ctx, "", "")
	if err != nil {
		return fmt.Errorf("server hydrate sandbox: %w", err)
	}
	facts := memory.ForSession(all, sess.ID.String(), sess.AgentID.String())
	if err := memory.HydrateSandbox(ctx, s.sandbox, id, facts); err != nil {
		return fmt.Errorf("server hydrate sandbox: %w", err)
	}
	return nil
}

func (s *Server) rememberSandbox(id string, sbx sandbox.ID) {
	if sbx.IsZero() {
		return
	}
	s.mu.Lock()
	s.sandboxes[id] = sbx
	s.mu.Unlock()
}
