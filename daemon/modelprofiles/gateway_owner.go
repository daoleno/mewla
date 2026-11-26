package modelprofiles

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles/translate"
)

// gatewayOwnerFile groups the Owner integration of the machine-level gateway
// and its client config takeovers. There is one gateway for every client: the
// daemon projects it into Codex's config.toml and Claude Code's settings env
// on start, so any process launched outside Mewla reaches the currently
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
	gateway := NewGateway(addr, cfg.Credentials, WithGatewayRequestResolver(o.resolveGatewayRequestLive), WithGatewayClaudeStandIn(o.gatewayClaudeStandIn))
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fmt.Errorf("%w: create gateway state dir: %v", ErrInvalid, err)
		}
		gateway.SetGatewayStatePath(filepath.Join(stateDir, "gateway.json"))
	}
	o.gateway = gateway
	if stateDir != "" {
		if path := strings.TrimSpace(cfg.CodexConfigPath); path != "" {
			o.takeover = NewTakeover(path, stateDir, gateway)
		}
		if path := strings.TrimSpace(cfg.ClaudeSettingsPath); path != "" {
			o.claudeTakeover = NewClaudeTakeover(path, stateDir)
		}
	}
	if err := gateway.Listen(); err != nil {
		// Non-fatal: takeover status reports broken; honest connection
		// failures reach every routed Codex process. Claude settings get the
		// user's own values back rather than a dead endpoint.
		o.syncClaudeTakeover("")
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
	o.syncClaudeTakeover("")
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
		o.refreshGateway("")
	}
	o.startClientConfigWatch()
	return nil
}

// clientConfigWatchInterval is how often the daemon looks at the client
// configs it projects into, for a change another tool made.
var clientConfigWatchInterval = 3 * time.Second

// startClientConfigWatch heals the Codex config and Claude settings whenever
// they change: another tool (Magpie) restoring what it remembered can name
// Mewla's gateway after Mewla let go of it. One stat per file per interval;
// Heal reads a file only after it changed.
func (o *Owner) startClientConfigWatch() {
	if o == nil || (o.takeover == nil && o.claudeTakeover == nil) || o.backgroundCtx == nil {
		return
	}
	codex, claude := o.takeover, o.claudeTakeover
	heal := func() {
		codex.Heal()
		claude.Heal()
	}
	stamp := func(path string) string {
		if path == "" {
			return ""
		}
		info, err := os.Stat(path)
		if err != nil {
			return "missing"
		}
		return fmt.Sprintf("%d.%d", info.ModTime().UnixNano(), info.Size())
	}
	paths := []string{codex.ConfigPath(), claude.SettingsPath()}
	// Stamped before the first heal: a write from now on is seen.
	seen := map[string]string{}
	for _, path := range paths {
		seen[path] = stamp(path)
	}
	heal()
	o.background.Add(1)
	go func() {
		defer o.background.Done()
		ticker := time.NewTicker(clientConfigWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-o.backgroundCtx.Done():
				return
			case <-ticker.C:
			}
			changed := false
			for _, path := range paths {
				if now := stamp(path); now != seen[path] {
					seen[path] = now
					changed = true
				}
			}
			if changed {
				heal()
			}
		}
	}()
}

// GatewayServesCodexModel reports whether the machine gateway answers a Codex
// request for model: a ready connection offers it, one way. A Codex Session
// on the gateway (model_provider "mewla-gateway") lists Codex's own catalog,
// so its model menu keeps only these.
func (o *Owner) GatewayServesCodexModel(model string) bool {
	_, err := o.resolveGatewayRequest(GatewayProtocolResponses, model)
	return err == nil
}

