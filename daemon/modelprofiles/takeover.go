package modelprofiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Takeover projection markers. Everything between the two markers is
// Mewla-owned and is replaced/removed atomically; unrelated user config bytes
// (comments, formatting, other keys, projects, trust) are preserved exactly.
const (
	// The marker text is persisted in user config files; it keeps the
	// pre-rename wording so existing blocks stay recognised.
	takeoverMarkerOpen  = "# >>> mewla-gateway: managed by Mewla. Do not edit. >>>"
	takeoverMarkerClose = "# <<< mewla-gateway <<<"
)

// TakeoverState is the durable takeover record (never secrets).
type TakeoverState struct {
	Enabled               bool   `json:"enabled"`
	ConfigPath            string `json:"config_path"`
	BackupPath            string `json:"backup_path"`
	ListenAddr            string `json:"listen_addr"`
	ProviderName          string `json:"provider_name"`
	OriginalProviderValue string `json:"original_provider_value,omitempty"`
	// OriginalProviderHadTable: Codex could use the saved value when Mewla
	// saved it (a built-in, or a table was there). Disconnect does not write
	// back a value whose table has gone since.
	OriginalProviderHadTable bool   `json:"original_provider_had_table,omitempty"`
	EnabledAt                string `json:"enabled_at,omitempty"`
}

// TakeoverStatus is the truthful control-plane view of takeover state.
type TakeoverStatus struct {
	State             string `json:"state"` // active | inactive | drifted | broken
	Detail            string `json:"detail,omitempty"`
	ConfigPath        string `json:"config_path,omitempty"`
	BackupPath        string `json:"backup_path,omitempty"`
	ListenAddr        string `json:"listen_addr,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`
	UpstreamProfileID string `json:"upstream_profile_id,omitempty"`
	GatewayListening  bool   `json:"gateway_listening"`
	Enabled           bool   `json:"enabled"`
	RestoreAvailable  bool   `json:"restore_available"`
}

const (
	TakeoverStateActive   = "active"
	TakeoverStateInactive = "inactive"
	TakeoverStateDrifted  = "drifted"
	TakeoverStateBroken   = "broken"
)

// Takeover manages the machine-level Codex gateway projection into the CLI's
// native config (~/.codex/config.toml by default, CODEX_HOME-aware). It never
// touches the user's model, effort, or unrelated config; it preserves the
// exact pre-takeover bytes in a durable backup and only ever writes the
// marked Mewla-owned projection.
type Takeover struct {
	mu         sync.Mutex
	statePath  string
	backupDir  string
	configPath string
	gateway    *Gateway
}

// NewTakeover constructs the takeover manager. configPath is the Codex config
// file (honor CODEX_HOME); stateDir is the daemon-owned gateway state dir.
func NewTakeover(configPath, stateDir string, gateway *Gateway) *Takeover {
	return &Takeover{
		configPath: strings.TrimSpace(configPath),
		statePath:  filepath.Join(strings.TrimSpace(stateDir), "state.json"),
		backupDir:  filepath.Join(strings.TrimSpace(stateDir), "backups"),
		gateway:    gateway,
	}
}

// ConfigPath returns the managed Codex config path.
func (t *Takeover) ConfigPath() string {
	if t == nil {
		return ""
	}
	return t.configPath
}

// StatePath returns the durable takeover state path.
func (t *Takeover) StatePath() string {
	if t == nil {
		return ""
	}
	return t.statePath
}

// LoadState reads the durable takeover state; missing state means inactive.
func (t *Takeover) LoadState() (TakeoverState, error) {
	if t == nil || t.statePath == "" {
		return TakeoverState{}, nil
	}
	raw, err := os.ReadFile(t.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return TakeoverState{}, nil
		}
		return TakeoverState{}, err
	}
	var state TakeoverState
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return TakeoverState{}, fmt.Errorf("%w: takeover state: %v", ErrRouteSnapshotInvalid, err)
	}
	return state, nil
}

