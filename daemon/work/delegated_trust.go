package work

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daoleno/mewla/daemon/codextrust"
	"github.com/daoleno/mewla/daemon/shellquote"
)

var delegatedTrustMu sync.Mutex
var errTrustConfigChanged = errors.New("provider config changed while preparing trust")

// PrepareDelegatedWorkspace answers the native folder trust gate for a delegated
// provider before it starts. Approval bypass does not bypass folder trust. Trust
// is limited to the actual cwd (and its canonical spelling), never HOME or every
// ancestor. Manual Sessions do not call this function; the Brain service calls it
// for a Claude Brain host, which is hidden and so not prepared by the watcher.
//
// Codex is told on its own command line and Mewla writes nothing to the user's
// Codex config. Claude Code has no per-launch way to say it: its own errors name
// projects[<cwd>].hasTrustDialogAccepted in ~/.claude.json as the way to trust a
// folder without the dialog, so that one key is written (see docs/sessions.md).
func PrepareDelegatedWorkspace(command, cwd string, env map[string]string) (string, error) {
	provider := InferWorkerProvider(command)
	if provider != WorkerProviderCodex && provider != WorkerProviderClaude && provider != WorkerProviderCursor {
		return command, nil
	}
	options, ok := inspectLaunchCommandOptions(command)
	if !ok || options.terminated {
		return "", fmt.Errorf("cannot establish delegated workspace for %q", provider)
	}
	if provider == WorkerProviderCursor {
		present, enabled := options.option("", "--disable-auto-update")
		if present && !enabled {
			return "", fmt.Errorf("delegated Cursor requires exact --disable-auto-update")
		}
		if !enabled {
			command += " --disable-auto-update"
		}
		return command, nil
	}
	if provider == WorkerProviderCodex {
		present, dir := options.optionValue("--cd")
		if short, value := options.optionValue("-C"); short {
			present, dir = true, value
		}
		if present {
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(cwd, dir)
			}
			cwd = dir
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve delegated cwd: %w", err)
	}
	dirs := []string{abs}
	if real != abs {
		dirs = append(dirs, real)
	}
	if provider == WorkerProviderCodex {
		present, enabled := options.option("", "--dangerously-bypass-hook-trust")
		if present && !enabled {
			return "", fmt.Errorf("delegated Codex requires exact --dangerously-bypass-hook-trust")
		}
		// Startup self-update consumes Enter and exits instead of admitting the
		// brief. Hook trust is a separate invocation-scoped gate from folder trust.
		if !enabled {
			command += " --dangerously-bypass-hook-trust"
		}
		return command + " -c check_for_update_on_startup=false " + codextrust.Flag(dirs...), nil
	}
	// Direct env wrappers are part of the parsed launch contract too.
	launchEnv := make(map[string]string, len(env))
	for key, value := range env {
		launchEnv[key] = value
	}
	fields, _ := shellquote.Fields(command)
	if len(fields) > 0 && filepath.Base(fields[0]) == "env" {
		for _, field := range fields[1:] {
			if !isLaunchEnvAssignment(field) {
				break
			}
			key, value, _ := strings.Cut(field, "=")
			launchEnv[key] = value
		}
	}
	value := func(key string) string {
		if v, ok := launchEnv[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
	home := value("HOME")
	if home == "" {
		return "", fmt.Errorf("delegated provider HOME is missing")
	}
	delegatedTrustMu.Lock()
	defer delegatedTrustMu.Unlock()
	path := filepath.Join(home, ".claude.json")
	if root := value("CLAUDE_CONFIG_DIR"); root != "" {
		path = filepath.Join(root, ".claude.json")
	}
	if err := updateTrustFile(path, func(raw []byte) ([]byte, error) {
		config := map[string]json.RawMessage{}
		if len(bytes.TrimSpace(raw)) != 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return nil, err
			}
		}
		if config == nil {
			return nil, fmt.Errorf("Claude config must be an object")
		}
		projects := map[string]json.RawMessage{}
		if p := config["projects"]; p != nil {
			if err := json.Unmarshal(p, &projects); err != nil {
				return nil, err
			}
		}
		if projects == nil {
			projects = map[string]json.RawMessage{}
		}
		changed := false
		for _, dir := range dirs {
			project := map[string]json.RawMessage{}
			if p := projects[dir]; p != nil {
				if err := json.Unmarshal(p, &project); err != nil {
					return nil, err
				}
			}
			if project == nil {
				project = map[string]json.RawMessage{}
			}
			if string(project["hasTrustDialogAccepted"]) == "true" {
				continue
			}
			project["hasTrustDialogAccepted"] = json.RawMessage("true")
			projects[dir], _ = json.Marshal(project)
			changed = true
		}
		if !changed {
			return raw, nil
		}
		config["projects"], _ = json.Marshal(projects)
		return json.MarshalIndent(config, "", "  ")
	}); err != nil {
		return "", err
	}
	return command, nil
}

func updateTrustFile(path string, patch func([]byte) ([]byte, error)) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := updateTrustFileOnce(path, patch)
		if !errors.Is(err, errTrustConfigChanged) {
			return err
		}
	}
	return fmt.Errorf("%w repeatedly", errTrustConfigChanged)
}

func updateTrustFileOnce(path string, patch func([]byte) ([]byte, error)) error {
	// Honor an explicitly symlinked config, preserving the symlink itself.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := patch(raw)
	if err != nil {
		return fmt.Errorf("prepare delegated trust in %s: %w", path, err)
	}
	if bytes.Equal(raw, out) {
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mewla-trust-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(out); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if !bytes.Equal(raw, current) {
		return errTrustConfigChanged
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
