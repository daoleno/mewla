package connections

import "encoding/json"

// Set these public values once in Zen's release build after publisher
// registration. They contain no secret and require no end-user configuration.
// They are intentionally empty until Zen-owned registrations actually exist.
var GitHubPublicClientID string
var SlackPublicClientID string

// GoogleExchangeOrigin is a product-owned HTTPS origin shipped in releases.
// It points to cmd/zen-google-auth, never a caller-supplied callback or daemon.
var GoogleExchangeOrigin string

func (m *Manager) clientConfig(kind string) (OAuthClientConfig, bool, error) {
	switch kind {
	case "github":
		if GitHubPublicClientID != "" {
			return OAuthClientConfig{ClientID: GitHubPublicClientID}, true, nil
		}
	case "slack":
		if SlackPublicClientID != "" {
			return OAuthClientConfig{ClientID: SlackPublicClientID, RedirectURL: NativeCallback}, true, nil
		}
	}
	// Operator overrides serve existing single-operator installations. This is
	// not the product distribution mechanism and is never asked of end users.
	raw, ok, err := m.vault.Get("oauth-client:" + kind)
	var client OAuthClientConfig
	if err == nil && ok {
		err = json.Unmarshal([]byte(raw), &client)
	}
	return client, ok, err
}