func (t *Takeover) persistState(state TakeoverState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(t.statePath, raw, 0o600)
}

// CodexActorMarkerHeader is the provider header that makes Codex offer its
// built-in image_gen (and standalone web.run) tools for a non-OpenAI provider.
// The value is a marker, not a credential: the gateway strips it and injects
// the selected connection's own credential.
const CodexActorMarkerHeader = "x-openai-actor-authorization"

// Projection returns the exact Mewla-owned config block for the gateway.
func (t *Takeover) Projection(listenAddr string) string {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	var b strings.Builder
	b.WriteString(takeoverMarkerOpen + "\n")
	b.WriteString("[model_providers." + GatewayProviderName + "]\n")
	b.WriteString("name = \"" + GatewayProviderName + "\"\n")
	b.WriteString("base_url = \"http://" + listenAddr + "/v1\"\n")
	b.WriteString("wire_api = \"responses\"\n")
	b.WriteString("requires_openai_auth = false\n")
	b.WriteString("http_headers = { \"" + CodexActorMarkerHeader + "\" = \"" + GatewayProviderName + "\" }\n")
	b.WriteString(takeoverMarkerClose + "\n")
	return b.String()
}

// projectedProviderLine returns the top-level model_provider assignment the
// takeover installs.
func projectedProviderLine() string {
	return "model_provider = \"" + GatewayProviderName + "\""
}

// How the takeover shares ~/.codex/config.toml with the user and with other
// tools that point Codex at a gateway of their own (Magpie, CC Switch),
// learned from Magpie (github.com/yetone/magpie, MIT: internal/agent/codex.go
// unroute/Unwire, internal/agent/stash.go):
//
//   - Ownership is by value, not by line format: model_provider is Mewla's
//     while it names mewla-gateway, and the table is Mewla's by its name.
//   - Connect (Enable) saves the user's own model_provider only when Codex is
//     not on Mewla already, so a second connect never saves Mewla's own value.
//   - Disconnect (Disable) gives model_provider back only while it still names
//     Mewla; a value someone set since is theirs and stays.
//   - The user's value is given back only when Codex can still use it: a
//     provider whose table was there when Mewla saved it and is gone now is
//     not written back (Codex would refuse to start: "Model provider ... not
//     found"); the line goes, and Codex uses its own login.
//   - A restart (Repair) never takes Codex back from another tool: it puts
//     Mewla's table back and names it only when model_provider is empty.
//   - Heal covers what Magpie does not: a tool that remembered "mewla-gateway"
//     while it was connected writes it back after Mewla's gateway is off, and
//     Codex then names a table that is gone. While the takeover is off, a
//     model_provider naming Mewla is given back to the user's own value.
//   - Codex lists and resumes a thread only on the provider it names, so the
//     threads follow the provider (codex_threads.go), as Magpie lends them.

// codexBuiltinProviders are the providers Codex knows without a table.
var codexBuiltinProviders = map[string]bool{"openai": true, "oss": true, "ollama": true, "lmstudio": true}

