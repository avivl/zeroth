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

// Config is how zerothd constructs the Linear provider. APIKey is required.
// AgentUserID is the Linear user the operator assigns issues to. Empty
// means the API key's viewer, which is the usual agent-identity setup.
type Config struct {
	APIKey       string
	Endpoint     string
	AgentUserID  string
	TeamID       string
	ProjectID    string
	PollInterval time.Duration
	// WebhookSecret, when set, enables [Provider.ServeHTTP] (Z1-082 opt-in).
	WebhookSecret string
	HTTPClient    *http.Client
}
