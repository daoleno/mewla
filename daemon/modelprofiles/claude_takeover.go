package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ClaudeTakeover projects the machine-level gateway into Claude Code's user
// settings (the env block of ~/.claude/settings.json, CLAUDE_CONFIG_DIR-aware).
// Claude Code reads that env at start-up, so a Claude process launched outside
// Zen (a plain shell, an IDE) reaches the selected Claude Provider through the
// gateway. Zen-managed launches pass their own --settings route and are
// unaffected. Only the projected env keys are touched; the user's prior values
// are kept in daemon state and put back on release.
type ClaudeTakeover struct {
	mu           sync.Mutex
	settingsPath string
	stateDir     string
	statePath    string
}

type claudeTakeoverState struct {
	Enabled      bool   `json:"enabled"`
	SettingsPath string `json:"settings_path"`
	BaseURL      string `json:"base_url"`
	// Written holds the exact JSON value Zen wrote per env key, so release
	// never clobbers a value the user changed afterwards.
	Written map[string]json.RawMessage `json:"written,omitempty"`
	// Prior holds the user's own value per env key Zen overwrote; a key absent
	// here was absent before the takeover.
	Prior map[string]json.RawMessage `json:"prior,omitempty"`
	// BackupPath is the exact pre-takeover copy of the settings file.
	BackupPath string `json:"backup_path,omitempty"`
}

const claudeTakeoverStateFile = "claude-takeover.json"

// NewClaudeTakeover constructs the settings takeover. stateDir is the
// daemon-owned gateway state dir.
func NewClaudeTakeover(settingsPath, stateDir string) *ClaudeTakeover {
	return &ClaudeTakeover{
		settingsPath: strings.TrimSpace(settingsPath),
		stateDir:     strings.TrimSpace(stateDir),
		statePath:    filepath.Join(strings.TrimSpace(stateDir), claudeTakeoverStateFile),
	}
}

// DefaultClaudeSettingsPath resolves Claude Code's user settings file,
// honoring CLAUDE_CONFIG_DIR.
func DefaultClaudeSettingsPath() string {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Enabled reports whether the projection is currently owned by Zen.
func (t *ClaudeTakeover) Enabled() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.loadState()
	return err == nil && state.Enabled
}

