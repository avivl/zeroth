// Package bench measures G1 attach latency and G6 SQLite write stalls.
//
// `spike bench` is the re-runnable M1 baseline. Tests call Run with a
// smaller sample count so CI still exercises the pass bars.
package bench
