//go:build !race

package main

// raceBuild is false for an uninstrumented test binary. That is the
// configuration that produced the spike G1 numbers, so the full 10+110
// CLI attach bench runs here. See cliAttachLatencyPlan.
const raceBuild = false
