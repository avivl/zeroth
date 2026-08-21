// Command gate runs BA-6 checkpoint gates G2 and G3 against the docker
// sandbox driver and prints a markdown fragment for RESULTS.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/sandbox"
)

func main() {
	fixtures := flag.String("fixtures", "./fixtures", "fixture tar directory")
	runs := flag.Int("runs", 10, "round-trips per fixture size")
	sizes := flag.String("sizes", "S,M,L", "comma-separated sizes to measure (empty skips G2 hydrate)")
	buildSec := flag.Int("build-sec", 300, "G3 simulated build length in seconds")
	flag.Parse()

	if err := sandbox.DockerReady(); err != nil {
		fmt.Fprintf(os.Stderr, "gate: docker not ready: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	d := sandbox.NewDocker()
	wanted := map[string]bool{}
	for _, s := range strings.Split(*sizes, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		wanted[s] = true
	}

	fmt.Println("## Checkpoint round-trip (Linear 42-7, Z1-036 / Z1-080)")
	fmt.Println()
	fmt.Println("A checkpoint is a workspace tar plus the session transcript,")
	fmt.Println("not a frozen process. Measured against `sandbox.Driver` docker")
	fmt.Println("with overlay workspace, ExportTar, ImportTar, Exec, and Kill.")
	fmt.Println()
	fmt.Println("### Hydration matrix")
	fmt.Println()
	fmt.Println("| Size | Runs | Overlay | Import p50 | Import p95 | Export p50 | Export p95 | Restore p50 | Restore p95 | Hash |")
	fmt.Println("| --- | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |")

	type row struct {
		size string
		p50r time.Duration
	}
	var restores []row

	for _, size := range []string{"S", "M", "L"} {
		if !wanted[size] {
			continue
		}
		tarPath := filepath.Join(*fixtures, size+".tar")
		if _, err := os.Stat(tarPath); err != nil {
			fmt.Fprintf(os.Stderr, "gate: skip %s: %v\n", size, err)
			fmt.Printf("| %s | 0 | | | | | | | | missing |\n", size)
			continue
		}
		var ingest, export, restore []time.Duration
		overlay := ""
		hash := ""
		for i := 0; i < *runs; i++ {
			fmt.Fprintf(os.Stderr, "gate: %s run %d/%d\n", size, i+1, *runs)
			got, err := sandbox.RoundTrip(ctx, d, tarPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gate: %s run %d: %v\n", size, i+1, err)
				os.Exit(1)
			}
			ingest = append(ingest, got.Ingest)
			export = append(export, got.Export)
			restore = append(restore, got.Restore)
			overlay = got.Overlay
			hash = got.Hash
		}
		fmt.Printf("| %s | %d | %s | %s | %s | %s | %s | %s | %s | `%s` |\n",
			size, *runs, overlay,
			fmtDur(sandbox.Percentile(ingest, 0.50)), fmtDur(sandbox.Percentile(ingest, 0.95)),
			fmtDur(sandbox.Percentile(export, 0.50)), fmtDur(sandbox.Percentile(export, 0.95)),
			fmtDur(sandbox.Percentile(restore, 0.50)), fmtDur(sandbox.Percentile(restore, 0.95)),
			hash[:12],
		)
		restores = append(restores, row{size: size, p50r: sandbox.Percentile(restore, 0.50)})
	}

	fmt.Println()
	fmt.Println("Byte-identity is the content hash of the overlay tree (paths,")
	fmt.Println("modes, file bytes). Tar mtimes are ignored. Workspace M pass")
	fmt.Println("bar: restore p50 < 10 s. L target: restore p50 < 60 s.")
	fmt.Println()
	for _, r := range restores {
		switch r.size {
		case "M":
			status := "PASS"
			if r.p50r >= 10*time.Second {
				status = "FAIL"
			}
			fmt.Printf("- M restore p50 %s: **%s**\n", fmtDur(r.p50r), status)
		case "L":
			status := "PASS (target)"
			if r.p50r >= 60*time.Second {
				status = "OVER TARGET"
			}
			fmt.Printf("- L restore p50 %s: **%s**\n", fmtDur(r.p50r), status)
		}
	}

	sTar := filepath.Join(*fixtures, "S.tar")
	if _, err := os.Stat(sTar); err == nil && wanted["S"] {
		fmt.Println()
		fmt.Println("### Async export")
		fmt.Println()
		fmt.Fprintf(os.Stderr, "gate: async export\n")
		async, err := sandbox.AsyncExport(ctx, d, sTar, 2*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gate: async export: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("ExportTar ran alongside Exec(sleep 2s). exec=%s export=%s blocked=%v overlay=%s.\n",
			fmtDur(async.Exec), fmtDur(async.Export), async.Blocked, async.Overlay)
		fmt.Println("Async export does not take a turn lock: the tar is a host-side")
		fmt.Println("read of the overlay, while Exec is `docker exec`.")
	}

	fmt.Println()
	fmt.Println("### G3 Kill and resume")
	fmt.Println()
	ckpt := *buildSec / 3
	if ckpt < 1 {
		ckpt = 1
	}
	killAt := *buildSec / 2
	if killAt <= ckpt {
		killAt = ckpt + 1
	}
	fmt.Fprintf(os.Stderr, "gate: kill-resume build=%ds checkpoint=%d kill=%d\n", *buildSec, ckpt, killAt)
	kr, err := sandbox.KillResume(ctx, d, sTar, ckpt, killAt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate: kill-resume: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Simulated build would have run %d s. Checkpoint at step %d, kill at step %d.\n", *buildSec, kr.CheckpointAt, kr.KilledAt)
	fmt.Printf("Files at export: **%d**. Files after restore: **%d**. Lost work: **%d** files (the ticks after the last export).\n",
		kr.CheckpointFiles, kr.RestoredFiles, kr.LostFiles)
	fmt.Printf("Resume clean: **%v** (restored tree matches the checkpoint, a new write succeeds).\n", kr.ResumeClean)

	fmt.Println()
	fmt.Println("### Daemon-on-restore")
	fmt.Println()
	fmt.Fprintf(os.Stderr, "gate: daemon-on-restore\n")
	dr, err := sandbox.DaemonRestore(ctx, d, sTar)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gate: daemon: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Alive before kill: **%v**. Workspace files restored: **%v**. Alive after restore: **%v**.\n",
		dr.AliveBeforeKill, dr.LogRestored, dr.AliveAfterRestore)
	fmt.Println()
	fmt.Println(dr.Notes)
}

func fmtDur(d time.Duration) string {
	if d < time.Millisecond {
		return d.String()
	}
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}
