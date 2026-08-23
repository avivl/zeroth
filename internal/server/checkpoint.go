package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avivl/zeroth/internal/audit"
	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/session"
	"github.com/avivl/zeroth/internal/store"
)

var (
	errCheckpointNoDriver  = fmt.Errorf("checkpoint: no sandbox driver")
	errCheckpointNoSandbox = fmt.Errorf("checkpoint: run has no sandbox")
)

func (s *Server) snapshotRun(ctx context.Context, id session.ID, label string) (store.Checkpoint, error) {
	if s.sandbox == nil {
		return store.Checkpoint{}, errCheckpointNoDriver
	}
	sbx := s.sandboxOf(id)
	if sbx.IsZero() {
		return store.Checkpoint{}, errCheckpointNoSandbox
	}
	cid, err := newCheckpointID()
	if err != nil {
		return store.Checkpoint{}, err
	}
	sid, err := store.ParseSessionID(id.String())
	if err != nil {
		return store.Checkpoint{}, err
	}
	loc, err := s.writeCheckpointTar(ctx, sbx, cid)
	if err != nil {
		return store.Checkpoint{}, err
	}
	now := time.Now().UTC()
	ck := store.Checkpoint{
		ID:        cid,
		SessionID: sid,
		Label:     label,
		Location:  loc,
		CreatedAt: now,
	}
	if err := s.store.CreateCheckpoint(ctx, ck); err != nil {
		_ = os.Remove(loc)
		return store.Checkpoint{}, fmt.Errorf("checkpoint: %w", err)
	}
	if err := s.sup.TakeCheckpoint(ctx, id, cid.String()); err != nil {
		return store.Checkpoint{}, fmt.Errorf("checkpoint event: %w", err)
	}
	sess, err := s.store.GetSession(ctx, sid)
	if err != nil {
		return store.Checkpoint{}, fmt.Errorf("checkpoint session: %w", err)
	}
	if _, err := s.audit.Append(ctx, audit.Entry{
		Action:       audit.ActionCheckpoint,
		Target:       cid.String(),
		Approver:     audit.ApproverOperator,
		AgentID:      sess.AgentID,
		SessionID:    sid,
		ResourceType: "checkpoint",
		ResourceID:   cid.String(),
	}); err != nil {
		return store.Checkpoint{}, fmt.Errorf("checkpoint %s: %w", audit.ActionCheckpoint, err)
	}
	return ck, nil
}

func (s *Server) writeCheckpointTar(ctx context.Context, sbx sandbox.ID, cid store.CheckpointID) (string, error) {
	if err := os.MkdirAll(s.checkpointDir, 0o755); err != nil {
		return "", fmt.Errorf("checkpoint dir: %w", err)
	}
	final := filepath.Join(s.checkpointDir, cid.String()+".tar")
	tmp := final + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("checkpoint archive: %w", err)
	}
	exportErr := s.sandbox.ExportTar(ctx, sbx, f)
	if exportErr == nil {
		exportErr = f.Sync()
	}
	closeErr := f.Close()
	if exportErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("checkpoint export: %w", exportErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("checkpoint archive: %w", closeErr)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("checkpoint archive: %w", err)
	}
	return final, nil
}

func (s *Server) restoreSandbox(ctx context.Context, sess store.Session, loc string) (sandbox.ID, error) {
	if s.sandbox == nil {
		return sandbox.ID{}, fmt.Errorf("server restore checkpoint: no sandbox driver")
	}
	f, err := os.Open(loc)
	if err != nil {
		return sandbox.ID{}, fmt.Errorf("server restore checkpoint: open archive: %w", err)
	}
	defer f.Close()
	handle, err := s.sandbox.Spawn(ctx, sandbox.Spec{})
	if err != nil {
		return sandbox.ID{}, fmt.Errorf("server restore checkpoint: %w", err)
	}
	if err := s.sandbox.ImportTar(ctx, handle.ID, f); err != nil {
		_ = s.sandbox.Stop(context.Background(), handle.ID)
		return sandbox.ID{}, fmt.Errorf("server restore checkpoint: import: %w", err)
	}
	if err := s.hydrateSandbox(ctx, handle.ID, sess); err != nil {
		_ = s.sandbox.Stop(context.Background(), handle.ID)
		return sandbox.ID{}, err
	}
	return handle.ID, nil
}

func (s *Server) sandboxOf(id session.ID) sandbox.ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sandboxes[id.String()]
}
