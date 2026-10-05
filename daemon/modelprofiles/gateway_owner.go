package modelprofiles

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// gatewayOwnerFile groups the Owner integration of the machine-level gateway
// and its client config takeovers. There is one gateway for every client: the
// daemon projects it into Codex's config.toml and Claude Code's settings env
// on start, so any process launched outside Zen reaches the currently
// selected Provider. Selecting a Provider is the only control; there is no
// separate enable step.

// SetGatewayBypass installs the takeover-readiness callback consulted by
// PrepareLaunchModel: when it reports true, new managed Codex launches use the
// plain base command and rely on the machine-level config projection (the
// canonical gateway route) instead of per-Session loopback injection.
func (o *Owner) SetGatewayBypass(bypass func() bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.gatewayBypass = bypass
}

// Gateway returns the machine-level gateway runtime (nil when not configured).
func (o *Owner) Gateway() *Gateway {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.gateway
}

// Takeover returns the Codex config takeover manager (nil when not configured).
func (o *Owner) Takeover() *Takeover {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.takeover
}

// startGateway builds, binds, and restores the machine-level gateway from
// durable state. A bind failure is non-fatal for the daemon: takeover reports
// broken truthfully and per-Session routing continues to work. A corrupt
// gateway state file is fatal (fail closed, same as the route listener).
func (o *Owner) startGateway(cfg OwnerConfig) error {
	addr := strings.TrimSpace(cfg.GatewayAddr)
	if addr == "" {
		return nil
	}
	stateDir := strings.TrimSpace(cfg.GatewayStateDir)
	statePath := gatewayStateFilePath(stateDir)
	listenAddr, _, stateErr := LoadGatewayState(statePath)
	if stateErr != nil {
		return stateErr
	}
	if listenAddr != "" {
		addr = listenAddr
	}
	gateway := NewGateway(addr, cfg.Credentials, WithGatewayRequestResolver(o.resolveGatewayRequest))
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fmt.Errorf("%w: create gateway state dir: %v", ErrInvalid, err)
		}
		gateway.SetGatewayStatePath(filepath.Join(stateDir, "gateway.json"))
	}
	o.gateway = gateway
	if stateDir != "" {
		o.takeover = NewTakeover(cfg.CodexConfigPath, stateDir, gateway)
		if path := strings.TrimSpace(cfg.ClaudeSettingsPath); path != "" {
			o.claudeTakeover = NewClaudeTakeover(path, stateDir)
		}
	}
	if err := gateway.Listen(); err != nil {
		// Non-fatal: takeover status reports broken; honest connection
		// failures reach every routed Codex process. Claude settings get the
		// user's own values back rather than a dead endpoint.
		o.syncClaudeTakeover()
		return nil
	}
	// Restore the same listener address / upstream profile across restarts.
	_, profileID, err := LoadGatewayState(statePath)
	if err != nil {
		return err
	}
	if up, ok := o.resolveGatewayUpstream(profileID); ok {
		gateway.SetUpstream(up)
	} else {
		gateway.ClearUpstream()
	}
	// Re-project on every start: the listener may have fallen back to another
	// port, and the Claude selection may have changed while the daemon was down.
	o.syncClaudeTakeover()
	if o.takeover != nil {
		state, err := o.takeover.LoadState()
		if err != nil {
			return err
		}
		if state.Enabled {
			// Repair the live projection to the same address before the
			// takeover can claim active (daemon restart path).
			repairAddr := gateway.ActualAddr()
			if repairAddr == "" {
				repairAddr = strings.TrimSpace(state.ListenAddr)
			}
			if repairAddr == "" {
				repairAddr = addr
			}
			if _, repairErr := o.takeover.Repair(repairAddr); repairErr != nil {
				return repairErr
			}
		}
		o.refreshGatewayUpstream()
	}
	return nil
}

