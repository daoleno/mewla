package codexctl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daoleno/mewla/daemon/procspawn"
)

// Model is one entry of Codex's own model list: what the installed Codex
// offers for selection, as the app-server's `model/list` reports it.
type Model struct {
	ID          string
	DisplayName string
	// Hidden models are ones Codex does not offer for selection (internal or
	// retired identities).
	Hidden bool
	// DefaultEffort is the reasoning effort Codex starts the model with.
	DefaultEffort string
	// Efforts are the reasoning efforts the model accepts, in Codex's order.
	Efforts []string
	// Modalities are the input modalities the model accepts.
	Modalities []string
}

const maxModelListPages = 20

// ReadModelList asks a short-lived `codex app-server` for its model list
// through `model/list`, so the list is the installed Codex's own and moves with
// it instead of with Mewla's releases. Hidden models are not returned.
// binary is the codex executable; env is its environment (nil inherits).
func ReadModelList(ctx context.Context, binary string, env []string) ([]Model, error) {
	rpc, stop, err := startStdioAppServer(ctx, binary, env, "codex app-server models", "mewla-models")
	if err != nil {
		return nil, err
	}
	defer stop()
	var models []Model
	cursor := ""
	for page := 0; page < maxModelListPages; page++ {
		params := map[string]any{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := rpc.call(ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		var response struct {
			Data []struct {
				ID                     string `json:"id"`
				Model                  string `json:"model"`
				DisplayName            string `json:"displayName"`
				Hidden                 bool   `json:"hidden"`
				DefaultReasoningEffort string `json:"defaultReasoningEffort"`
				SupportedEfforts       []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
				InputModalities []string `json:"inputModalities"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, fmt.Errorf("%w: model/list: %v", ErrProtocol, err)
		}
		for _, entry := range response.Data {
			id := strings.TrimSpace(entry.Model)
			if id == "" {
				id = strings.TrimSpace(entry.ID)
			}
			if id == "" || entry.Hidden {
				continue
			}
			model := Model{ID: id, DisplayName: strings.TrimSpace(entry.DisplayName), DefaultEffort: strings.TrimSpace(entry.DefaultReasoningEffort),
				Modalities: entry.InputModalities}
			for _, effort := range entry.SupportedEfforts {
				if effort.Effort = strings.TrimSpace(effort.Effort); effort.Effort != "" {
					model.Efforts = append(model.Efforts, effort.Effort)
				}
			}
			models = append(models, model)
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			return models, nil
		}
		cursor = *response.NextCursor
	}
	return nil, fmt.Errorf("%w: model/list does not end after %d pages", ErrProtocol, maxModelListPages)
}

// startStdioAppServer starts a short-lived `codex app-server` on stdio and
// completes the initialize handshake. stop kills the process and reaps it.
func startStdioAppServer(ctx context.Context, binary string, env []string, label, clientName string) (*stdioRPC, func(), error) {
	cmd := procspawn.CommandContext(ctx, label, binary, "app-server", "--listen", "stdio://")
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrConnect, err)
	}
	stop := func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	rpc := newStdioRPC(stdin, stdout)
	if _, err := rpc.call(ctx, "initialize", map[string]any{
		"capabilities": map[string]any{"experimentalApi": true},
		"clientInfo":   map[string]any{"name": clientName, "version": "1"},
	}); err != nil {
		stop()
		return nil, nil, err
	}
	if err := rpc.notify("initialized"); err != nil {
		stop()
		return nil, nil, err
	}
	return rpc, stop, nil
}
