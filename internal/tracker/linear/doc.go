// Package linear is the Linear-backed [tracker.Provider].
//
// It talks to Linear's GraphQL API over HTTP. There is no vendor SDK.
// Polling is the stage-1 default (Z1-082). A webhook secret is opt-in:
// when set, [Provider] also implements http.Handler for faster assign
// and delegate edges, still backed by the same poller so a missed POST
// cannot skip an un-assign. Classic Linear assignee and native
// agent-delegation (Issue.delegate / AgentSessionEvent) both start a run.
//
// GraphQL auth is a Linear personal API key by default (Authorization is
// the raw key). [Config.AuthStyle] [AuthOAuth] is for an OAuth application
// actor token, sent as "Bearer <token>". The styles are not auto-detected:
// a wrong header is a 401, logged at error on every poll.
package linear
