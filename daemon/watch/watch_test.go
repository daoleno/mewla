package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/connections"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

// Secret fixtures are assembled at runtime so no literal key shape lives in
// the repository.
func fixtureSecrets() map[string]string {
	hex64 := strings.Repeat("0123456789abcdef", 4)
	return map[string]string{
		"openai-compatible": "sk-" + hex64,
		"anthropic":         "sk-" + "ant-" + "api03-" + strings.Repeat("Ab3", 12),
		"github":            "gh" + "p_" + strings.Repeat("A1b2", 9),
		"github-pat":        "github_" + "pat_" + strings.Repeat("11AB", 8),
		"slack":             "xo" + "xb-" + "1234567890-abcdefghij",
		"aws":               "AK" + "IA" + "ABCDEFGHIJ234567",
		"jwt":               "ey" + "JhbGciOiJIUzI1NiJ9." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0." + "dozjgNryP4J3jVmNHl0w5N",
		"fal":               "1b2c3d4e-1111-2222-3333-444455556666:" + strings.Repeat("ab12", 8),
		"assignment":        "api_key=" + "Zq9" + strings.Repeat("x7", 10),
	}
}

func TestRedactRemovesKnownSecretShapes(t *testing.T) {
	for name, secret := range fixtureSecrets() {
		text := "please use key:" + secret + " for this"
		got := Redact(text)
		value := secret
		if name == "assignment" {
			value = strings.TrimPrefix(secret, "api_key=")
		}
		if strings.Contains(got, value) {
			t.Errorf("%s: secret survived redaction: %q", name, got)
		}
	}
	for _, keep := range []string{"task-notification handling", "risk-assessment-report-final", "https://github.com/daoleno/mewla"} {
		if got := Redact(keep); got != keep {
			t.Errorf("Redact(%q) = %q, want unchanged", keep, got)
		}
	}
	if got := Redact("https://x-access-token:" + "abc123secret@github.com/o/r"); strings.Contains(got, "abc123secret") {
		t.Errorf("URL credentials survived: %q", got)
	}
}

