//go:build race

package main

// raceBuild is true when tests are compiled with -race. The full 10+110
// attach bench is too slow and too noisy under the race detector, so CI
// uses a smoke sample instead. See cliAttachLatencyPlan.
const raceBuild = true
