package main

import (
	"context"
	"encoding/json"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/remote"
)

// accountService is the Mewla account service as this computer's signed-in
// account (remote access or Friends only, either counts). Mewla's Slack app
// redeems and renews its tokens there.
type accountService struct {
	stateDir string
	auth     *auth.Manager
}

func (s accountService) SignedIn() bool {
	_, ok, err := remote.LoadAccount(s.stateDir)
	return err == nil && ok
}

func (s accountService) SlackToken(ctx context.Context, path string, body any) (json.RawMessage, error) {
	config, ok, err := remote.LoadAccount(s.stateDir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, remote.ErrSignedOut
	}
	client, err := remote.NewClient(config.ServiceURL, config.ServiceCAPEM, s.auth)
	if err != nil {
		return nil, err
	}
	return client.SlackToken(ctx, path, body)
}