// Enable activates the machine-level takeover: exact backup, then an atomic
// surgical projection that preserves every unrelated byte of the user's
// config. Idempotent: re-enabling an already-matching projection is a no-op.
// Missing config is created; malformed config fails safe.
func (t *Takeover) Enable(listenAddr string) (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if !missing {
		if err := validateConfigTOML(current); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	top := readTopProvider(string(current))
	if top.count > 1 {
		conflict := "multiple model_provider assignments exist; refusing to clobber"
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: conflict, ConfigPath: t.configPath}, fmt.Errorf("%w: %s", ErrInvalid, conflict)
	}
	if top.value != GatewayProviderName {
		// Codex is not on Mewla: what it names now is the user's own.
		state.OriginalProviderValue = top.value
		state.OriginalProviderHadTable = top.value == "" || codexProviderUsable(current, top.value)
	}
	projected := projectConfig(current, t.Projection(listenAddr), GatewayProviderName)
	if bytes.Equal(projected, current) {
		state.Enabled = true
		state.ConfigPath = t.configPath
		state.ListenAddr = listenAddr
		state.ProviderName = GatewayProviderName
		if err := t.persistState(state); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		return t.Status(), nil
	}
	if err := validateConfigTOML(projected); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: "projection produced invalid TOML: " + err.Error(), ConfigPath: t.configPath}, err
	}
	backupPath := ""
	if state.BackupPath != "" {
		if _, statErr := os.Stat(state.BackupPath); statErr == nil {
			backupPath = state.BackupPath
		}
	}
	if backupPath == "" {
		if err := os.MkdirAll(t.backupDir, 0o700); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		backupPath = filepath.Join(t.backupDir, "backup-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".toml")
		if err := writeAtomicFile(backupPath, current, 0o600); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	if err := writeAtomicFile(t.configPath, projected, 0o600); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state.Enabled = true
	state.ConfigPath = t.configPath
	state.BackupPath = backupPath
	state.ListenAddr = listenAddr
	state.ProviderName = GatewayProviderName
	state.EnabledAt = time.Now().UTC().Format(time.RFC3339)
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	t.moveThreads(state.OriginalProviderValue, GatewayProviderName)
	return t.Status(), nil
}

// Disable removes Mewla's table and gives model_provider back while it still
// names Mewla. A value someone else set since (another tool connected after
// Mewla, or the user) is theirs and stays; Heal gives it back later should
// that tool restore "mewla-gateway" after Mewla let go.
func (t *Takeover) Disable() (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	status, err := t.giveBack(state)
	if err != nil {
		return status, err
	}
	state.Enabled = false
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	return t.Status(), nil
}

// giveBack removes Mewla's table and, while model_provider names Mewla, puts
// the user's own value back. It moves the threads started on Mewla to
// whatever Codex names afterwards.
func (t *Takeover) giveBack(state TakeoverState) (TakeoverStatus, error) {
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if missing {
		return TakeoverStatus{State: TakeoverStateInactive, ConfigPath: t.configPath}, nil
	}
	if err := validateConfigTOML(current); err != nil {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	content := removeOwnTables(string(current))
	top := readTopProvider(content)
	if top.count > 1 {
		conflict := "multiple model_provider assignments exist; refusing to clobber"
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: conflict, ConfigPath: t.configPath}, fmt.Errorf("%w: %s", ErrInvalid, conflict)
	}
	if top.value == GatewayProviderName {
		content = setTopProvider(content, t.restoreTarget(state, []byte(content)))
	}
	if content != string(current) {
		if err := validateConfigTOML([]byte(content)); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
		if err := writeAtomicFile(t.configPath, []byte(content), 0o600); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	to := readTopProvider(content).value
	if to == "" {
		to = "openai"
	}
	t.moveThreads(GatewayProviderName, to)
	return TakeoverStatus{State: TakeoverStateInactive, ConfigPath: t.configPath}, nil
}

// restoreTarget is the model_provider value disconnect writes back: the
// user's own, unless it had a table when Mewla saved it and has none now.
func (t *Takeover) restoreTarget(state TakeoverState, content []byte) string {
	original := strings.TrimSpace(state.OriginalProviderValue)
	if original == "" || original == GatewayProviderName {
		return ""
	}
	if state.OriginalProviderHadTable && !codexProviderUsable(content, original) {
		return ""
	}
	return original
}

// Heal keeps the config honest about Mewla between its own changes. While the
// takeover is off, a model_provider naming Mewla (another tool restoring what
// it saw while connected) is given back to the user's own value, a leftover
// Mewla table goes, and threads left on Mewla follow. While it is on and Codex
// names Mewla, a missing table is put back. A value naming anyone else is
// never touched. Cheap when there is nothing to do: one read of the config.
func (t *Takeover) Heal() {
	if t == nil || t.configPath == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.LoadState()
	if err != nil {
		return
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil || missing || validateConfigTOML(current) != nil {
		return
	}
	content := string(current)
	top := readTopProvider(content)
	if !state.Enabled {
		if top.value != GatewayProviderName && !hasOwnTable(content) {
			t.moveThreads(GatewayProviderName, providerOrOpenAI(top.value))
			return
		}
		if _, err := t.giveBack(state); err != nil {
			log.Printf("WARN codex config: Codex names Mewla's gateway while it is off; could not give it back: %v", err)
			return
		}
		log.Printf("codex config: Codex named Mewla's gateway while it is off (another tool put it back); gave Codex %q back", providerOrOpenAI(t.restoreTarget(state, current)))
		return
	}
	if top.value != GatewayProviderName || hasOwnTable(content) {
		return
	}
	listenAddr := state.ListenAddr
	if t.gateway != nil && t.gateway.ActualAddr() != "" {
		listenAddr = t.gateway.ActualAddr()
	}
	projected := projectConfig(current, t.Projection(listenAddr), GatewayProviderName)
	if validateConfigTOML(projected) != nil {
		return
	}
	if err := writeAtomicFile(t.configPath, projected, 0o600); err == nil {
		log.Printf("codex config: Codex named Mewla's gateway without its table; put the table back")
	}
}

func providerOrOpenAI(value string) string {
	if value == "" {
		return "openai"
	}
	return value
}

// Repair re-applies Mewla's table at daemon start (the listener may have moved
// to another port). It names Mewla in model_provider only when that is empty:
// a value another tool or the user set while the daemon was down stays, and
// Mewla is in front again once that tool gives Codex back to it.
func (t *Takeover) Repair(listenAddr string) (TakeoverStatus, error) {
	if t == nil {
		return TakeoverStatus{}, fmt.Errorf("%w: takeover not configured", ErrInvalid)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		listenAddr = DefaultGatewayListenAddr
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	if !missing {
		if err := validateConfigTOML(current); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, nil
		}
	}
	top := readTopProvider(string(current))
	if top.count > 1 {
		return TakeoverStatus{State: TakeoverStateDrifted, Detail: "multiple model_provider assignments exist; refusing to clobber", ConfigPath: t.configPath}, nil
	}
	name := GatewayProviderName
	if top.value != "" {
		name = top.value
	}
	projected := projectConfig(current, t.Projection(listenAddr), name)
	if !bytes.Equal(projected, current) {
		if err := validateConfigTOML(projected); err != nil {
			return TakeoverStatus{State: TakeoverStateDrifted, Detail: err.Error(), ConfigPath: t.configPath}, nil
		}
		if err := writeAtomicFile(t.configPath, projected, 0o600); err != nil {
			return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
		}
	}
	state, err := t.LoadState()
	if err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	state.Enabled = true
	state.ConfigPath = t.configPath
	state.ListenAddr = listenAddr
	state.ProviderName = GatewayProviderName
	if err := t.persistState(state); err != nil {
		return TakeoverStatus{State: TakeoverStateBroken, Detail: err.Error(), ConfigPath: t.configPath}, err
	}
	return t.Status(), nil
}

// Status computes the truthful takeover status.
func (t *Takeover) Status() TakeoverStatus {
	status := TakeoverStatus{
		State:            TakeoverStateInactive,
		GatewayListening: t != nil && t.gateway != nil && t.gateway.Listening(),
	}
	if t == nil {
		return status
	}
	status.ConfigPath = t.configPath
	state, err := t.LoadState()
	if err != nil {
		status.State = TakeoverStateBroken
		status.Detail = "state unreadable: " + err.Error()
		return status
	}
	status.Enabled = state.Enabled
	status.BackupPath = state.BackupPath
	status.ListenAddr = state.ListenAddr
	status.ProviderName = state.ProviderName
	if !state.Enabled {
		return status
	}
	status.RestoreAvailable = strings.TrimSpace(state.BackupPath) != ""
	if !status.GatewayListening {
		status.State = TakeoverStateBroken
		status.Detail = "gateway is not listening on " + status.ListenAddr
		return status
	}
	if up, ok := t.gateway.Upstream(); ok {
		status.UpstreamProfileID = up.ProfileID
	}
	current, missing, err := readConfigBytes(t.configPath)
	if err != nil {
		status.State = TakeoverStateBroken
		status.Detail = "config unreadable: " + err.Error()
		return status
	}
	if missing {
		status.State = TakeoverStateDrifted
		status.Detail = "managed config is missing"
		return status
	}
	if err := validateConfigTOML(current); err != nil {
		status.State = TakeoverStateDrifted
		status.Detail = "managed config is malformed: " + err.Error()
		return status
	}
	expected := t.Projection(state.ListenAddr)
	if !bytes.Contains(current, []byte(expected)) {
		status.State = TakeoverStateDrifted
		status.Detail = "Mewla projection block is missing or edited"
		return status
	}
	top := readTopProvider(string(current))
	if top.count != 1 || top.value != GatewayProviderName {
		status.State = TakeoverStateDrifted
		if top.value != "" && top.value != GatewayProviderName {
			status.Detail = "Codex is on " + top.value + ", set outside Mewla"
		} else {
			status.Detail = "model_provider was changed outside Mewla"
		}
		return status
	}
	status.State = TakeoverStateActive
	return status
}

// topProvider is the top-level model_provider assignment: lines before the
// first table header. A model_provider inside a table ([profiles.x]) is that
// table's and not Codex's choice.
type topProvider struct {
	value string
	index int // line index of the first assignment, -1 when none
	count int
}

func readTopProvider(content string) topProvider {
	out := topProvider{index: -1}
	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if isTableHeader(trimmed) {
			break
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != "model_provider" {
			continue
		}
		out.count++
		if out.index < 0 {
			out.index = index
			out.value = unquoteTOMLScalar(value)
		}
	}
	return out
}

func isTableHeader(trimmed string) bool {
	return strings.HasPrefix(trimmed, "[")
}

// unquoteTOMLScalar reads a basic or literal TOML string value, dropping a
// trailing comment.
func unquoteTOMLScalar(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if quote := raw[0]; quote == '"' || quote == '\'' {
		if end := strings.IndexByte(raw[1:], quote); end >= 0 {
			return raw[1 : end+1]
		}
	}
	if hash := strings.IndexByte(raw, '#'); hash >= 0 {
		raw = raw[:hash]
	}
	return strings.TrimSpace(raw)
}

// setTopProvider sets (or, for "", removes) the top-level model_provider.
func setTopProvider(content, value string) string {
	top := readTopProvider(content)
	lines := strings.Split(content, "\n")
	if top.index >= 0 {
		if value == "" {
			lines = append(lines[:top.index], lines[top.index+1:]...)
		} else {
			lines[top.index] = "model_provider = " + quoteTOMLValue(value)
		}
		return strings.Join(lines, "\n")
	}
	if value == "" {
		return content
	}
	at := topInsertIndex(lines)
	lines = append(lines[:at], append([]string{"model_provider = " + quoteTOMLValue(value)}, lines[at:]...)...)
	return strings.Join(lines, "\n")
}

// topInsertIndex is where a new top-level key goes: before the first table,
// walking back over the blank lines that separate it.
func topInsertIndex(lines []string) int {
	firstSection := -1
	for index, line := range lines {
		if isTableHeader(strings.TrimSpace(line)) {
			firstSection = index
			break
		}
	}
	if firstSection < 0 {
		end := len(lines)
		for end > 0 && lines[end-1] == "" {
			end--
		}
		return end
	}
	at := firstSection
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	return at
}

// projectConfig returns current with Mewla's table in place (any older copy of
// it, marked or not, replaced) and model_provider naming provider, preserving
// every other byte (no blank lines are invented, so disable can restore
// byte-for-byte).
func projectConfig(current []byte, projection, provider string) []byte {
	content := removeOwnTables(string(current))
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	top := readTopProvider(strings.Join(lines, "\n"))
	blockLines := strings.Split(strings.TrimRight(projection, "\n"), "\n")
	var merged []string
	if top.index >= 0 {
		lines[top.index] = "model_provider = " + quoteTOMLValue(provider)
		at := topInsertIndex(lines)
		merged = append(merged, lines[:at]...)
		merged = append(merged, blockLines...)
		merged = append(merged, lines[at:]...)
	} else {
		at := topInsertIndex(lines)
		merged = append(merged, lines[:at]...)
		merged = append(merged, "model_provider = "+quoteTOMLValue(provider))
		merged = append(merged, blockLines...)
		merged = append(merged, lines[at:]...)
	}
	return []byte(strings.TrimRight(strings.Join(merged, "\n"), "\n") + "\n")
}

// removeOwnTables strips Mewla's marked block and any other table under
// Mewla's provider name (a copy another tool wrote back without the markers),
// preserving everything else byte-for-byte.
func removeOwnTables(content string) string {
	content = removeProjectionBlock(content)
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inside := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isTableHeader(trimmed) {
			inside = isOwnTableHeader(trimmed)
			if inside {
				continue
			}
		}
		if inside {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isOwnTableHeader(trimmed string) bool {
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
	if hash := strings.IndexByte(trimmed, '#'); hash >= 0 {
		name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(trimmed[:hash], "[")), "]"))
	}
	switch name {
	case "model_providers." + GatewayProviderName, `model_providers."` + GatewayProviderName + `"`, "model_providers.'" + GatewayProviderName + "'":
		return true
	}
	return false
}

func hasOwnTable(content string) bool {
	return codexProviderUsable([]byte(content), GatewayProviderName)
}

// codexProviderUsable reports whether Codex can start on name: a built-in
// provider, or one with a [model_providers.<name>] table.
func codexProviderUsable(content []byte, name string) bool {
	if codexBuiltinProviders[name] {
		return true
	}
	var doc struct {
		ModelProviders map[string]any `toml:"model_providers"`
	}
	if _, err := toml.Decode(string(content), &doc); err != nil {
		return false
	}
	_, ok := doc.ModelProviders[name]
	return ok
}

// removeProjectionBlock strips the marked Mewla-owned block (by marker lines),
// preserving everything else byte-for-byte.
func removeProjectionBlock(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inside := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == takeoverMarkerOpen {
			inside = true
			continue
		}
		if inside {
			if trimmed == takeoverMarkerClose {
				inside = false
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// hasExactProviderLine reports whether content contains exactly the projected
// uncommented model_provider line.
func hasExactProviderLine(content []byte, projectedLine string) bool {
	top := readTopProvider(string(content))
	if top.count != 1 {
		return false
	}
	return strings.TrimSpace(strings.Split(string(content), "\n")[top.index]) == projectedLine
}

// topLevelProviderValue returns the current uncommented top-level
// model_provider value ("" when none).
func topLevelProviderValue(content []byte) string {
	return readTopProvider(string(content)).value
}

func quoteTOMLValue(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// readConfigBytes reads the managed config; missing reports a not-yet-created
// config (treated as empty for first enable).
func readConfigBytes(path string) ([]byte, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false, fmt.Errorf("%w: codex config path is required", ErrInvalid)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("%w: read codex config: %v", ErrInvalid, err)
	}
	return raw, false, nil
}

// validateConfigTOML fails safe on malformed config.
func validateConfigTOML(raw []byte) error {
	var doc map[string]any
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return fmt.Errorf("%w: codex config is not valid TOML: %v", ErrInvalid, err)
	}
	return nil
}
