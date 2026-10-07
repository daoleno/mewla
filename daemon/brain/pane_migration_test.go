package brain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/lifecycle"
	"github.com/daoleno/mewla/daemon/watcher"
)

func TestPaneMigrationPreservesRunningTurnAcrossRestart(t *testing.T) {
	binary, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	// Every command, including production migration, selects this owned socket.
	socket := filepath.Join(t.TempDir(), "tmux.sock")
	tm := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(binary, append([]string{"-S", socket, "-f", "/dev/null"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("scratch tmux %s: %v: %s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	owned := tm("new-session", "-d", "-s", "zen-worker-migration", "-P", "-F", "#{pane_id}", "exec /bin/sh")
	t.Cleanup(func() { _ = exec.Command(binary, "-S", socket, "kill-server").Run() })
	old := tm("display-message", "-p", "-t", owned, "#{session_name}:#{window_id}")
	tm("set-option", "-w", "-t", owned, "@zen_worker_created", "1")
	tm("set-option", "-w", "-t", owned, "@zen_worker_delegated", "1")
	other := tm("split-window", "-h", "-t", owned, "-P", "-F", "#{pane_id}", "exec /bin/sh")
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSessionID(old); err != nil {
		t.Fatal(err)
	}
	generation := fmt.Sprintf("%x", sha256.Sum256([]byte(owned)))
	if err := store.MarkHostActivation(HostActivation{SessionID: old, PaneGeneration: generation, HostGeneration: "same-process-and-pane", ProcessIdentity: "same-process", ActivatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	engine := store.FSM()
	if _, err := engine.DefineWork("pane-work", lifecycle.DefineWorkInput{Title: "migration", Objective: "preserve turn", Policy: lifecycle.PolicyBounded, SourceThreadID: "migration-thread"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.AdmitTurn("pane-work", lifecycle.AdmitTurnInput{SessionID: old, TurnToken: "same-turn", Delegated: true}); err != nil {
		t.Fatal(err)
	}
	references := PaneMigrationPaths{RouteBindings: filepath.Join(root, "routes.json"), TelegramState: filepath.Join(root, "telegram.json")}
	route := map[string]any{"schema_version": 4, "routes": []any{map[string]any{"session_id": old, "route_id": "keep-route", "codex_control_socket": "keep-socket", "launched": map[string]any{"session_id": old}}}}
	topicKey := "topic:msg:" + old + ":reply:0"
	telegram := map[string]any{"schema": 4, "next_offset": json.Number("9007199254740993"), "fallback_session_id": old, "topics": []any{map[string]any{"session_id": old}}, "reply_sessions": map[string]any{"99": old}, "callback_routes": map[string]any{"opaque-token": old}, "session_choices": []any{old}, "topic_projection": map[string]any{topicKey: "same-digest"}, "topic_messages": map[string]any{topicKey: 99}, "outbox": []any{map[string]any{"id": topicKey, "session_id": old, "topic_key": topicKey, "canonical_id": "session:" + old + ":reply", "state": "sent", "text": old}}}
	if err := writeJSONFile(references.RouteBindings, route); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(references.TelegramState, telegram); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		w := watcher.New(time.Second)
		w.SetTmuxServer(socket, "")
		if err := MigrateWorkerPaneIdentity(root, w, references); err != nil {
			t.Fatal(err)
		}
		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		state, err := reopened.FSM().State("pane-work")
		if err != nil {
			t.Fatal(err)
		}
		if state.Status != lifecycle.StatusRunning || state.Attempt == nil || state.Attempt.SessionID != owned || state.Attempt.TurnToken != "same-turn" {
			t.Fatalf("migration changed running turn: %+v", state)
		}
		host, err := reopened.HostSession()
		if err != nil || host.ID != owned {
			t.Fatalf("host=%+v err=%v", host, err)
		}
		activation, err := reopened.HostActivation()
		if err != nil || activation.SessionID != owned || activation.HostGeneration != "same-process-and-pane" {
			t.Fatalf("activation=%+v err=%v", activation, err)
		}
		routeRaw, _ := os.ReadFile(references.RouteBindings)
		var migratedRoute struct {
			Routes []struct {
				SessionID string `json:"session_id"`
				RouteID   string `json:"route_id"`
				Launched  struct {
					SessionID string `json:"session_id"`
				} `json:"launched"`
			} `json:"routes"`
		}
		if err := json.Unmarshal(routeRaw, &migratedRoute); err != nil {
			t.Fatal(err)
		}
		if len(migratedRoute.Routes) != 1 || migratedRoute.Routes[0].SessionID != owned || migratedRoute.Routes[0].Launched.SessionID != owned || migratedRoute.Routes[0].RouteID != "keep-route" {
			t.Fatalf("route binding changed: %s", routeRaw)
		}
		telegramRaw, _ := os.ReadFile(references.TelegramState)
		var migratedTelegram map[string]any
		if err := json.Unmarshal(telegramRaw, &migratedTelegram); err != nil {
			t.Fatal(err)
		}
		replies := migratedTelegram["reply_sessions"].(map[string]any)
		messages := migratedTelegram["topic_messages"].(map[string]any)
		row := migratedTelegram["outbox"].([]any)[0].(map[string]any)
		if replies["99"] != owned || messages["topic:msg:"+owned+":reply:0"] != float64(99) || row["text"] != old || row["state"] != "sent" || !strings.Contains(string(telegramRaw), "9007199254740993") {
			t.Fatal("routing migration changed delivery state or lost deduplication")
		}
		if !w.HasSession(owned) || w.HasSession(other) || w.CanonicalWorkerID(old) != owned {
			t.Fatal("restart did not preserve exact pane ownership")
		}
	}
	t.Logf("reopened twice: running turn=same-turn pane=%s; focused sibling=%s stays unowned", owned, other)
}

func TestPaneReferenceMigrationPreservesOpaqueContent(t *testing.T) {
	old := "worker:@7"
	pane := "%9"
	doc := map[string]any{"session_id": old, "provider_session_id": old, "body": old, "receipt": old, "turn_id": old, "ref": "session:" + old + ":turn:abc", "sequence": uint64(9007199254740993)}
	if !rebindPaneReferences(doc, map[string]string{old: pane}, false) {
		t.Fatal("no migration")
	}
	if doc["session_id"] != pane || doc["ref"] != "session:"+pane+":turn:abc" {
		t.Fatal(doc)
	}
	for _, key := range []string{"provider_session_id", "body", "receipt", "turn_id"} {
		if doc[key] != old {
			t.Fatalf("rewrote %s", key)
		}
	}
	if rebindPaneReferences(doc, map[string]string{old: pane}, false) {
		t.Fatal("migration not idempotent")
	}
}
