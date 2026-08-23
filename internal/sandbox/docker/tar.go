package docker

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/avivl/zeroth/internal/sandbox"
	"github.com/avivl/zeroth/internal/secretscan"
)

// ExportTar implements [sandbox.Driver].
func (d *Driver) ExportTar(ctx context.Context, id sandbox.ID, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	inst.mu.Lock()
	stopped := inst.stopped
	dir := inst.workspace
	inst.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker export: %w", sandbox.ErrStopped)
	}
	// Host-side tar of the workspace. Exec is docker exec, so this
	// does not take a turn lock. Excluded credential and compiled
	// memory paths are omitted; the packed tar is scanned before any
	// bytes reach w.
	if err := exportWorkspace(ctx, dir, w); err != nil {
		return fmt.Errorf("sandbox docker export: %w", err)
	}
	return nil
}

// ImportTar implements [sandbox.Driver].
func (d *Driver) ImportTar(ctx context.Context, id sandbox.ID, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	inst, err := d.lookup(id)
	if err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	inst.mu.Lock()
	stopped := inst.stopped
	dir := inst.workspace
	inst.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox docker import: %w", sandbox.ErrStopped)
	}
	if err := sandbox.ReplaceOverlay(dir, r); err != nil {
		return fmt.Errorf("sandbox docker import: %w", err)
	}
	return nil
}

func exportWorkspace(ctx context.Context, dir string, w io.Writer) error {
	tmp, err := os.CreateTemp("", "zeroth-export-*.tar")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := sandbox.PackOverlay(ctx, dir, tmp); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}
	findings, err := secretscan.ScanTar(tmp)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return fmt.Errorf("%w (%s:%s)", sandbox.ErrSecret, findings[0].Path, findings[0].Rule)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}
	if _, err := io.Copy(w, tmp); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
