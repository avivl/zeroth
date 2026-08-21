// Command effects runs BA-6 gate G4 (Linear 42-8) and prints a
// markdown fragment for RESULTS.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/avivl/zeroth/zeroth-spike/harness"
)

func main() {
	runs := flag.Int("runs", 10, "number of 3-file-change attempts")
	timeout := flag.Duration("timeout", 20*time.Minute, "overall timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	got, err := harness.RunGateG4(ctx, *runs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "effects: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("## Structured effects (Linear 42-8, G4, Z1-052)")
	fmt.Println()
	fmt.Println("Plan-then-apply needs proposed effects, not in-place writes.")
	fmt.Printf("Source: **%s**. Prompt: `harness.ProposeEffectsPrompt`.\n", got.Source)
	fmt.Println("Task: 3-file change (README.md, greet.go, main.go).")
	fmt.Println()
	fmt.Println("| Runs | Parseable | 3-file set | Parser agent | Wrote files | Result |")
	fmt.Println("| ---: | ---: | ---: | ---: | ---: | --- |")
	status := "FAIL"
	if got.Pass {
		status = "PASS"
	} else if got.ParseOK == 0 && allBilling(got) {
		status = "not measured (billing)"
	}
	fmt.Printf("| %d | %d | %d | %d | %d | **%s** |\n",
		len(got.Runs), got.ParseOK, got.ThreeFileOK, got.ParserAgent, got.WroteFiles, status)
	fmt.Println()
	fmt.Println("Pass bar: 9/10 runs produce parseable effects (op, target,")
	fmt.Println("diff/payload) for the 3-file change.")
	fmt.Println()
	if len(got.Example) > 0 {
		fmt.Println("### Example effect set")
		fmt.Println()
		fmt.Println("```json")
		fmt.Println(harness.FormatExample(got.Example))
		fmt.Println("```")
		fmt.Println()
	}
	fmt.Println("Per-run:")
	for _, r := range got.Runs {
		note := "ok"
		if r.Err != "" {
			note = r.Err
		} else if !r.ThreeFileOK {
			note = "parsed but not 3-file"
		}
		if r.WroteFiles {
			note += " (wrote files)"
		}
		if r.ParserAgent {
			note += " (parser agent)"
		}
		fmt.Printf("- run %d source=%s parse=%v three=%v: %s\n",
			r.Index, r.Source, r.ParseOK, r.ThreeFileOK, note)
	}
	if !got.Pass {
		os.Exit(1)
	}
}

func allBilling(got harness.GateResult) bool {
	if len(got.Runs) == 0 {
		return false
	}
	for _, r := range got.Runs {
		if r.ParseOK || r.Err == "" {
			return false
		}
		if !strings.Contains(strings.ToLower(r.Err), "credit balance is too low") {
			return false
		}
	}
	return true
}
