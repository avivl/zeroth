// Package linear is the Linear-backed [tracker.Provider].
//
// It talks to Linear's GraphQL API over HTTP. There is no vendor SDK.
// Polling is the stage-1 default (Z1-082). A webhook secret is opt-in:
// when set, [Provider] also implements http.Handler for faster assign
// edges, still backed by the same poller so a missed POST cannot skip
// an un-assign.
package linear
