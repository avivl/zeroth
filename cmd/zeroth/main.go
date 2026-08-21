// Command zeroth is the CLI and headless entry point for Zeroth.
//
// Talk to a local zerothd, or run headless workflows against the same kernel.
package main

import "os"

func main() {
	if err := newRoot().Execute(); err != nil {
		os.Exit(1)
	}
}
