// Package server is the local HTTP surface for zerothd.
//
// It implements the generated OpenAPI ServerInterface for the run
// lifecycle the CLI and web UI share: create, list, get, steer,
// background, foreground, GET /runs/{id}/events, audit list/verify,
// and agent PATCH (a signed audited action). Other contract paths
// return 501 until their packages land. The daemon wires this package
// to the store, signer, and session supervisor; it does not talk to
// Docker, Claude Code, or Linear by name.
package server