func (o *Owner) resolveGatewayRequest(protocol, modelID string) (GatewayUpstream, error) {
	if o == nil || o.store == nil {
		return GatewayUpstream{}, ErrNotFound
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return GatewayUpstream{}, ErrModelUnsupported
	}
	if protocol == GatewayProtocolAnthropic {
		return o.selectedClaudeGatewayUpstream()
	}
	wantClient := ClientCodex
	parts := strings.SplitN(modelID, "/", 2)
	wantSlug, wantModel := "", modelID
	if len(parts) == 2 {
		wantSlug, wantModel = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	var matches []Profile
	for _, profile := range o.store.Catalog().Profiles {
		if clientFromExecutor(profile.Client) != wantClient || !o.connectionReady(profile) {
			continue
		}
		if wantSlug != "" && !strings.EqualFold(profile.Slug, wantSlug) {
			continue
		}
		target, err := CompileConnectionTarget(profile, wantClient, wantModel, "")
		if err != nil || routeProtocolFor(target.Protocol) != protocol {
			continue
		}
		if o.gatewayModelDisabled(profile.ID, wantModel) {
			continue
		}
		entries, _ := o.modelsForConnection(profile, false)
		available := false
		for _, entry := range entries {
			if entry.ID == wantModel && entry.Available {
				available = true
				break
			}
		}
		if available || (len(entries) == 0 && target.Model == wantModel) {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		return GatewayUpstream{}, fmt.Errorf("%w: %s", ErrNotFound, modelID)
	}
	if len(matches) > 1 {
		// The same model is often installed on every Codex connection. The
		// selected client default is the current Provider; only a model that
		// the default does not expose stays an honest conflict.
		selected := normalizeID(o.store.DefaultProfileID(executorFromClient(wantClient)))
		if selected != "" {
			for _, match := range matches {
				if normalizeID(match.ID) == selected {
					return GatewayUpstreamFromProfile(match), nil
				}
			}
		}
		return GatewayUpstream{}, fmt.Errorf("%w: ambiguous model %s", ErrConflict, modelID)
	}
	return GatewayUpstreamFromProfile(matches[0]), nil
}

// selectedClaudeGatewayUpstream is the Anthropic gateway target: the currently
// selected Claude connection, whatever model the client asks for. The gateway
// is a passthrough for Claude, so model choice stays between the CLI and the
// Provider.
func (o *Owner) selectedClaudeGatewayUpstream() (GatewayUpstream, error) {
	selected := normalizeID(o.store.ClientDefault(ClientClaude))
	if selected == "" {
		return GatewayUpstream{}, fmt.Errorf("%w: no Claude connection selected", ErrNotFound)
	}
	profile, err := o.store.Get(selected)
	if err != nil {
		return GatewayUpstream{}, err
	}
	if !o.connectionReady(profile) {
		return GatewayUpstream{}, fmt.Errorf("%w: %s", ErrCredentialNotReady, selected)
	}
	target, err := CompileConnectionTarget(profile, ClientClaude, "", "")
	if err != nil {
		return GatewayUpstream{}, err
	}
	if routeProtocolFor(target.Protocol) != GatewayProtocolAnthropic {
		return GatewayUpstream{}, fmt.Errorf("%w: Claude connection %s does not speak Anthropic Messages", ErrUpstreamInvalid, selected)
	}
	return GatewayUpstreamFromProfile(target), nil
}

func (o *Owner) gatewayModelDisabled(connectionID, modelID string) bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	cache := o.discovery
	o.mu.Unlock()
	if cache == nil {
		return false
	}
	entry, ok := cache.get(connectionID)
	if !ok {
		return false
	}
	for _, disabled := range entry.Disabled {
		if normalizeSpace(disabled) == normalizeSpace(modelID) {
			return true
		}
	}
	return false
}

func routeProtocolFor(protocol string) string {
	switch normalizeID(protocol) {
	case ProtocolAnthropicMessages:
		return GatewayProtocolAnthropic
	default:
		return GatewayProtocolResponses
	}
}

func gatewayStateFilePath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(strings.TrimSpace(stateDir), "gateway.json")
}

