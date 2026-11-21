package work

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/shellquote"
)

func TestAntigravityProviderIsExactAgyOnly(t *testing.T) {
	for command, want := range map[string]string{
		"agy":                          WorkerProviderAntigravity,
		"/home/u/.local/bin/agy":       WorkerProviderAntigravity,
		"env HOME=/x agy --model m":    WorkerProviderAntigravity,
		"antigravity":                  "", // the desktop app and IDE launcher
		"agy-helper":                   "",
		"magy":                         "",
		"claude --model gemini-4-neon": WorkerProviderClaude,
	} {
		if got := InferWorkerProvider(command); got != want {
			t.Errorf("InferWorkerProvider(%q) = %q, want %q", command, got, want)
		}
	}
	cfg, err := LoadExecutors(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	agy, ok := cfg.WorkerExecutor("agy")
	if !ok || agy.Provider != WorkerProviderAntigravity || agy.Command != "agy" || !agy.Capabilities.NativeResume {
		t.Fatalf("default agy executor = %+v, %v", agy, ok)
	}
	if !agy.Capabilities.StructuredEvents {
		t.Fatalf("agy's conversation is read through ACP; StructuredEvents must be true: %+v", agy.Capabilities)
	}
	if cfg.IDForCommand("agy --model gemini-4-neon-high") != "agy" {
		t.Fatalf("a launch of agy with a model must belong to the agy executor")
	}
}

func TestAntigravityWorkerLaunchIsUnattendedAndSelectsModelById(t *testing.T) {
	cfg := NewExecutorConfig(map[string]Executor{"agy": {Name: "agy", Command: "agy", Kind: "antigravity"}})
	got, err := cfg.ResolveWorkerCommand("agy", "", "gemini-4-neon-high", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "agy --model gemini-4-neon-high --dangerously-skip-permissions" {
		t.Fatalf("command = %q", got)
	}
	if _, err := cfg.ResolveWorkerCommand("agy", "", "gemini-4-neon-high", "high", true); err == nil ||
		!strings.Contains(err.Error(), "no per-launch reasoning option") {
		t.Fatalf("reasoning must be refused (the level is part of the model id), got %v", err)
	}
	for _, command := range []string{
		"agy -p hi", "agy --print hi", "agy --mode auto", "agy --mode=plan", "agy --sandbox", "agy -c",
		"agy --continue", "agy --remote-control", "agy -i hi",
	} {
		_, err := ScheduledActionCommand("agy", Executor{Name: "agy", Command: command, Kind: "antigravity"})
		if !errors.Is(err, ErrScheduledActionUnattended) {
			t.Errorf("%q must not launch unattended, got %v", command, err)
		}
	}
	kept, err := ScheduledActionCommand("agy", Executor{Name: "agy", Command: "agy --dangerously-skip-permissions", Kind: "antigravity"})
	if err != nil || kept != "agy --dangerously-skip-permissions" {
		t.Fatalf("explicit flag kept as is: %q %v", kept, err)
	}
}

func TestAntigravityResumeToken(t *testing.T) {
	const id = "a03fe13b-79b6-4b0b-ab9e-62b8efbcff60"
	got, err := WithProviderResumeToken(WorkerProviderAntigravity, "agy --dangerously-skip-permissions", id)
	if err != nil || got != "agy --dangerously-skip-permissions --conversation "+id {
		t.Fatalf("resume command = %q %v", got, err)
	}
	token, present, err := ProviderResumeToken(WorkerProviderAntigravity, got)
	if err != nil || !present || token != id {
		t.Fatalf("token = %q %v %v", token, present, err)
	}
	if _, err := WithProviderResumeToken(WorkerProviderAntigravity, "agy", "not-a-uuid; rm -rf /"); err == nil {
		t.Fatalf("a token that is not a conversation id must be refused")
	}
	if _, err := WithProviderResumeToken(WorkerProviderAntigravity, got, "b03fe13b-79b6-4b0b-ab9e-62b8efbcff60"); err == nil {
		t.Fatalf("a command that already resumes another conversation must be refused")
	}
}

func TestAntigravityWorkspaceTrustIsInlineAndSessionOnly(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	prepared, err := PrepareDelegatedWorkspace("agy --dangerously-skip-permissions", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := inlineAntigravitySettings(t, prepared)
	trusted, _ := settings["trustedWorkspaces"].([]any)
	if len(trusted) == 0 || trusted[0] != dir {
		t.Fatalf("trustedWorkspaces = %v, want %s first", trusted, dir)
	}
	if settings["toolPermission"] != "always-proceed" || settings["artifactReviewPolicy"] != "always-proceed" {
		t.Fatalf("review gates not opened: %v", settings)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini")); !os.IsNotExist(err) {
		t.Fatalf("trusting a Worker folder must write nothing to the person's agy directory: %v", err)
	}

	// agy keeps only the last --settings: an inline object of the command's own
	// is merged into, never followed by a second one.
	merged, err := PrepareDelegatedWorkspace(`agy --settings '{"modelProvider":"gemini","toolPermission":"strict"}' --dangerously-skip-permissions`, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(merged, "--settings") != 1 {
		t.Fatalf("two --settings in %q", merged)
	}
	settings = inlineAntigravitySettings(t, merged)
	if settings["modelProvider"] != "gemini" || settings["toolPermission"] != "strict" {
		t.Fatalf("the command's own keys must be kept: %v", settings)
	}
	if trusted, _ := settings["trustedWorkspaces"].([]any); len(trusted) == 0 {
		t.Fatalf("cwd not trusted: %v", settings)
	}

	file, err := PrepareDelegatedWorkspace("agy --settings /etc/agy.json", dir, nil)
	if err != nil || file != "agy --settings /etc/agy.json" {
		t.Fatalf("a --settings file is the person's own: %q %v", file, err)
	}
}

func inlineAntigravitySettings(t *testing.T, command string) map[string]any {
	t.Helper()
	fields, ok := shellquote.Fields(command)
	if !ok {
		t.Fatalf("unparseable %q", command)
	}
	for index, field := range fields {
		if field == "--settings" && index+1 < len(fields) {
			var out map[string]any
			if err := json.Unmarshal([]byte(fields[index+1]), &out); err != nil {
				t.Fatalf("--settings is not JSON: %v", err)
			}
			return out
		}
	}
	t.Fatalf("no --settings in %q", command)
	return nil
}

func writeAntigravitySummaries(t *testing.T, home string, rows ...[]any) {
	t.Helper()
	dir := AntigravityDataDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The schema is agy 1.5.14's, copied from a real conversation_summaries.db.
	if _, err := db.Exec("CREATE TABLE `conversation_summaries` (`conversation_id` text,`title` text NOT NULL DEFAULT \"\",`preview` text NOT NULL DEFAULT \"\",`step_count` integer NOT NULL DEFAULT 0,`last_modified_time` datetime NOT NULL,`workspace_uris` text NOT NULL,`status` text NOT NULL DEFAULT \"\",`source` text NOT NULL DEFAULT \"\",`project_id` text NOT NULL DEFAULT \"\",`agent_name` text NOT NULL DEFAULT \"\",`parent_conversation_id` text NOT NULL DEFAULT \"\",`nesting_depth` integer NOT NULL DEFAULT 0,`not_fully_idle` numeric NOT NULL DEFAULT false,`killed` numeric NOT NULL DEFAULT false, PRIMARY KEY (`conversation_id`))"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec("INSERT INTO conversation_summaries (conversation_id, preview, last_modified_time, workspace_uris, status, parent_conversation_id, not_fully_idle) VALUES (?,?,?,?,?,?,?)", row...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindAntigravityConversationBindsByFolderAndLaunchTime(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	other := t.TempDir()
	start := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	uri := func(dir string) string { b, _ := json.Marshal([]string{"file://" + dir}); return string(b) }
	writeAntigravitySummaries(t, home,
		[]any{"a03fe13b-79b6-4b0b-ab9e-62b8efbcff60", "earlier", "2026-11-22 02:00:00.5+00:00", uri(cwd), "CASCADE_RUN_STATUS_IDLE", "", false},
		[]any{"b03fe13b-79b6-4b0b-ab9e-62b8efbcff60", "elsewhere", "2026-11-22 03:05:00.5+00:00", uri(other), "CASCADE_RUN_STATUS_IDLE", "", false},
		[]any{"c03fe13b-79b6-4b0b-ab9e-62b8efbcff60", "subagent", "2026-11-22 03:06:00.5+00:00", uri(cwd), "CASCADE_RUN_STATUS_IDLE", "a03fe13b-79b6-4b0b-ab9e-62b8efbcff60", false},
	)
	if _, found, err := FindAntigravityConversation(home, cwd, start); err != nil || found {
		t.Fatalf("an earlier conversation, another folder and a sub-agent must not bind: found=%v err=%v", found, err)
	}
	writeAntigravitySummaries2(t, home, "d03fe13b-79b6-4b0b-ab9e-62b8efbcff60", "03:10:00", uri(cwd))
	got, found, err := FindAntigravityConversation(home, cwd, start)
	if err != nil || !found || got.ID != "d03fe13b-79b6-4b0b-ab9e-62b8efbcff60" || got.Idle() {
		t.Fatalf("got %+v found=%v err=%v", got, found, err)
	}
	if none, err := ListAntigravityConversations(t.TempDir()); err != nil || none != nil {
		t.Fatalf("no agy directory is no conversations, got %v %v", none, err)
	}
}

func writeAntigravitySummaries2(t *testing.T, home, id, clock, uris string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(AntigravityDataDir(home), "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("INSERT INTO conversation_summaries (conversation_id, preview, last_modified_time, workspace_uris, status, parent_conversation_id, not_fully_idle) VALUES (?,?,?,?,?,?,?)",
		id, "now", "2026-11-22 "+clock+".25+00:00", uris, "CASCADE_RUN_STATUS_RUNNING", "", true); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAntigravityWorkerTranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir()
	uri := func(dir string) string { b, _ := json.Marshal([]string{"file://" + dir}); return string(b) }
	const first = "d03fe13b-79b6-4b0b-ab9e-62b8efbcff60"
	const second = "e03fe13b-79b6-4b0b-ab9e-62b8efbcff60"
	writeAntigravitySummaries(t, home)
	if err := os.MkdirAll(filepath.Join(AntigravityDataDir(home), "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first, second} {
		if err := os.WriteFile(antigravityConversationPath(home, id), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	worker := classifier.Worker{ID: "s1", Command: "agy", Cwd: cwd, StartedAt: started, Name: "agy"}

	if got := ResolveAntigravityWorkerTranscript(worker); got.Bound() {
		t.Fatalf("no conversation yet, got %+v", got)
	}
	writeAntigravitySummaries2(t, home, first, "03:10:00", uri(cwd))
	got := ResolveAntigravityWorkerTranscript(worker)
	if !got.Bound() || got.SessionID != first || got.Provider != WorkerProviderAntigravity {
		t.Fatalf("one conversation since launch must bind, got %+v", got)
	}
	// A second Worker in the same folder makes "newest since launch" a guess:
	// the Worker stays unbound, where the Brain host's own lookup still takes
	// the newest.
	writeAntigravitySummaries2(t, home, second, "03:11:00", uri(cwd))
	if got := ResolveAntigravityWorkerTranscript(worker); got.Bound() {
		t.Fatalf("two conversations in the folder must not bind a Worker, got %+v", got)
	}
	if got := resolveAntigravityTranscriptIdentity(worker); !got.Bound() {
		t.Fatalf("the Brain host lookup keeps taking the newest, got %+v", got)
	}
	// The id on the command line is exact, whatever else the folder holds.
	resumed := worker
	resumed.Command = "agy --conversation " + first
	if got := ResolveAntigravityWorkerTranscript(resumed); got.SessionID != first {
		t.Fatalf("resume id must bind, got %+v", got)
	}
}
