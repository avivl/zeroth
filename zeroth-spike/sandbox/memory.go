package sandbox

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/avivl/zeroth/zeroth-spike/session"
)

// Memory is a throwaway Driver that unpacks a workspace tar into a
// temp dir. It is not an isolation boundary. Use it to exercise the
// interface and fixture ingest without Docker.
type Memory struct {
	seq atomic.Uint64
}

// NewMemory returns a memory sandbox driver.
func NewMemory() *Memory { return &Memory{} }

// Name implements [Driver].
func (*Memory) Name() string { return "memory" }

var _ Driver = (*Memory)(nil)
var _ Instance = (*memoryInstance)(nil)

// Start unpacks req.Workspace.TarPath and returns an instance rooted
// at that tree.
func (d *Memory) Start(ctx context.Context, req StartRequest) (Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sandbox memory start: %w", err)
	}
	if req.SessionID.IsZero() {
		return nil, fmt.Errorf("sandbox memory start: empty session id")
	}
	if req.Workspace.TarPath == "" {
		return nil, fmt.Errorf("sandbox memory start: empty workspace tar")
	}

	dir, err := os.MkdirTemp("", "zeroth-spike-memory-")
	if err != nil {
		return nil, fmt.Errorf("sandbox memory start: temp dir: %w", err)
	}
	if err := unpackTar(req.Workspace.TarPath, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("sandbox memory start: %w", err)
	}

	n := d.seq.Add(1)
	hid, err := ParseHandleID(fmt.Sprintf("mem-%d", n))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("sandbox memory start: %w", err)
	}
	return &memoryInstance{
		id:        hid,
		sessionID: req.SessionID,
		dir:       dir,
	}, nil
}

type memoryInstance struct {
	id        HandleID
	sessionID session.ID
	dir       string

	mu      sync.Mutex
	killed  bool
	stopped bool
}

func (i *memoryInstance) ID() HandleID          { return i.id }
func (i *memoryInstance) SessionID() session.ID { return i.sessionID }

func (i *memoryInstance) Exec(ctx context.Context, argv []string) (ExecResult, error) {
	i.mu.Lock()
	stopped := i.stopped
	killed := i.killed
	dir := i.dir
	i.mu.Unlock()
	if stopped {
		return ExecResult{}, fmt.Errorf("sandbox memory exec: stopped")
	}
	if killed {
		return ExecResult{}, fmt.Errorf("sandbox memory exec: killed")
	}
	if len(argv) == 0 {
		return ExecResult{}, fmt.Errorf("sandbox memory exec: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	res := ExecResult{Stdout: string(out)}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return ExecResult{}, fmt.Errorf("sandbox memory exec: %w", err)
	}
	return res, nil
}

func (i *memoryInstance) ExportTar(ctx context.Context, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox memory export: %w", err)
	}
	i.mu.Lock()
	stopped := i.stopped
	dir := i.dir
	i.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox memory export: stopped")
	}
	if err := writeTar(ctx, dir, w); err != nil {
		return fmt.Errorf("sandbox memory export: %w", err)
	}
	return nil
}

func (i *memoryInstance) ImportTar(ctx context.Context, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox memory import: %w", err)
	}
	i.mu.Lock()
	stopped := i.stopped
	dir := i.dir
	i.mu.Unlock()
	if stopped {
		return fmt.Errorf("sandbox memory import: stopped")
	}
	if err := clearDir(dir); err != nil {
		return fmt.Errorf("sandbox memory import: %w", err)
	}
	if err := readTar(ctx, dir, r); err != nil {
		return fmt.Errorf("sandbox memory import: %w", err)
	}
	return nil
}

func (i *memoryInstance) Kill(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox memory kill: %w", err)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.stopped {
		return fmt.Errorf("sandbox memory kill: stopped")
	}
	i.killed = true
	return nil
}

func (i *memoryInstance) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox memory stop: %w", err)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.stopped {
		return nil
	}
	i.stopped = true
	if err := os.RemoveAll(i.dir); err != nil {
		return fmt.Errorf("sandbox memory stop: %w", err)
	}
	return nil
}

func unpackTar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent: %w", err)
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return fmt.Errorf("create file: %w", err)
			}
			if _, err := io.Copy(w, tr); err != nil {
				_ = w.Close()
				return fmt.Errorf("write file: %w", err)
			}
			if err := w.Close(); err != nil {
				return fmt.Errorf("close file: %w", err)
			}
		default:
			// Skip links and specials in this throwaway unpacker.
		}
	}
}

func safeJoin(dir, name string) (string, error) {
	dest := filepath.Clean(dir)
	full := filepath.Join(dest, filepath.Clean(name))
	rel, err := filepath.Rel(dest, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("tar path escapes dest: %q", name)
	}
	return full, nil
}
