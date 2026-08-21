package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/session"
)

// RoundTripResult is one export/import measurement.
type RoundTripResult struct {
	Ingest  time.Duration
	Export  time.Duration
	Restore time.Duration
	Hash    string
	Match   bool
	Overlay string
}

// RoundTrip starts from fixtureTar, hashes the workspace, exports,
// restores into a new sandbox, and checks the hash. The original
// instance is stopped before restore so disk is not held twice for L.
func RoundTrip(ctx context.Context, d Driver, fixtureTar string) (RoundTripResult, error) {
	var out RoundTripResult
	sid, err := session.ParseID("gate-roundtrip")
	if err != nil {
		return out, err
	}

	t0 := time.Now()
	inst, err := d.Start(ctx, StartRequest{
		SessionID: sid,
		Workspace: Workspace{TarPath: fixtureTar},
	})
	if err != nil {
		return out, fmt.Errorf("round trip start: %w", err)
	}
	out.Ingest = time.Since(t0)
	out.Overlay = OverlayMethod(inst)

	sum, err := HashWorkspace(inst)
	if err != nil {
		_ = inst.Stop(ctx)
		return out, err
	}
	out.Hash = HashHex(sum)

	exportPath := fixtureTar + ".export"
	f, err := os.Create(exportPath)
	if err != nil {
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("round trip export create: %w", err)
	}
	t1 := time.Now()
	err = inst.ExportTar(ctx, f)
	_ = f.Close()
	if err != nil {
		_ = inst.Stop(ctx)
		_ = os.Remove(exportPath)
		return out, fmt.Errorf("round trip export: %w", err)
	}
	out.Export = time.Since(t1)

	if err := inst.Stop(ctx); err != nil {
		_ = os.Remove(exportPath)
		return out, fmt.Errorf("round trip stop: %w", err)
	}

	t2 := time.Now()
	restored, err := d.Start(ctx, StartRequest{
		SessionID: sid,
		Workspace: Workspace{TarPath: exportPath},
	})
	if err != nil {
		_ = os.Remove(exportPath)
		return out, fmt.Errorf("round trip restore: %w", err)
	}
	out.Restore = time.Since(t2)
	got, err := HashWorkspace(restored)
	stopErr := restored.Stop(ctx)
	_ = os.Remove(exportPath)
	if err != nil {
		return out, err
	}
	if stopErr != nil {
		return out, fmt.Errorf("round trip restore stop: %w", stopErr)
	}
	out.Match = got == sum
	if !out.Match {
		return out, fmt.Errorf("round trip hash mismatch: started %s restored %s", HashHex(sum), HashHex(got))
	}
	return out, nil
}

// Percentile returns the (p*100)th percentile of a sorted copy of ds.
// p is in 0..1. The slice may be unsorted.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), ds...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(math.Round(p * float64(len(cp)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// KillResumeResult is G3: a mid-build kill, restore from the last
// export, and a count of lost workspace files.
type KillResumeResult struct {
	TickSeconds     int
	CheckpointAt    int
	KilledAt        int
	CheckpointFiles int
	RestoredFiles   int
	LostFiles       int
	ResumeClean     bool
	Overlay         string
}