// Project points Claude Code at baseURL (the gateway root, no /v1). withToken
// also writes the loopback placeholder token so the CLI never asks for a login;
// the gateway injects the Provider's real credential. Idempotent.
func (t *ClaudeTakeover) Project(baseURL string, withToken bool) error {
	if t == nil || t.settingsPath == "" {
		return fmt.Errorf("%w: claude settings path is required", ErrInvalid)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.loadState()
	if err != nil {
		return err
	}
	path := resolveSettingsPath(t.settingsPath)
	fields, mode, err := readClaudeSettings(path)
	if err != nil {
		return err
	}
	env, err := settingsEnv(fields)
	if err != nil {
		return err
	}
	want := map[string]string{EnvAnthropicBaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}
	if withToken {
		want[EnvAnthropicAuthToken] = LoopbackAuthPlaceholder
	}
	if !state.Enabled {
		backup, err := t.backupSettings(path)
		if err != nil {
			return err
		}
		state = claudeTakeoverState{Prior: map[string]json.RawMessage{}, BackupPath: backup}
	}
	if state.Written == nil {
		state.Written = map[string]json.RawMessage{}
	}
	if state.Prior == nil {
		state.Prior = map[string]json.RawMessage{}
	}
	for _, key := range []string{EnvAnthropicBaseURL, EnvAnthropicAuthToken} {
		current, has := fieldValue(env, key)
		_, owned := state.Written[key]
		value, wanted := want[key]
		if wanted {
			if !owned && has {
				state.Prior[key] = current
			}
			raw := encodeJSONString(value)
			env = setField(env, key, raw)
			state.Written[key] = raw
			continue
		}
		if owned {
			env = restoreField(env, key, state)
			delete(state.Written, key)
			delete(state.Prior, key)
		}
	}
	if err := writeClaudeSettings(path, setField(fields, "env", encodeFields(env)), mode); err != nil {
		return err
	}
	state.Enabled = true
	state.SettingsPath = t.settingsPath
	state.BaseURL = want[EnvAnthropicBaseURL]
	return t.persistState(state)
}

// Release puts back the user's prior values for every key Zen still owns. A
// key the user changed after the takeover is left as the user set it.
func (t *ClaudeTakeover) Release() error {
	if t == nil || t.settingsPath == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.loadState()
	if err != nil || !state.Enabled {
		return err
	}
	path := resolveSettingsPath(t.settingsPath)
	fields, mode, err := readClaudeSettings(path)
	if err != nil {
		return err
	}
	env, err := settingsEnv(fields)
	if err != nil {
		return err
	}
	for key := range state.Written {
		env = restoreField(env, key, state)
	}
	if len(env) == 0 {
		fields = deleteField(fields, "env")
	} else {
		fields = setField(fields, "env", encodeFields(env))
	}
	if err := writeClaudeSettings(path, fields, mode); err != nil {
		return err
	}
	return t.persistState(claudeTakeoverState{SettingsPath: t.settingsPath, BackupPath: state.BackupPath})
}

// backupSettings keeps the exact pre-takeover bytes (0600, daemon state dir).
// A missing settings file has nothing to back up.
func (t *ClaudeTakeover) backupSettings(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("%w: read claude settings: %v", ErrInvalid, err)
	}
	backup := filepath.Join(t.stateDir, "backups", "claude-settings-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	if err := writeAtomicFile(backup, raw, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}

// resolveSettingsPath follows a dotfiles symlink so the atomic rewrite
// replaces the target file, never the link itself.
func resolveSettingsPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// restoreField puts back the prior value of key when the current value is
// still exactly what Zen wrote.
func restoreField(env []jsonField, key string, state claudeTakeoverState) []jsonField {
	current, has := fieldValue(env, key)
	if !has || !bytes.Equal(compactJSON(current), compactJSON(state.Written[key])) {
		return env
	}
	if prior, ok := state.Prior[key]; ok {
		return setField(env, key, prior)
	}
	return deleteField(env, key)
}

func (t *ClaudeTakeover) loadState() (claudeTakeoverState, error) {
	if t == nil {
		return claudeTakeoverState{}, nil
	}
	raw, err := os.ReadFile(t.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return claudeTakeoverState{}, nil
		}
		return claudeTakeoverState{}, err
	}
	var state claudeTakeoverState
	if err := json.Unmarshal(raw, &state); err != nil {
		return claudeTakeoverState{}, fmt.Errorf("%w: claude takeover state: %v", ErrRouteSnapshotInvalid, err)
	}
	return state, nil
}

func (t *ClaudeTakeover) persistState(state claudeTakeoverState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// Prior values may hold the user's own token: daemon-private file.
	return writeAtomicFile(t.statePath, raw, 0o600)
}

func readClaudeSettings(path string) ([]jsonField, os.FileMode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0o600, nil
		}
		return nil, 0, fmt.Errorf("%w: read claude settings: %v", ErrInvalid, err)
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, mode, nil
	}
	fields, err := decodeFields(raw)
	if err != nil {
		// Malformed user settings fail safe: never rewrite what we cannot parse.
		return nil, 0, fmt.Errorf("%w: claude settings is not a JSON object: %v", ErrInvalid, err)
	}
	return fields, mode, nil
}

func writeClaudeSettings(path string, fields []jsonField, mode os.FileMode) error {
	var out bytes.Buffer
	if err := json.Indent(&out, encodeFields(fields), "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, out.Bytes()) {
		return nil
	}
	return writeAtomicFile(path, out.Bytes(), mode)
}

func settingsEnv(fields []jsonField) ([]jsonField, error) {
	raw, ok := fieldValue(fields, "env")
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	env, err := decodeFields(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: claude settings env is not an object: %v", ErrInvalid, err)
	}
	return env, nil
}

// jsonField keeps object members in their original order so a settings
// rewrite only moves the keys it changes.
type jsonField struct {
	Key   string
	Value json.RawMessage
}

func decodeFields(raw []byte) ([]jsonField, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("expected an object")
	}
	var fields []jsonField
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		fields = append(fields, jsonField{Key: key, Value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err == nil {
		return nil, errors.New("trailing data after object")
	}
	return fields, nil
}

func encodeFields(fields []jsonField) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, field := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encodeJSONString(field.Key))
		b.WriteByte(':')
		b.Write(field.Value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

func fieldValue(fields []jsonField, key string) (json.RawMessage, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field.Value, true
		}
	}
	return nil, false
}

func setField(fields []jsonField, key string, value json.RawMessage) []jsonField {
	for i := range fields {
		if fields[i].Key == key {
			fields[i].Value = value
			return fields
		}
	}
	return append(fields, jsonField{Key: key, Value: value})
}

func deleteField(fields []jsonField, key string) []jsonField {
	out := fields[:0]
	for _, field := range fields {
		if field.Key != key {
			out = append(out, field)
		}
	}
	return out
}

func encodeJSONString(s string) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func compactJSON(raw json.RawMessage) []byte {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return raw
	}
	return b.Bytes()
}