func gitRepo(t *testing.T, root string, remote string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := "[core]\n\tbare = false\n"
	if remote != "" {
		config += "[remote \"origin\"]\n\turl = " + remote + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectInference(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "code", "alpha")
	gitRepo(t, repo, "https://user:"+"hunter2pass@github.com/acme/alpha.git")
	nested := filepath.Join(repo, "app", "src")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	// A linked worktree under the worktree root, as git lays it out.
	worktree := filepath.Join(base, "worktrees", "alpha-fix")
	meta := filepath.Join(repo, ".git", "worktrees", "alpha-fix")
	for _, dir := range []string{worktree, meta} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+meta+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "commondir"), []byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ids := map[string]string{}
	for _, cwd := range []string{repo, nested, filepath.Join(nested, "gone", "deeper"), worktree} {
		id, ok := store.ProjectForCWD(cwd, at)
		if !ok {
			t.Fatalf("no project for %s", cwd)
		}
		ids[cwd] = id
	}
	for cwd, id := range ids {
		if id != ids[repo] {
			t.Errorf("%s mapped to %s, want %s", cwd, id, ids[repo])
		}
	}
	if _, ok := store.ProjectForCWD(filepath.Join(base, "not-a-repo"), at); ok {
		t.Errorf("a directory outside any repository got a project")
	}
	projects := store.Projects()
	if len(projects) != 1 || projects[0].Name != "alpha" || projects[0].Root != repo {
		t.Fatalf("projects = %+v", projects)
	}
	if remote := projects[0].Remotes[0].URL; strings.Contains(remote, "hunter2pass") {
		t.Errorf("remote kept its credentials: %q", remote)
	}
	if owner, name, ok := githubRemoteSlug(projects[0].Remotes, nil); !ok || owner != "acme" || name != "alpha" {
		t.Errorf("github slug = %q/%q %v", owner, name, ok)
	}
	for _, remote := range []string{"git@github.com:acme/alpha.git", "ssh://git@github.com/acme/alpha", "https://github.com/acme/alpha"} {
		if owner, name, ok := githubSlug(remote, nil); !ok || owner != "acme" || name != "alpha" {
			t.Errorf("githubSlug(%q) = %q/%q %v", remote, owner, name, ok)
		}
	}
	sshDir := filepath.Join(base, ".ssh")
	_ = os.MkdirAll(sshDir, 0o700)
	_ = os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host github-work\n  HostName github.com\n  IdentityFile ~/.ssh/work\n"), 0o600)
	if owner, name, ok := githubSlug("git@github-work:acme/alpha.git", sshHostNames(base)); !ok || owner != "acme" || name != "alpha" {
		t.Errorf("ssh alias remote = %q/%q %v", owner, name, ok)
	}
	if _, _, ok := githubSlug("git@gitlab.com:acme/alpha.git", sshHostNames(base)); ok {
		t.Errorf("a GitLab remote was taken for GitHub")
	}

	if _, err := (&Collector{store: store}).Handle(context.Background(), Request{Action: "rename", ProjectID: ids[repo], Name: "Alpha App"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Projects()[0]; got.Name != "Alpha App" || got.ID != ids[repo] {
		t.Errorf("after rename and reopen: %+v", got)
	}
}

func TestStoreAppendIsIdempotentAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	signals := []Signal{
		{ID: "a", Source: SourceHuman, Kind: kindUserMessage, Time: time.Now(), Excerpt: "one"},
		{ID: "b", Source: SourceHuman, Kind: kindUserMessage, Time: time.Now(), Excerpt: "two"},
	}
	if added, err := store.Append(signals); err != nil || added != 2 {
		t.Fatalf("first append = %d, %v", added, err)
	}
	if added, _ := store.Append(signals); added != 0 {
		t.Fatalf("second append added %d", added)
	}
	// A torn final line from a crash is ignored on reopen.
	file, _ := os.OpenFile(filepath.Join(dir, signalsFile), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = file.WriteString(`{"id":"c","sour`)
	_ = file.Close()
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SignalCount() != 2 {
		t.Fatalf("reopened count = %d", reopened.SignalCount())
	}
	if added, _ := reopened.Append(signals); added != 0 {
		t.Fatalf("append after reopen added %d", added)
	}
}

type claudeLine map[string]any

func writeLines(t *testing.T, path string, lines []claudeLine, partial string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		raw, _ := json.Marshal(line)
		_, _ = file.Write(append(raw, '\n'))
	}
	_, _ = file.WriteString(partial)
}

func claudeMessage(role, uuid, at, cwd string, content any) claudeLine {
	return claudeLine{
		"type": role, "uuid": uuid, "timestamp": at, "sessionId": "sess-1", "cwd": cwd,
		"message": map[string]any{"role": role, "content": content},
	}
}

func newTestCollector(t *testing.T, home string) (*Collector, *Store) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, ".pi", "agent"))
	t.Setenv("MEWLA_STATE_DIR", filepath.Join(home, ".mewla"))
	t.Setenv("MEWLA_OPENCODE_DB", filepath.Join(home, "none.db"))
	store, err := OpenStore(filepath.Join(home, ".mewla", "watch"))
	if err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(Options{Store: store, Home: home})
	collector.pause = func() {}
	return collector, store
}

