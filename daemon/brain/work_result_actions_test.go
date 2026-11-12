package brain

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

func gitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A result offers only what is there: the PR it links, its project's
// changes (on the current result only), files it names that exist, and the
// Work's own worklog. Nothing is derived from Brain's next action.
func TestWorkResultActionsAreOnlyWhatTheDaemonCanShow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	workspace := store.WorkspacePath()
	writeTestFile(t, filepath.Join(workspace, "worklog", "release", "RESULT.md"), "done\n")
	writeTestFile(t, filepath.Join(workspace, "worklog", "release", "BRIEF.md"), "do it\n")

	repo := filepath.Join(t.TempDir(), "repo")
	writeTestFile(t, filepath.Join(repo, "src", "main.go"), "package main\n")
	gitForTest(t, repo, "init", "-q")
	gitForTest(t, repo, "add", ".")
	gitForTest(t, repo, "commit", "-q", "-m", "init")
	cwd := filepath.Join(repo, "src")

	source := workResultSource{
		sessionID:  "worker:@1",
		summary:    "Opened https://github.com/daoleno/mewla/pull/123. See `src/main.go`.",
		details:    `{"report":"` + filepath.Join(workspace, "worklog", "release", "RESULT.md") + `","missing":"gone/nothere.txt","mr":["https://gitlab.example.com/team/app/-/merge_requests/7"]}`,
		contextRef: "worklog/release",
		current:    true,
		cwd:        cwd,
	}
	clean := []work.ResultAction{
		{Kind: ResultActionPullRequest, URL: "https://github.com/daoleno/mewla/pull/123"},
		{Kind: ResultActionPullRequest, URL: "https://gitlab.example.com/team/app/-/merge_requests/7"},
		{Kind: ResultActionFile, SessionID: "worker:@1", Cwd: repo, Path: "src/main.go"},
		{Kind: ResultActionBrainFile, Path: "worklog/release/RESULT.md"},
		{Kind: ResultActionBrainFolder, Path: "worklog/release"},
	}
	if got := service.workResultActions(source); !reflect.DeepEqual(got, clean) {
		t.Fatalf("clean project:\n got %+v\nwant %+v", got, clean)
	}

	// The project now has a change: the current result offers Git review; an
	// older result of the same Work does not (the change is now, not then).
	writeTestFile(t, filepath.Join(repo, "src", "main.go"), "package main\n\nfunc main() {}\n")
	service.resultGit = resultGitProbes{}
	dirty := service.workResultActions(source)
	want := append([]work.ResultAction{clean[0], clean[1], {Kind: ResultActionGitReview, SessionID: "worker:@1", Cwd: repo}}, clean[2:]...)
	if !reflect.DeepEqual(dirty, want) {
		t.Fatalf("dirty project:\n got %+v\nwant %+v", dirty, want)
	}
	older := source
	older.current = false
	if got := service.workResultActions(older); !reflect.DeepEqual(got, clean) {
		t.Fatalf("older result:\n got %+v\nwant %+v", got, clean)
	}

	// A result that names nothing that exists, from a Worker with no known
	// project and a Work with no worklog, has no actions.
	if got := service.workResultActions(workResultSource{summary: "Review the delegated Session result.", details: `{"criteria_met":true}`, current: true, contextRef: "calendar:item:run"}); len(got) != 0 {
		t.Fatalf("plain result got %+v", got)
	}
}

// The thread's result rows carry their actions: AnnotateWorkResultEvents is
// what brainScopedConversation runs over every row it sends.
func TestWorkResultRowsCarryTheirActions(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	item, err := store.CreateWork(Work{Title: "open the PR", Objective: "open it", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	events := []work.CodexConversationEvent{
		{ID: "result-1", Source: workResultConversationSource, WorkID: item.ID, WorkSession: "worker:@1", Body: "PR https://github.com/daoleno/mewla/pull/9 is up"},
		{ID: "chat-1", Kind: "assistant_message", Body: "https://github.com/daoleno/mewla/pull/9"},
	}
	if err := service.AnnotateWorkResultEvents(events); err != nil {
		t.Fatal(err)
	}
	if want := []work.ResultAction{{Kind: ResultActionPullRequest, URL: "https://github.com/daoleno/mewla/pull/9"}}; !reflect.DeepEqual(events[0].WorkActions, want) {
		t.Fatalf("result row actions = %+v", events[0].WorkActions)
	}
	if events[1].WorkActions != nil {
		t.Fatalf("a chat row got actions: %+v", events[1].WorkActions)
	}
}
