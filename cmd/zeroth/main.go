// Command zeroth is the CLI and headless entry point for Zeroth.
//
// Talk to a local zerothd, or run headless workflows against the same kernel.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "zeroth: skeleton stub. See README.md.")
	os.Exit(0)
}