// KillResume runs a ticking "build" that writes one file per second,
// exports at checkpointAt, kills at killedAt, restores, and counts
// files. Resume is clean when restored files equal checkpoint files
// and a new write succeeds.
func KillResume(ctx context.Context, d Driver, workspaceTar string, checkpointAt, killedAt int) (KillResumeResult, error) {
	out := KillResumeResult{TickSeconds: killedAt, CheckpointAt: checkpointAt, KilledAt: killedAt}
	sid, err := session.ParseID("gate-kill-resume")
	if err != nil {
		return out, err
	}
	inst, err := d.Start(ctx, StartRequest{SessionID: sid, Workspace: Workspace{TarPath: workspaceTar}})
	if err != nil {
		return out, fmt.Errorf("kill resume start: %w", err)
	}
	out.Overlay = OverlayMethod(inst)

	script := `n=0
while [ "$n" -lt "$1" ]; do
  n=$((n+1))
  echo "$n" > "/workspace/build/step-$n"
  echo "$n" > /workspace/build/progress
  sleep 1
done`
	if _, err := inst.Exec(ctx, []string{"mkdir", "-p", "/workspace/build"}); err != nil {
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("kill resume mkdir: %w", err)
	}

	buildCtx, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()
	errCh := make(chan error, 1)
	go func() {
		_, err := inst.Exec(buildCtx, []string{"sh", "-c", script, "build", fmt.Sprintf("%d", killedAt+60)})
		errCh <- err
	}()

	if err := waitForFile(ctx, inst, "/workspace/build/progress", checkpointAt, 2*time.Duration(checkpointAt+5)*time.Second); err != nil {
		cancelBuild()
		_ = inst.Kill(ctx)
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("kill resume wait checkpoint: %w", err)
	}

	tmp, err := os.CreateTemp("", "zeroth-spike-ckpt-*.tar")
	if err != nil {
		cancelBuild()
		_ = inst.Stop(ctx)
		return out, err
	}
	ckptPath := tmp.Name()
	if err := inst.ExportTar(ctx, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(ckptPath)
		cancelBuild()
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("kill resume export: %w", err)
	}
	_ = tmp.Close()

	listed, err := inst.Exec(ctx, []string{"sh", "-c", "ls /workspace/build/step-* | wc -l"})
	if err != nil {
		_ = os.Remove(ckptPath)
		cancelBuild()
		_ = inst.Stop(ctx)
		return out, err
	}
	fmt.Sscanf(listed.Stdout, "%d", &out.CheckpointFiles)

	if err := waitForFile(ctx, inst, "/workspace/build/progress", killedAt, 2*time.Duration(killedAt-checkpointAt+5)*time.Second); err != nil {
		_ = os.Remove(ckptPath)
		cancelBuild()
		_ = inst.Kill(ctx)
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("kill resume wait kill: %w", err)
	}
	if err := inst.Kill(ctx); err != nil {
		_ = os.Remove(ckptPath)
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("kill resume kill: %w", err)
	}
	cancelBuild()
	<-errCh
	if err := inst.Stop(ctx); err != nil {
		_ = os.Remove(ckptPath)
		return out, fmt.Errorf("kill resume stop: %w", err)
	}

	restored, err := d.Start(ctx, StartRequest{SessionID: sid, Workspace: Workspace{TarPath: ckptPath}})
	_ = os.Remove(ckptPath)
	if err != nil {
		return out, fmt.Errorf("kill resume restore: %w", err)
	}
	defer restored.Stop(ctx)

	listed, err = restored.Exec(ctx, []string{"sh", "-c", "ls /workspace/build/step-* 2>/dev/null | wc -l"})
	if err != nil {
		return out, err
	}
	fmt.Sscanf(listed.Stdout, "%d", &out.RestoredFiles)
	out.LostFiles = killedAt - out.RestoredFiles
	if out.LostFiles < 0 {
		out.LostFiles = 0
	}

	write, err := restored.Exec(ctx, []string{"sh", "-c", "echo resumed > /workspace/build/resumed && cat /workspace/build/resumed"})
	if err != nil {
		return out, err
	}
	out.ResumeClean = out.RestoredFiles == out.CheckpointFiles && write.ExitCode == 0 && bytes.Contains([]byte(write.Stdout), []byte("resumed"))
	if !out.ResumeClean {
		return out, fmt.Errorf("kill resume not clean: checkpoint=%d restored=%d write=%q exit=%d", out.CheckpointFiles, out.RestoredFiles, write.Stdout, write.ExitCode)
	}
	return out, nil
}

// DaemonRestoreResult is G3's in-sandbox daemon case.
type DaemonRestoreResult struct {
	AliveBeforeKill   bool
	AliveAfterRestore bool
	LogRestored       bool
	Overlay           string
	LimitationHolds   bool
	Notes             string
}

// DaemonRestore starts a background writer (a stand-in for a dev
// server), checkpoints, kills, restores, and records that the process
// is gone while its workspace files remain.
func DaemonRestore(ctx context.Context, d Driver, workspaceTar string) (DaemonRestoreResult, error) {
	var out DaemonRestoreResult
	sid, err := session.ParseID("gate-daemon")
	if err != nil {
		return out, err
	}
	inst, err := d.Start(ctx, StartRequest{SessionID: sid, Workspace: Workspace{TarPath: workspaceTar}})
	if err != nil {
		return out, fmt.Errorf("daemon restore start: %w", err)
	}
	out.Overlay = OverlayMethod(inst)

	daemon := `: > /workspace/devserver.log
while true; do
  echo alive >> /workspace/devserver.log
  sleep 1
done`
	go func() {
		_, _ = inst.Exec(ctx, []string{"sh", "-c", daemon})
	}()
	if err := waitUntilNonempty(ctx, inst, "/workspace/devserver.log", 15*time.Second); err != nil {
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("daemon restore wait log: %w", err)
	}
	out.AliveBeforeKill = logGrowing(ctx, inst)
	if !out.AliveBeforeKill {
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("daemon restore: log did not grow before kill")
	}

	tmp, err := os.CreateTemp("", "zeroth-spike-daemon-*.tar")
	if err != nil {
		_ = inst.Stop(ctx)
		return out, err
	}
	ckpt := tmp.Name()
	if err := inst.ExportTar(ctx, tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(ckpt)
		_ = inst.Stop(ctx)
		return out, fmt.Errorf("daemon restore export: %w", err)
	}
	_ = tmp.Close()
	if err := inst.Kill(ctx); err != nil {
		_ = os.Remove(ckpt)
		_ = inst.Stop(ctx)
		return out, err
	}
	_ = inst.Stop(ctx)

	restored, err := d.Start(ctx, StartRequest{SessionID: sid, Workspace: Workspace{TarPath: ckpt}})
	_ = os.Remove(ckpt)
	if err != nil {
		return out, fmt.Errorf("daemon restore restore: %w", err)
	}
	defer restored.Stop(ctx)

	logCheck, err := restored.Exec(ctx, []string{"sh", "-c", "test -s /workspace/devserver.log && echo restored"})
	if err != nil {
		return out, err
	}
	out.LogRestored = logCheck.ExitCode == 0
	out.AliveAfterRestore = logGrowing(ctx, restored)
	out.LimitationHolds = out.AliveBeforeKill && out.LogRestored && !out.AliveAfterRestore
	out.Notes = "Checkpoint is a workspace tar plus transcript, not a frozen process. The in-sandbox daemon's log restores; the process does not. A pid file would also restore, but that number is a different process in the new pid namespace. Resume must start the server again."
	if !out.LimitationHolds {
		return out, fmt.Errorf("daemon restore limitation did not hold: before=%v after=%v log=%v", out.AliveBeforeKill, out.AliveAfterRestore, out.LogRestored)
	}
	return out, nil
}

