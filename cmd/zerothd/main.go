// Command zerothd is the Zeroth daemon: the local control plane.
//
// Stage 1 is single-player and runs on the operator's machine. There is no
// deployment story yet. The default bind is 127.0.0.1:8420 (ZEROTH_ADDR or
// --addr). Startup config is Cobra flags over Viper (env, config file, defaults).
package main

import "os"

func main() {
	cmd, _ := newRoot(deps{})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
