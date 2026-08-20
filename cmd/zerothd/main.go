// Command zerothd is the Zeroth daemon: the local control plane.
//
// Stage 1 is single-player and runs on the operator's machine. There is no
// deployment story yet.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "zerothd: skeleton stub. See README.md.")
	os.Exit(0)
}
