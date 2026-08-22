// Package linear is the Linear-backed [tracker.Provider].
//
// It talks to Linear's GraphQL API over HTTP. There is no vendor SDK.
// Polling is the stage-1 default (Z1-082). A webhook secret is opt-in:
// when set, [Provider] also implements http.Handler for faster assign
// edges, still backed by the same poller so a missed POST cannot skip
// an un-assign.
//
// GraphQL auth is a Linear personal API key by default (Authorization is
// the raw key). [Config.AuthStyle] [AuthOAuth] is for an OAuth application
// actor token, sent as "Bearer <token>". The styles are not auto-detected:
// a wrong header is a silent 401.
package linear
