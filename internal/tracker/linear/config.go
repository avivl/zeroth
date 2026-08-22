package linear

import (
	"net/http"
	"time"
)

const (
	defaultEndpoint = "https://api.linear.app/graphql"
	defaultPoll     = 15 * time.Second
	driverName      = "linear"
)

// AuthStyle is how Config.APIKey is sent in the Authorization header.
// The two Linear token kinds are not interchangeable: a personal API key
// is the raw key, and an OAuth application actor token needs Bearer.
type AuthStyle string

const (
	// AuthPersonal is a Linear personal API key. Authorization is the raw key
	// with no scheme prefix. This is the default, matching existing setups.
	AuthPersonal AuthStyle = "personal"
	// AuthOAuth is a Linear OAuth application actor token.
	// Authorization is "Bearer <token>".
	AuthOAuth AuthStyle = "oauth"
)

// Config is how zerothd constructs the Linear provider. APIKey is required.
// AgentUserID is the Linear user the operator assigns or delegates
// issues to. Empty means the API key's viewer, which is the usual
// agent-identity setup.
type Config struct {
	APIKey       string
	Endpoint     string
	AgentUserID  string
	TeamID       string
	ProjectID    string
	PollInterval time.Duration
	// AuthStyle selects the Authorization header format. Empty means
	// [AuthPersonal]. Unknown values are rejected by [New].
	AuthStyle AuthStyle
	// WebhookSecret, when set, enables [Provider.ServeHTTP] (Z1-082 opt-in).
	WebhookSecret string
	HTTPClient    *http.Client
}
