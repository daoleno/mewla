package brain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/watcher"
)

// PaneMigrationPaths identifies the other durable owners of Worker references.
type PaneMigrationPaths struct {
	RouteBindings string
	TelegramState string
}

// MigrateWorkerPaneIdentity runs under the daemon's exclusive Brain-root lock,
// before opening the lifecycle engine or serving requests. Each document is
// atomically replaced. The durable alias journal precedes tmux mutation, making
// interruption at any file or option boundary idempotent on the next startup.
// Aliases exist solely for already-running launch shells carrying an old
// MEWLA_WORKER_ID; all stored lifecycle identities and new Workers use %pane_id.
func MigrateWorkerPaneIdentity(root string, w *watcher.Watcher, references PaneMigrationPaths) error {
	state := filepath.Join(root, "state")
	journal := filepath.Join(state, "worker_pane_aliases.json")
	aliases := map[string]string{}
	if raw, err := os.ReadFile(journal); err == nil {
		if err := json.Unmarshal(raw, &aliases); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Restrict migration to known control-plane documents. Provider transcripts,
	// user messages, receipts, tokens and content references are not identities.
	paths := []string{}
	for _, name := range []string{"lifecycle/state.json", "presentation.json", "host_session.json", "host_activation.json", "messages.jsonl"} {
		paths = append(paths, filepath.Join(state, name))
	}
	for _, path := range []string{references.RouteBindings, references.TelegramState} {
		if path != "" {
			paths = append(paths, path)
		}
	}
	documents := map[string][]any{}
	generations := map[string][]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		for {
			var doc any
			if err := dec.Decode(&doc); err == io.EOF {
				break
			} else if err != nil {
				return fmt.Errorf("read pane migration %s: %w", path, err)
			}
			documents[path] = append(documents[path], doc)
			collectPaneGenerations(doc, generations)
		}
	}
	if err := w.MigrateLegacyWorkerPanes(aliases, generations, func(next map[string]string) error { return writeJSONFile(journal, next) }); err != nil {
		return err
	}
	for _, path := range paths {
		changed := false
		for _, doc := range documents[path] {
			changed = rebindPaneReferences(doc, aliases, path == filepath.Join(state, "host_session.json")) || changed
			if path == references.TelegramState {
				changed = rebindTelegramPaneRouting(doc, aliases) || changed
			}
		}
		if !changed {
			continue
		}
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		for _, doc := range documents[path] {
			if err := enc.Encode(doc); err != nil {
				return err
			}
		}
		if err := writePaneMigrationFile(path, out.Bytes()); err != nil {
			return err
		}
	}
	w.SetLegacyWorkerIDs(aliases)
	return nil
}

func collectPaneGenerations(value any, generations map[string][]string) {
	switch v := value.(type) {
	case map[string]any:
		id, _ := v["session_id"].(string)
		generation, _ := v["pane_generation"].(string)
		if id != "" && generation != "" {
			generations[id] = append(generations[id], generation)
		}
		for _, child := range v {
			collectPaneGenerations(child, generations)
		}
	case []any:
		for _, child := range v {
			collectPaneGenerations(child, generations)
		}
	}
}

func rebindPaneReferences(value any, aliases map[string]string, hostFile bool) bool {
	changed := false
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if text, ok := child.(string); ok {
				switch key {
				case "session_id", "host_session_id", "attempt_session_id", "delivery_host_session_id", "admission_echo_session_id", "fallback_session_id":
					if pane := aliases[text]; pane != "" {
						v[key] = pane
						changed = true
					}
				case "id":
					if hostFile {
						if pane := aliases[text]; pane != "" {
							v[key] = pane
							changed = true
						}
					}
				case "ref", "wake_ref":
					for old, pane := range aliases {
						prefix := "session:" + old + ":turn:"
						if strings.HasPrefix(text, prefix) {
							v[key] = "session:" + pane + ":turn:" + strings.TrimPrefix(text, prefix)
							changed = true
							break
						}
					}
				}
			}
			changed = rebindPaneReferences(child, aliases, false) || changed
		}
	case []any:
		for _, child := range v {
			changed = rebindPaneReferences(child, aliases, false) || changed
		}
	}
	return changed
}

func writePaneMigrationFile(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pane-migration-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// Telegram keeps local deduplication checkpoints and reply routes alongside
// explicit Session fields. Rebind only these routing structures, preserving
// message text, bot credentials, callback tokens, timestamps and delivery state.
func rebindTelegramPaneRouting(value any, aliases map[string]string) bool {
	doc, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	rewrite := func(text string) string {
		for old, pane := range aliases {
			for _, prefix := range []string{"session:", "topic:msg:", "topic:mark:", "topic:life:"} {
				head := prefix + old + ":"
				if strings.HasPrefix(text, head) {
					return prefix + pane + ":" + strings.TrimPrefix(text, head)
				}
			}
		}
		return text
	}
	for _, field := range []string{"callback_routes", "reply_sessions"} {
		entries, _ := doc[field].(map[string]any)
		for key, value := range entries {
			if old, ok := value.(string); ok {
				if pane := aliases[old]; pane != "" {
					entries[key] = pane
					changed = true
				}
			}
		}
	}
	if choices, ok := doc["session_choices"].([]any); ok {
		for i, value := range choices {
			if old, ok := value.(string); ok {
				if pane := aliases[old]; pane != "" {
					choices[i] = pane
					changed = true
				}
			}
		}
	}
	for _, field := range []string{"topic_projection", "topic_messages"} {
		entries, _ := doc[field].(map[string]any)
		for key, value := range entries {
			if next := rewrite(key); next != key {
				entries[next] = value
				delete(entries, key)
				changed = true
			}
		}
	}
	rows, _ := doc["outbox"].([]any)
	for _, value := range rows {
		row, _ := value.(map[string]any)
		for _, field := range []string{"id", "canonical_id", "topic_key"} {
			if old, ok := row[field].(string); ok {
				if next := rewrite(old); next != old {
					row[field] = next
					changed = true
				}
			}
		}
	}
	return changed
}