func TestHumanCollectorIsIncrementalTaggedAndRedacted(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "work", "beta")
	gitRepo(t, repo, "git@github.com:acme/beta.git")
	collector, store := newTestCollector(t, home)
	now := time.Now().UTC()
	stamp := func(minutes int) string {
		return now.Add(time.Duration(minutes-60) * time.Minute).Format(time.RFC3339Nano)
	}
	secrets := fixtureSecrets()
	transcript := filepath.Join(home, ".claude", "projects", "-work-beta", "sess-1.jsonl")
	writeLines(t, transcript, []claudeLine{
		claudeMessage("user", "u1", stamp(1), repo, "Read BRIEF.md.\n\nMewla lifecycle protocol:\nProfile: implementation. --turn-id turn:11111111-2222-3333-4444-555555555555"),
		claudeMessage("assistant", "a1", stamp(2), repo, []any{map[string]any{"type": "text", "text": "I rebuilt the APK and installed it."}}),
		claudeMessage("user", "u2", stamp(3), repo, "为什么要 build APK? use key:"+secrets["openai-compatible"]+" and "+secrets["anthropic"]),
		claudeMessage("user", "u3", stamp(4), repo, "<mewla_work_event>\n{\"event_id\":\"x\",\"objective\":\"Mewla lifecycle protocol: ...\",\"ref\":\"turn:99999999-2222-3333-4444-555555555555\"}\n</mewla_work_event>"),
		claudeMessage("user", "u3b", stamp(4), repo, "This session is being continued from a previous conversation that ran out of context."),
		claudeMessage("user", "u3c", stamp(4), repo, "Brain Host activation contract:\nRead AGENTS.md before continuing."),
	}, `{"type":"user","uuid":"u4","timestamp":"`+stamp(5)+`","message":{"role":"user","content":"half writ`)

	pass := collector.Collect(context.Background(), []string{SourceHuman}, true)
	if pass.Added[SourceHuman] != 2 {
		t.Fatalf("first pass added %d: %+v", pass.Added[SourceHuman], store.Signals(Filter{}))
	}
	signals := store.Signals(Filter{Source: SourceHuman})
	byKind := map[string]Signal{}
	for _, signal := range signals {
		byKind[signal.Kind] = signal
	}
	user, delegated := byKind[kindUserMessage], byKind[kindDelegatedMessage]
	if user.ID == "" || delegated.ID == "" {
		t.Fatalf("want one user and one delegated message, got %+v", signals)
	}
	if user.Refs["answered"] != "I rebuilt the APK and installed it." || user.Refs["author"] != "user" || user.Refs["session_role"] != "worker" {
		t.Errorf("user refs = %+v", user.Refs)
	}
	if user.ProjectID == "" || user.ProjectID != delegated.ProjectID {
		t.Errorf("project ids: user %q delegated %q", user.ProjectID, delegated.ProjectID)
	}
	if store.PlaceOf("turn", "11111111-2222-3333-4444-555555555555") != repo {
		t.Errorf("turn was not placed in the repository")
	}
	if store.PlaceOf("turn", "99999999-2222-3333-4444-555555555555") != "" {
		t.Errorf("a turn merely mentioned in a Work event was placed")
	}

	if again := collector.Collect(context.Background(), []string{SourceHuman}, true); again.Added[SourceHuman] != 0 {
		t.Fatalf("re-run added %d", again.Added[SourceHuman])
	}

	// Completing the torn line and adding a turn collects exactly those two.
	writeLines(t, transcript, nil, `"}}`+"\n")
	writeLines(t, transcript, []claudeLine{claudeMessage("user", "u5", stamp(6), repo, "不要再 build APK 了")}, "")
	if next := collector.Collect(context.Background(), []string{SourceHuman}, true); next.Added[SourceHuman] != 2 {
		t.Fatalf("incremental pass added %d", next.Added[SourceHuman])
	}

	raw, err := os.ReadFile(filepath.Join(home, ".mewla", "watch", signalsFile))
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range secrets {
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), secret[:20]) {
			t.Errorf("%s secret reached the signal store", name)
		}
	}
}

func TestHumanCollectorReadsCodex(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "work", "gamma")
	gitRepo(t, repo, "")
	collector, store := newTestCollector(t, home)
	now := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "10", "12", "rollout-2026-10-12T00-00-00-abc.jsonl")
	writeLines(t, rollout, []claudeLine{
		{"timestamp": now, "type": "session_meta", "payload": map[string]any{"id": "codex-1", "cwd": repo}},
		{"timestamp": now, "type": "event_msg", "payload": map[string]any{"type": "agent_message", "message": "Done: I added a settings page."}},
		{"timestamp": now, "type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "这个应该放在现有的设置里，不要新页面"}},
	}, "")
	pass := collector.Collect(context.Background(), []string{SourceHuman}, true)
	signals := store.Signals(Filter{Source: SourceHuman})
	if pass.Added[SourceHuman] != 1 || len(signals) != 1 {
		t.Fatalf("codex pass added %d: %+v", pass.Added[SourceHuman], signals)
	}
	if signals[0].Refs["provider"] != "codex" || signals[0].ProjectID == "" || !strings.Contains(signals[0].Refs["answered"], "settings page") {
		t.Errorf("codex signal = %+v", signals[0])
	}
}

type fakeWorks struct {
	works  []brain.Work
	events []brain.WorkEvent
}

func (f *fakeWorks) ListWork() ([]brain.Work, error) { return f.works, nil }
func (f *fakeWorks) ListWorkEvents(string) ([]brain.WorkEvent, error) {
	return f.events, nil
}

type fakeAdmissions []*lifecycle.State

func (f fakeAdmissions) ListStates() []*lifecycle.State { return f }

func TestProcessCollectorPlacesWorkAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "work", "delta")
	gitRepo(t, repo, "")
	collector, store := newTestCollector(t, home)
	turnA := "aaaaaaaa-0000-0000-0000-000000000001"
	turnB := "aaaaaaaa-0000-0000-0000-000000000002"
	store.Place("turn", turnA, repo)
	at := time.Now().UTC().Add(-2 * time.Hour)
	works := &fakeWorks{
		works: []brain.Work{{ID: "w1", Title: "fix login"}},
		events: []brain.WorkEvent{
			{ID: "e1", WorkID: "w1", Kind: "session.running", Sequence: 1, CreatedAt: at},
			{ID: "e2", WorkID: "w1", Kind: "turn_lost", PayloadRef: "turn:" + turnA, Sequence: 2, CreatedAt: at.Add(time.Minute), Summary: "Worker turn lost"},
			{ID: "e3", WorkID: "w1", Kind: "session.stale", Sequence: 3, CreatedAt: at.Add(2 * time.Minute)},
			{ID: "e4", WorkID: "w1", Kind: "brain.work_closed", Sequence: 4, CreatedAt: at.Add(3 * time.Minute)},
			{ID: "e5", WorkID: "w1", Kind: "user.input", Sequence: 5, CreatedAt: at.Add(4 * time.Minute)},
		},
	}
	collector.works = works
	collector.admissions = fakeAdmissions{{
		ID: "w1", Title: "fix login",
		Admissions: map[lifecycle.TurnToken]*lifecycle.AdmissionState{
			lifecycle.TurnToken("%5:turn:" + turnA): {TurnToken: lifecycle.TurnToken("%5:turn:" + turnA), SessionID: "%5", Status: lifecycle.AdmissionAccepted, AttemptedAt: at},
			lifecycle.TurnToken("%5:turn:" + turnB): {TurnToken: lifecycle.TurnToken("%5:turn:" + turnB), SessionID: "%5", Status: lifecycle.AdmissionAccepted, AttemptedAt: at.Add(5 * time.Minute)},
		},
	}}
	pass := collector.Collect(context.Background(), []string{SourceProcess}, true)
	kinds := map[string]int{}
	for _, signal := range store.Signals(Filter{Source: SourceProcess}) {
		kinds[signal.Kind]++
		if signal.ProjectID == "" || signal.Refs["work"] != "w1" {
			t.Errorf("unplaced process signal %+v", signal)
		}
	}
	want := map[string]int{"turn_lost": 1, "stalled": 1, kindReopened: 1, kindFollowUp: 1}
	if fmt.Sprint(kinds) != fmt.Sprint(want) || pass.Added[SourceProcess] != 4 {
		t.Fatalf("kinds = %v (added %d), want %v", kinds, pass.Added[SourceProcess], want)
	}
	if again := collector.Collect(context.Background(), []string{SourceProcess}, true); again.Added[SourceProcess] != 0 {
		t.Fatalf("re-run added %d", again.Added[SourceProcess])
	}
	problems := store.Problems(Filter{Kind: RuleWorkerStalls})
	if len(problems) != 1 || problems[0].Count != 2 || problems[0].Status != StatusNew || !problems[0].Active {
		t.Fatalf("worker stall problems = %+v", problems)
	}
}

