package bench_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/bench"
)

func TestG1G6Smoke(t *testing.T) {
	report, err := bench.Run(context.Background(), bench.Config{
		Dir:           t.TempDir(),
		Warmup:        2,
		Samples:       8,
		Sessions:      5,
		TokenInterval: 5 * time.Millisecond,
		SkipCold:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.G1Warm.N != 8 {
		t.Fatalf("sample count warm=%d", report.G1Warm.N)
	}
	if !report.G1Pass {
		t.Fatalf("G1 warm p99=%s max=%s, want < 2s", report.G1Warm.P99, report.G1Warm.Max)
	}
	if !report.G6Pass {
		t.Fatalf("G6 unbatched max=%s, want <= 50ms", report.G6Unbatched.Max)
	}
	p := report.G1Warm
	if p.P50 > p.P95 || p.P95 > p.P99 || p.P99 > p.Max {
		t.Fatalf("percentiles out of order: %+v", p)
	}
	md := bench.Markdown(report)
	if !strings.Contains(md, "G1 Attach (warm)") || !strings.Contains(md, "G6 write stall") {
		t.Fatalf("markdown missing gates:\n%s", md)
	}
}

func TestG1ColdOnce(t *testing.T) {
	report, err := bench.Run(context.Background(), bench.Config{
		Dir:           t.TempDir(),
		Warmup:        1,
		Samples:       1,
		Sessions:      5,
		TokenInterval: 5 * time.Millisecond,
		ColdWarmup:    1,
		ColdSamples:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.G1Cold.N != 1 {
		t.Fatalf("cold n=%d", report.G1Cold.N)
	}
	if report.G1Cold.Max > 2*time.Second {
		t.Fatalf("G1 cold max=%s", report.G1Cold.Max)
	}
}
