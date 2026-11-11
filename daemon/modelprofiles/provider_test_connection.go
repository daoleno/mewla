package modelprofiles

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TestProviderConnection validates credentials and protocol compatibility by
// issuing a bounded, SSRF-safe model-list request. It does not create a
// connection, touch the credential store, mutate discovery cache, or alter defaults.
func (o *Owner) TestProviderConnection(in ProviderConnectionTestInput) (ProviderConnectionTestResult, error) {
	out := ProviderConnectionTestResult{}
	if o == nil || !o.started {
		return out, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	client := clientFromExecutor(in.Client)
	baseURL := strings.TrimSpace(in.BaseURL)
	secret := strings.TrimSpace(in.Credential)
	if client != ClientCodex && client != ClientClaude {
		return out, fmt.Errorf("%w: client must be codex or claude", ErrInvalid)
	}
	if baseURL == "" || secret == "" {
		return out, fmt.Errorf("%w: base_url and credential are required", ErrInvalid)
	}

	// Empty ModelID: compileProviderConnectionForClient uses the preset
	// ClientModel contract id as the ephemeral probe placeholder.
	profile, err := compileProviderConnectionForClient(ProviderConnectionInput{
		ID:       "connection-test",
		Name:     "Connection test",
		Client:   client,
		PresetID: ProviderPresetCustom,
		BaseURL:  baseURL,
		Advanced: true,
	}, executorFromClient(client))
	if err != nil {
		return out, err
	}

	lookup := func(name string) (string, bool) {
		if name == profile.CredentialEnv {
			return secret, true
		}
		return "", false
	}
	result, err := runConnectionProbe(client, profile, lookup)
	secret = ""
	return result, err
}

// TestSavedProviderConnection probes the exact saved connection by stable
// Provider ID: the persisted Base URL, the compiled per-client protocol, and
// the active stored credential ref are all resolved daemon-side, so the App
// never supplies or receives the secret. Read-only: no catalog/default/session
// mutation, no discovery-cache write, and no secret in the result or logs.
func (o *Owner) TestSavedProviderConnection(connectionID string) (ProviderConnectionTestResult, error) {
	out := ProviderConnectionTestResult{}
	if o == nil || !o.started || o.store == nil {
		return out, fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	connectionID = normalizeID(connectionID)
	raw, err := o.store.Get(connectionID)
	if err != nil {
		return out, err
	}
	if !isAccountConnection(raw) {
		return out, fmt.Errorf("%w: connection %s is not a saved Provider connection", ErrInvalid, connectionID)
	}
	client := clientFromExecutor(raw.Client)
	if client != ClientCodex && client != ClientClaude {
		return out, fmt.Errorf("%w: connection %s has no client scope", ErrInvalid, connectionID)
	}
	// Compile the exact saved endpoint/protocol for this client; the model
	// stays a probe placeholder — testing never selects or mutates a model.
	profile, err := CompileConnectionTarget(raw, executorFromClient(client), "", "")
	if err != nil {
		return out, err
	}
	o.mu.Lock()
	secret := ""
	if o.creds != nil {
		if val, ok, gerr := o.creds.Get(activeCredentialRef(raw)); gerr == nil && ok {
			secret = strings.TrimSpace(val)
		}
	}
	o.mu.Unlock()
	if secret == "" {
		return out, fmt.Errorf("%w: %s", ErrCredentialNotReady, connectionID)
	}
	lookup := func(name string) (string, bool) {
		if name == profile.CredentialEnv {
			return secret, true
		}
		return "", false
	}
	result, err := runConnectionProbe(client, profile, lookup)
	secret = ""
	return result, err
}

// runConnectionProbe issues the bounded SSRF-safe model-list probe against a
// compiled profile and returns secret-free facts only. lookup supplies the
// secret to the probe without ever retaining it on the result.
func runConnectionProbe(client string, profile Profile, lookup func(string) (string, bool)) (ProviderConnectionTestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	startedAt := time.Now()
	ids, err := fetchUpstreamModelIDs(ctx, NewSafeHTTPClient(15*time.Second), profile, nil, lookup)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return ProviderConnectionTestResult{}, connectionTestError(err, profile, lookup)
	}
	return ProviderConnectionTestResult{Client: client, ModelCount: len(ids), LatencyMS: latencyMS}, nil
}

// connectionTestError keeps a typed failure (its own wire code) and gives any
// other one ErrConnectionTestFailed, so the App shows the probe's reason
// ("connection refused", "HTTP 404") instead of "Providers are not
// available". The key never leaves in it: an endpoint that echoes it back
// (in a URL or a body) gets it replaced.
func connectionTestError(err error, profile Profile, lookup func(string) (string, bool)) error {
	if ControlErrorCode(err) != CodeProfilesUnavailable {
		return err
	}
	message := err.Error()
	if secret, ok := lookup(profile.CredentialEnv); ok {
		if secret = strings.TrimSpace(secret); secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return connectionTestFailure{reason: message}
}

// connectionTestFailure is ErrConnectionTestFailed whose words are only the reason.
type connectionTestFailure struct{ reason string }

func (e connectionTestFailure) Error() string { return e.reason }
func (e connectionTestFailure) Unwrap() error { return ErrConnectionTestFailed }