// resolveGatewayRequestLive is what the gateway asks for each request. A model
// no cached list offers gets one live look at the Codex connections' lists
// before the gateway refuses it: a list fetched weeks ago does not know a
// model the Provider added since (11-26: gpt-6.2-astra answered 404 from a
// list cached on 10-15 while the Provider served it). The look is the
// per-Session router's: shared per connection and at most once per
// routeRefreshMinAge, so junk model names cannot turn into upstream traffic.
func (o *Owner) resolveGatewayRequestLive(protocol, modelID string) (GatewayUpstream, error) {
	up, err := o.resolveGatewayRequest(protocol, modelID)
	if err == nil || protocol != GatewayProtocolResponses || !errors.Is(err, ErrNotFound) || strings.TrimSpace(modelID) == "" {
		return up, err
	}
	wantSlug, _ := splitGatewayModelID(modelID)
	refreshed := false
	for _, profile := range o.store.Catalog().Profiles {
		if clientFromExecutor(profile.Client) != ClientCodex || !o.connectionReady(profile) {
			continue
		}
		if wantSlug != "" && !strings.EqualFold(profile.Slug, wantSlug) {
			continue
		}
		if _, rerr := o.refreshRouteModelCatalog(profile.ID); rerr == nil {
			refreshed = true
		}
	}
	if !refreshed {
		return up, err
	}
	return o.resolveGatewayRequest(protocol, modelID)
}

// gatewayClaudeStandIn is the model a plain-terminal Claude Code's request
// goes out as when the chosen Claude connection's list (as the Provider
// answered it) lacks the Claude id asked for. See claudeStandIn.
func (o *Owner) gatewayClaudeStandIn(profileID, model string) (string, bool) {
	if o == nil || o.store == nil || !o.connectionListedByProvider(profileID) {
		return "", false
	}
	profile, err := o.store.Get(profileID)
	if err != nil {
		return "", false
	}
	entries, _ := o.modelsForConnection(profile, false)
	for _, e := range entries {
		if e.Available && e.ID == model {
			return "", false
		}
	}
	return claudeStandIn(model, standInCandidates(entries), profile.Model)
}

// connectionListedByProvider reports whether the Provider itself has answered
// a model list for the connection. Until it has, the list shown is a stand-in
// (Codex's own models, the bundled list) that says nothing of what it serves.
func (o *Owner) connectionListedByProvider(profileID string) bool {
	o.mu.Lock()
	cache := o.discovery
	o.mu.Unlock()
	if cache == nil {
		return false
	}
	e, ok := cache.get(profileID)
	return ok && e.Err == "" && len(e.IDs) > 0
}

func splitGatewayModelID(modelID string) (slug, model string) {
	modelID = strings.TrimSpace(modelID)
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", modelID
}