func TestRuntimeCollectorFollowsRotationAndFlagsRepeats(t *testing.T) {
	home := t.TempDir()
	collector, store := newTestCollector(t, home)
	logPath := filepath.Join(home, ".mewla", "logs", "daemon.log")
	collector.logPath = logPath
	line := func(at time.Time, message string) string {
		return at.Local().Format("2006/01/02 15:04:05") + " " + message + "\n"
	}
	base := time.Now().Add(-time.Hour)
	appendLog := func(path, text string) {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		file, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		_, _ = file.WriteString(text)
		_ = file.Close()
	}
	appendLog(logPath, line(base, "WARN telegram poll failed for session %12: timeout after 30s"))
	if pass := collector.Collect(context.Background(), []string{SourceRuntime}, true); pass.Added[SourceRuntime] != 1 {
		t.Fatalf("first pass added %d", pass.Added[SourceRuntime])
	}
	for index := 1; index <= 3; index++ {
		appendLog(logPath, line(base.Add(time.Duration(index)*time.Minute), fmt.Sprintf("WARN telegram poll failed for session %%%d: timeout after %ds", index, 30+index)))
	}
	// Rotate mid-stream: the unread tail of the old file must still be read.
	if err := os.Rename(logPath, logPath+".1"); err != nil {
		t.Fatal(err)
	}
	appendLog(logPath, line(base.Add(5*time.Minute), "WARN telegram poll failed for session %99: timeout after 41s"))
	appendLog(logPath, line(base.Add(6*time.Minute), "plugin catalog could not be saved"))
	if pass := collector.Collect(context.Background(), []string{SourceRuntime}, true); pass.Added[SourceRuntime] != 5 {
		t.Fatalf("rotation pass added %d", pass.Added[SourceRuntime])
	}
	if pass := collector.Collect(context.Background(), []string{SourceRuntime}, true); pass.Added[SourceRuntime] != 0 {
		t.Fatalf("re-run added %d", pass.Added[SourceRuntime])
	}
	signals := store.Signals(Filter{Source: SourceRuntime})
	signatures := map[string]int{}
	for _, signal := range signals {
		signatures[signal.Refs["signature"]]++
		if signal.ProjectID != RuntimeProjectID {
			t.Errorf("runtime signal in project %q", signal.ProjectID)
		}
	}
	if len(signatures) != 2 {
		t.Fatalf("signatures = %v", signatures)
	}
	problems := store.Problems(Filter{Kind: RuleRepeatedError})
	if len(problems) != 1 || problems[0].Count != 5 {
		t.Fatalf("repeated error problems = %+v", problems)
	}
}

type fakeExternal struct {
	responses map[string]connections.WatchResponse
	linear    map[string]string
	calls     []string
}

func (f *fakeExternal) WatchAccounts(integration string) []connections.Account {
	return []connections.Account{{ID: integration + "-1", Name: integration + " account", Integration: integration}}
}

func (f *fakeExternal) WatchGitHub(_ context.Context, _ string, path string, _ url.Values) (connections.WatchResponse, error) {
	f.calls = append(f.calls, path)
	response, ok := f.responses[path]
	if !ok {
		return connections.WatchResponse{Status: 404, RateRemaining: -1}, fmt.Errorf("GitHub returned HTTP 404")
	}
	if response.Status >= 300 {
		return response, fmt.Errorf("GitHub returned HTTP %d", response.Status)
	}
	return response, nil
}

func (f *fakeExternal) WatchLinear(_ context.Context, _ string, tool string, args map[string]any) ([]byte, error) {
	f.calls = append(f.calls, "linear:"+tool)
	return []byte(f.linear[tool]), nil
}

