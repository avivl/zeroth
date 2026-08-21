package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// defaultAddr is the stage-1 loopback bind. Override with ZEROTH_ADDR or --addr.
const defaultAddr = "127.0.0.1:8420"

func parseAddr(args []string) (string, error) {
	fs := flag.NewFlagSet("zerothd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addrFlag := fs.String("addr", "", "bind address for the local control plane (default 127.0.0.1:8420, or ZEROTH_ADDR)")
	if err := fs.Parse(args); err != nil {
		return "", fmt.Errorf("parse flags: %w", err)
	}
	return resolveAddr(*addrFlag, os.Getenv("ZEROTH_ADDR")), nil
}

func resolveAddr(flagAddr, envAddr string) string {
	if strings.TrimSpace(flagAddr) != "" {
		return flagAddr
	}
	if strings.TrimSpace(envAddr) != "" {
		return envAddr
	}
	return defaultAddr
}