func waitForFile(ctx context.Context, inst Instance, path string, min int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := inst.Exec(ctx, []string{"sh", "-c", fmt.Sprintf("test -f '%s' && cat '%s' || true", path, path)})
		if err != nil {
			return err
		}
		var n int
		fmt.Sscanf(res.Stdout, "%d", &n)
		if n >= min {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s >= %d", path, min)
}

func waitUntilNonempty(ctx context.Context, inst Instance, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		res, err := inst.Exec(ctx, []string{"sh", "-c", fmt.Sprintf("test -s '%s' && echo yes || true", path)})
		if err != nil {
			return err
		}
		if bytes.Contains([]byte(res.Stdout), []byte("yes")) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for nonempty %s", path)
}

func logBytes(ctx context.Context, inst Instance) (int, error) {
	res, err := inst.Exec(ctx, []string{"sh", "-c", "wc -c < /workspace/devserver.log"})
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(res.Stdout, "%d", &n)
	return n, nil
}

func logGrowing(ctx context.Context, inst Instance) bool {
	a, err := logBytes(ctx, inst)
	if err != nil {
		return false
	}
	time.Sleep(2 * time.Second)
	b, err := logBytes(ctx, inst)
	if err != nil {
		return false
	}
	return b > a
}

// AsyncExportResult records that ExportTar overlapped an Exec without
// stretching the turn by the export duration.
type AsyncExportResult struct {
	Exec    time.Duration
	Export  time.Duration
	Blocked bool
	Overlay string
}

// AsyncExport runs Exec(sleep) in parallel with ExportTar. Blocked is
// true when exec wall time is close to sleep+export instead of sleep.
func AsyncExport(ctx context.Context, d Driver, workspaceTar string, sleep time.Duration) (AsyncExportResult, error) {
	var out AsyncExportResult
	sid, err := session.ParseID("gate-async")
	if err != nil {
		return out, err
	}
	inst, err := d.Start(ctx, StartRequest{SessionID: sid, Workspace: Workspace{TarPath: workspaceTar}})
	if err != nil {
		return out, err
	}
	defer inst.Stop(ctx)
	out.Overlay = OverlayMethod(inst)

	if _, err := inst.Exec(ctx, []string{"dd", "if=/dev/zero", "of=/workspace/.pad", "bs=1M", "count=80", "status=none"}); err != nil {
		return out, fmt.Errorf("async pad: %w", err)
	}

	exportPath := filepath.Join(os.TempDir(), "zeroth-spike-async.tar")
	f, err := os.Create(exportPath)
	if err != nil {
		return out, err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(exportPath)
	}()

	sec := int(sleep.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}

	start := time.Now()
	errCh := make(chan error, 1)
	go func() {
		_, err := inst.Exec(ctx, []string{"sleep", fmt.Sprintf("%d", sec)})
		errCh <- err
	}()
	exportStart := time.Now()
	err = inst.ExportTar(ctx, f)
	out.Export = time.Since(exportStart)
	if err != nil {
		return out, fmt.Errorf("async export: %w", err)
	}
	if err := <-errCh; err != nil {
		return out, fmt.Errorf("async exec: %w", err)
	}
	out.Exec = time.Since(start)
	// If export blocked the turn, exec wall time would be about
	// sleep+export. Allow 50% of export as slack on top of sleep.
	limit := sleep + out.Export/2 + 500*time.Millisecond
	out.Blocked = out.Exec > limit
	if out.Blocked {
		return out, fmt.Errorf("async export blocked turn: exec=%s sleep=%s export=%s", out.Exec, sleep, out.Export)
	}
	return out, nil
}