// refreshGatewayUpstream points the gateway at the currently selected Codex
// Provider connection when takeover is active. Called after Provider switches
// and default changes.
func (o *Owner) refreshGatewayUpstream() {
	o.syncClaudeTakeover()
	if o == nil || o.gateway == nil || o.takeover == nil {
		return
	}
	o.syncCodexTakeover()
	state, err := o.takeover.LoadState()
	if err != nil || !state.Enabled {
		return
	}
	profileID := ""
	if o.store != nil {
		profileID = strings.TrimSpace(o.store.DefaultProfileID(ExecutorCodex))
	}
	if up, ok := o.resolveGatewayUpstream(profileID); ok {
		o.gateway.SetUpstream(up)
		return
	}
	o.gateway.ClearUpstream()
}

// syncCodexTakeover projects the gateway into the Codex config while a Codex
// Provider connection is selected and the gateway listens, with no opt-in.
// Official login (no selection) gives the config back so Codex uses its own
// login. A config Zen cannot safely project stays untouched and is logged.
func (o *Owner) syncCodexTakeover() {
	state, err := o.takeover.LoadState()
	if err != nil {
		log.Printf("WARN codex config takeover: %v", err)
		return
	}
	selected := o.store != nil && normalizeID(o.store.DefaultProfileID(ExecutorCodex)) != ""
	switch {
	case selected && !state.Enabled && o.gateway.Listening():
		_, err = o.takeover.Enable(o.gateway.ActualAddr())
	case !selected && state.Enabled:
		_, err = o.takeover.Disable()
	}
	if err != nil {
		log.Printf("WARN codex config takeover: %v", err)
	}
}

// syncClaudeTakeover keeps Claude Code's user settings pointed at the gateway
// while a Claude connection is selected and the gateway listens, and puts the
// user's own values back otherwise. Failures leave the settings untouched and
// are logged: a projection problem must never block a Provider switch.
func (o *Owner) syncClaudeTakeover() {
	if o == nil || o.claudeTakeover == nil || o.store == nil {
		return
	}
	var err error
	if addr, withToken, ok := o.claudeTakeoverTarget(); ok {
		err = o.claudeTakeover.Project("http://"+addr, withToken)
	} else {
		err = o.claudeTakeover.Release()
	}
	if err != nil {
		log.Printf("WARN claude settings takeover: %v", err)
	}
}

func (o *Owner) claudeTakeoverTarget() (addr string, withToken bool, ok bool) {
	gateway := o.gateway
	if gateway == nil || !gateway.Listening() {
		return "", false, false
	}
	addr = gateway.ActualAddr()
	selected := normalizeID(o.store.ClientDefault(ClientClaude))
	if addr == "" || selected == "" {
		return "", false, false
	}
	profile, err := o.store.Get(selected)
	if err != nil {
		return "", false, false
	}
	target, err := CompileConnectionTarget(profile, ClientClaude, "", "")
	if err != nil {
		return "", false, false
	}
	// A native-passthrough connection relies on the CLI's own login.
	return addr, normalizeID(target.AuthMode) != AuthModeNativePassthrough, true
}

// resolveGatewayUpstream derives the machine-level gateway upstream for a
// Codex connection id through the same per-client compile the launch/router
// path uses. Durable account connections store auth_mode=none (the raw form:
// per-client auth semantics are compiled at use time); compiling the target
// yields the codex bearer_env credential contract so the gateway injects the
// same credentials as the per-Session router. Secret-free; ok=false clears the
// upstream (no default, unknown id, or un-compilable connection).
func (o *Owner) resolveGatewayUpstream(profileID string) (GatewayUpstream, bool) {
	if o == nil || o.store == nil {
		return GatewayUpstream{}, false
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return GatewayUpstream{}, false
	}
	profile, err := o.store.ResolveProfileWithModel(ExecutorCodex, profileID, "")
	if err != nil {
		return GatewayUpstream{}, false
	}
	return GatewayUpstreamFromProfile(profile), true
}

// DefaultCodexConfigPath resolves the CLI's native Codex config path,
// honoring CODEX_HOME.
func DefaultCodexConfigPath() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml")
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".codex", "config.toml")
}