func (o *Owner) resolveGatewayRequest(protocol, modelID string) (GatewayUpstream, error) {
	if o == nil || o.store == nil {
		return GatewayUpstream{}, ErrNotFound
	}
	if protocol == GatewayProtocolCodexTools {
		return o.selectedCodexGatewayUpstream()
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return GatewayUpstream{}, ErrModelUnsupported
	}
	if protocol == GatewayProtocolAnthropic {
		return o.selectedClaudeGatewayUpstream()
	}
	wantClient := ClientCodex
	wantSlug, wantModel := splitGatewayModelID(modelID)
	selectedCodex := normalizeID(o.store.DefaultProfileID(executorFromClient(wantClient)))
	var matches []Profile
	for _, profile := range o.store.Catalog().Profiles {
		if clientFromExecutor(profile.Client) != wantClient || !o.connectionReady(profile) {
			continue
		}
		if wantSlug != "" && !strings.EqualFold(profile.Slug, wantSlug) {
			continue
		}
		target, err := CompileConnectionTarget(profile, wantClient, wantModel, "")
		if route, ok := RouteProtocolForExecutor(ExecutorCodex, target.Protocol); err != nil || !ok || route != protocol {
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
		// A connection whose models were never listed (just added, or its
		// list could not be fetched) is a candidate when it is the one chosen
		// for Codex: the Provider decides, instead of another connection that
		// happens to have an older list (11-27: a request went to a
		// connection that was not chosen because only its list had the model).
		unlisted := normalizeID(profile.ID) == selectedCodex && !o.connectionListedByProvider(profile.ID)
		if available || unlisted || (len(entries) == 0 && target.Model == wantModel) {
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
		return GatewayUpstream{}, fmt.Errorf("%w: no Claude connection selected", ErrNoProviderSelected)
	}
	profile, err := o.store.Get(selected)
	if err != nil {
		return GatewayUpstream{}, err
	}
	if !o.connectionReady(profile) {
		return GatewayUpstream{}, forProvider(fmt.Errorf("%w: %s", ErrCredentialNotReady, selected), profile.Name, "")
	}
	target, err := CompileConnectionTarget(profile, ClientClaude, "", "")
	if err != nil {
		return GatewayUpstream{}, err
	}
	if route, ok := RouteProtocolForExecutor(ExecutorClaude, target.Protocol); !ok || route != GatewayProtocolAnthropic {
		return GatewayUpstream{}, forProvider(fmt.Errorf("%w: Claude connection %s does not speak Anthropic Messages", ErrRouteProtocolMismatch, selected), profile.Name, "")
	}
	return GatewayUpstreamFromProfile(target), nil
}

// selectedCodexGatewayUpstream is the target of Codex's standalone tool
// endpoints (image_gen, web.run): the currently selected Codex connection, which
// must speak Responses. Like the Claude passthrough, the tool model is left to
// the Provider.
func (o *Owner) selectedCodexGatewayUpstream() (GatewayUpstream, error) {
	selected := normalizeID(o.store.DefaultProfileID(ExecutorCodex))
	if selected == "" {
		return GatewayUpstream{}, fmt.Errorf("%w: no Codex connection selected", ErrNoProviderSelected)
	}
	profile, err := o.store.Get(selected)
	if err != nil {
		return GatewayUpstream{}, err
	}
	if !o.connectionReady(profile) {
		return GatewayUpstream{}, forProvider(fmt.Errorf("%w: %s", ErrCredentialNotReady, selected), profile.Name, "")
	}
	target, err := CompileConnectionTarget(profile, ClientCodex, "", "")
	if err != nil {
		return GatewayUpstream{}, err
	}
	if apiOfProtocol(target.Protocol) != translate.APIResponses {
		return GatewayUpstream{}, forProvider(fmt.Errorf("%w: Codex connection %s does not speak Responses", ErrRouteProtocolMismatch, selected), profile.Name, "")
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
// and default changes; client names the client whose selection the person
// changed ("" when unknown), which may take that client back from another
// tool.
func (o *Owner) refreshGatewayUpstream(client ...string) {
	which := ""
	if len(client) > 0 {
		which = clientFromExecutor(client[0])
	}
	if which == "" {
		which = "any"
	}
	o.refreshGateway(which)
}

// refreshGateway is refreshGatewayUpstream; explicit is the client the person
// just chose a Provider for ("any" for every client, "" for a background
// look, such as daemon start, which never takes a client from another tool).
func (o *Owner) refreshGateway(explicit string) {
	o.syncClaudeTakeover(explicit)
	if o == nil || o.gateway == nil || o.takeover == nil {
		return
	}
	o.syncCodexTakeover(explicit)
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
// login. A config Mewla cannot safely project stays untouched and is logged.
func (o *Owner) syncCodexTakeover(explicit string) {
	state, err := o.takeover.LoadState()
	if err != nil {
		log.Printf("WARN codex config takeover: %v", err)
		return
	}
	selected := o.store != nil && normalizeID(o.store.DefaultProfileID(ExecutorCodex)) != ""
	chosen := explicit == "any" || explicit == ClientCodex
	switch {
	case selected && (!state.Enabled || chosen) && o.gateway.Listening():
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
func (o *Owner) syncClaudeTakeover(explicit string) {
	if o == nil || o.claudeTakeover == nil || o.store == nil {
		return
	}
	var err error
	if addr, withToken, ok := o.claudeTakeoverTarget(); ok {
		err = o.claudeTakeover.Project("http://"+addr, withToken, explicit == "any" || explicit == ClientClaude)
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
