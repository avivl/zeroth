// Command zerothd is the Zeroth daemon: the local control plane.
//
// Stage 1 is single-player and runs on the operator's machine. There is no
// deployment story yet. The default bind is 127.0.0.1:8420 (ZEROTH_ADDR or
// --addr).
package main

import (
	"fmt"
	"os"
)

func main() {
	addr, err := parseAddr(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zerothd: %v\n", err)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "zerothd: skeleton stub (would bind %s). See README.md.\n", addr)
	os.Exit(0)
}