func TestExternalCollectorFlagsRedCIAndSuggestsLinear(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "work", "perpetuo")
	gitRepo(t, repo, "git@github.com:acme/perpetuo.git")
	collector, store := newTestCollector(t, home)
	if _, ok := store.ProjectForCWD(repo, time.Now()); !ok {
		t.Fatal("no project")
	}
	now := time.Now().UTC()
	run := func(id int, conclusion string, ago time.Duration) map[string]any {
		return map[string]any{"id": id, "name": "CI", "display_title": "commit " + fmt.Sprint(id), "head_branch": "main",
			"status": "completed", "conclusion": conclusion, "run_attempt": 1, "updated_at": now.Add(-ago).Format(time.RFC3339)}
	}
	body := func(value any) []byte { raw, _ := json.Marshal(value); return raw }
	external := &fakeExternal{
		responses: map[string]connections.WatchResponse{
			"/repos/acme/perpetuo":              {Status: 200, RateRemaining: 4000, Body: body(map[string]any{"default_branch": "main"})},
			"/repos/acme/perpetuo/actions/runs": {Status: 200, RateRemaining: 4000, Body: body(map[string]any{"workflow_runs": []any{run(1, "success", 5*time.Hour), run(2, "failure", 3*time.Hour), run(3, "failure", time.Hour)}})},
			"/repos/acme/perpetuo/issues": {Status: 200, RateRemaining: 4000, Body: body([]any{
				map[string]any{"number": 7, "title": "Login broken", "body": "steps…", "updated_at": now.Add(-time.Hour).Format(time.RFC3339), "user": map[string]any{"login": "teammate"}},
				map[string]any{"number": 8, "title": "Add invoices", "updated_at": now.Add(-time.Hour).Format(time.RFC3339), "pull_request": map[string]any{}},
			})},
			"/repos/acme/perpetuo/dependabot/alerts": {Status: 403, RateRemaining: 4000},
		},
		linear: map[string]string{"list_teams": `{"teams":[{"id":"t1","key":"PER","name":"Perpetuo"},{"id":"t2","key":"OPS","name":"Operations"}]}`},
	}
	collector.external = external
	collector.Collect(context.Background(), []string{SourceExternal}, true)

	kinds := map[string]int{}
	for _, signal := range store.Signals(Filter{Source: SourceExternal}) {
		kinds[signal.Kind]++
	}
	if kinds[kindCIFailed] != 2 || kinds[kindIssueActivity] != 1 || kinds[kindPRActivity] != 1 || kinds[kindCIPassed] != 0 {
		t.Fatalf("external kinds = %v", kinds)
	}
	problems := store.Problems(Filter{Kind: RuleCIRed})
	if len(problems) != 1 || !problems[0].Active || problems[0].Count != 2 || !problems[0].Severe {
		t.Fatalf("ci problems = %+v", problems)
	}
	project := store.Projects()[0]
	if project.GitHub == nil || project.GitHub.Endpoints["dependabot"] != "HTTP 403" || project.GitHub.Endpoints["actions"] != "ok" {
		t.Errorf("endpoints = %+v", project.GitHub)
	}
	if project.Linear == nil || len(project.Linear.Suggested) != 1 || project.Linear.Suggested[0].Key != "PER" || project.Linear.Confirmed != nil {
		t.Fatalf("linear = %+v", project.Linear)
	}

	// CI goes green: the problem becomes inactive.
	external.responses["/repos/acme/perpetuo/actions/runs"] = connections.WatchResponse{Status: 200, RateRemaining: 4000,
		Body: body(map[string]any{"workflow_runs": []any{run(3, "failure", time.Hour), run(4, "success", time.Minute)}})}
	collector.Collect(context.Background(), []string{SourceExternal}, true)
	if problems := store.Problems(Filter{Kind: RuleCIRed}); len(problems) != 1 || problems[0].Active {
		t.Fatalf("after green CI: %+v", problems)
	}

	// Confirming the Linear team starts its collection.
	if _, err := collector.Handle(context.Background(), Request{Action: "link_linear", ProjectID: project.ID, TeamKey: "OPS"}); err == nil {
		t.Fatal("confirming an unsuggested team succeeded")
	}
	if _, err := collector.Handle(context.Background(), Request{Action: "link_linear", ProjectID: project.ID, TeamKey: "per"}); err != nil {
		t.Fatal(err)
	}
	external.linear["list_issues"] = `[{"id":"i1","identifier":"PER-12","title":"Invoice totals wrong","url":"https://linear.app/x/issue/PER-12","updatedAt":"` + now.Add(-time.Hour).Format(time.RFC3339) + `"}]`
	external.linear["list_comments"] = `{"comments":[{"id":"c1","body":"Still broken for EUR","updatedAt":"` + now.Add(-30*time.Minute).Format(time.RFC3339) + `"}]}`
	collector.Collect(context.Background(), []string{SourceExternal}, true)
	if got := store.Signals(Filter{Kind: kindLinearIssue}); len(got) != 1 || got[0].ProjectID != project.ID {
		t.Fatalf("linear issues = %+v", got)
	}
	if got := store.Signals(Filter{Kind: kindLinearComment}); len(got) != 1 {
		t.Fatalf("linear comments = %+v", got)
	}
}

func TestLogSignatureIgnoresVariableParts(t *testing.T) {
	a := LogSignature(`WARN send to %12 failed after 30s: open /home/x/.mewla/run/a.sock: refused (id 9f3c2a1b)`)
	b := LogSignature(`WARN send to %977 failed after 4s: open /tmp/other.sock: refused (id 00aa11bb22)`)
	if a != b {
		t.Errorf("signatures differ:\n%s\n%s", a, b)
	}
}

func TestCancelledPassRunsNoLaterSource(t *testing.T) {
	home := t.TempDir()
	collector, store := newTestCollector(t, home)
	collector.works = &fakeWorks{events: []brain.WorkEvent{{ID: "e1", WorkID: "w1", Kind: "turn_lost", Sequence: 1, CreatedAt: time.Now()}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector.Collect(ctx, nil, true)
	if count := store.SignalCount(); count != 0 {
		t.Fatalf("a cancelled pass stored %d signals", count)
	}
}
