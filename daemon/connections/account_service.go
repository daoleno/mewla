package connections

import (
	"context"
	"encoding/json"
)

// AccountService is the Mewla account service, reached as this computer's
// signed-in account. Slack's public app redeems and renews tokens there.
type AccountService interface {
	// SignedIn reports a computer signed in to a Mewla account.
	SignedIn() bool
	// SlackToken posts body to a slacktoken path and returns Slack's JSON.
	SlackToken(ctx context.Context, path string, body any) (json.RawMessage, error)
}

func (m *Manager) SetAccountService(service AccountService) {
	m.mu.Lock()
	m.accountService = service
	m.mu.Unlock()
}
