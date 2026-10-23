package connections

import "encoding/json"

// Set these public values once in Mewla's release build after publisher
// registration. They contain no secret and require no end-user configuration.
// They are intentionally empty until Mewla-owned registrations actually exist.
var GitHubPublicClientID string
var SlackPublicClientID string

// GoogleExchangeOrigin is a product-owned HTTPS origin shipped in releases:
// the account service (cmd/mewla-account) that runs Google sign-in for Google
// Calendar and Gmail. It is never a caller-supplied callback or daemon.
var GoogleExchangeOrigin string

func (m *Manager) clientConfig(kind string) (OAuthClientConfig, bool, error) {
	if e := lookup(kind); e != nil && e.OAuth != nil && e.OAuth.Public != nil {
		if client, ok := e.OAuth.Public(); ok {
			return client, true, nil
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
	e := lookup(kind)
	if e == nil {
		return ""
	}
	if e.Google != nil {
		// Google signs in only through Mewla's exchange, which holds the
		// client secret; a daemon never has one.
		if GoogleExchangeOrigin == "" {
			return "Google sign-in is not set up yet."
		}
		return ""
	}
	if e.OAuth == nil || e.OAuth.Source != clientPublisher {
		return ""
	}
	client, ok, err := m.clientConfig(kind)
	if err != nil {
		return "Connection settings are unavailable. Try again later."
	}
	ready := ok && client.ClientID != ""
	if e.OAuth.ReturnOK != nil {
		ready = ready && e.OAuth.ReturnOK(client)
	}
	if !ready {
		return e.Name + " sign-in is not set up yet."
	}
	return ""
}

func (m *Manager) catalog() []Integration {
	catalog := []Integration{}
	for _, item := range Catalog() {
		item.UnavailableReason = m.unavailableReason(item.ID)
		item.Available = item.UnavailableReason == ""
		catalog = append(catalog, item)
	}
	return catalog
}
