// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, and GET /runs/{id}/events (JSON replay or
// WebSocket replay-then-live-tail). Other contract paths return 501
// until their packages land. The daemon wires this package to the
// store and session supervisor; it does not talk to Docker, Claude
// Code, or Linear by name.
package server
