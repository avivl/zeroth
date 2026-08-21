// Command egress runs BA-6 gate G5 (Linear 42-8) and prints a
// markdown fragment for RESULTS.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/sandbox"
)

func main() {
	warmup := flag.Int("warmup", 10, "warmup samples discarded")
	samples := flag.Int("samples", 110, "measured samples")
	timeout := flag.Duration("timeout", 2*time.Minute, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	got, err := sandbox.MeasureEgress(ctx, *warmup, *samples)
	if err != nil {
		fmt.Fprintf(os.Stderr, "egress: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("## Egress deny-by-default (Linear 42-8, G5, Z1-080 / Z2-111)")
	fmt.Println()
	fmt.Println("The sandbox allowlist is derived from active leases. Empty")
	fmt.Println("leases keep docker `--network none`. Per-destination allow is")
	fmt.Println("an HTTP/HTTPS CONNECT proxy. A destination that is not listed")
	fmt.Println("returns 403. Enforcement is the proxy: clients that ignore")
	fmt.Println("`HTTP_PROXY` are out of scope for stage 1.")
	fmt.Println()
	fmt.Println("Measured against a local httptest origin so the number is the")
	fmt.Println("proxy hop, not WAN noise. Re-run: `go run ./cmd/egress`.")
	fmt.Println()
	fmt.Println("| Check | Pass bar | Result |")
	fmt.Println("| --- | --- | --- |")
	fmt.Printf("| Allow listed destination | HTTP 200 through proxy | %s |\n", yn(got.AllowOK))
	fmt.Printf("| Deny unlisted destination | HTTP 403, upstream not reached | %s |\n", yn(got.DenyOK))
	fmt.Printf("| Proxy latency delta p50 | < 20 ms (n=%d) | %s (direct %s, proxy %s) |\n",
		got.Samples, fmtDur(got.DeltaP50), fmtDur(got.DirectP50), fmtDur(got.ProxyP50))
	fmt.Println()
	status := "FAIL"
	if got.Pass {
		status = "PASS"
	}
	fmt.Printf("G5: **%s**\n", status)
	if !got.Pass {
		os.Exit(1)
	}
}

func yn(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func fmtDur(d time.Duration) string {
	if d < time.Microsecond {
		return "0"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%d us", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%.3f ms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}
