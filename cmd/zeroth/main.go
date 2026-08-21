// Command zeroth is the CLI and headless entry point for Zeroth.
//
// Talk to a local zerothd, or run headless workflows against the same kernel.
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
