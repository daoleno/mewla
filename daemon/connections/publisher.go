package connections

import "encoding/json"

// Set these public values once in Zen's release build after publisher
// registration. They contain no secret and require no end-user configuration.
// They are intentionally empty until Zen-owned registrations actually exist.
var GitHubPublicClientID string
var SlackPublicClientID string

// GoogleExchangeOrigin is a product-owned HTTPS origin shipped in releases.
// It points to cmd/mewla-google-auth, never a caller-supplied callback or daemon.
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

// Availability describes starting a new built-in connection on this daemon.
// Existing accounts remain visible and usable independently of publisher setup.
func (m *Manager) unavailableReason(kind string) string {
	switch kind {
	case "github", "slack", "google":
	default:
		return ""
	}
	if kind == "google" && GoogleExchangeOrigin != "" {
		return ""
	}
	client, ok, err := m.clientConfig(kind)
	if err != nil {
		return "Connection settings are unavailable. Try again later."
	}
	ready := ok && client.ClientID != ""
	if kind == "google" {
		ready = ready && client.ClientSecret != "" && validCallback(client.RedirectURL)
	}
	if kind == "slack" {
		ready = ready && (client.RedirectURL == NativeCallback && client.ClientSecret == "" || validCallback(client.RedirectURL) && client.ClientSecret != "")
	}
	if !ready {
		return serviceName(kind) + " sign-in is not set up yet."
	}
	return ""
}

func (m *Manager) catalog() []Integration {
	catalog := Catalog()
	for i := range catalog {
		catalog[i].UnavailableReason = m.unavailableReason(catalog[i].ID)
		catalog[i].Available = catalog[i].UnavailableReason == ""
	}
	return catalog
}
