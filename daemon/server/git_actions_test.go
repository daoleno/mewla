package server

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures here are hermetic: no global or system git config, repo-local
// identity, and every repository lives under t.TempDir().

func hermeticGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func newActionRepo(t *testing.T) string {
	t.Helper()
	hermeticGit(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGitDiffTestGit(t, root, "init", "-q", "-b", "main")
	runGitDiffTestGit(t, root, "config", "user.email", "test@example.com")
	runGitDiffTestGit(t, root, "config", "user.name", "Test User")
	return root
}

func commitAll(t *testing.T, repo, message string) {
	t.Helper()
	runGitDiffTestGit(t, repo, "add", "-A")
	runGitDiffTestGit(t, repo, "commit", "-q", "-m", message)
}

// newRemotePair returns a repository with one pushed commit and its bare remote.
func newRemotePair(t *testing.T) (repo, bare string) {
	t.Helper()
	repo = newActionRepo(t)
	bare = t.TempDir()
	runGitDiffTestGit(t, bare, "init", "-q", "--bare", "-b", "main")
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	runGitDiffTestGit(t, repo, "remote", "add", "origin", bare)
	runGitDiffTestGit(t, repo, "push", "-q", "-u", "origin", "main")
	return repo, bare
}

func cloneRemote(t *testing.T, bare string) string {
	t.Helper()
	parent := t.TempDir()
	runGitDiffTestGit(t, parent, "clone", "-q", bare, "clone")
	clone, err := filepath.EvalSymlinks(filepath.Join(parent, "clone"))
	if err != nil {
		t.Fatal(err)
	}
	runGitDiffTestGit(t, clone, "config", "user.email", "other@example.com")
	runGitDiffTestGit(t, clone, "config", "user.name", "Other")
	return clone
}

func numberedLines(prefix string, count int) string {
	var b strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&b, "%s%02d\n", prefix, i)
	}
	return b.String()
}

// twoHunkRepo commits a 30-line file and edits line 2 and line 28, so the
// working tree diff has two hunks.
func twoHunkRepo(t *testing.T) string {
	t.Helper()
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "f.txt", numberedLines("line", 30))
	writeGitDiffTestFile(t, repo, "other.txt", "other\n")
	commitAll(t, repo, "init")
	content := strings.Replace(numberedLines("line", 30), "line02\n", "LINE TWO\n", 1)
	content = strings.Replace(content, "line28\n", "LINE TWENTY-EIGHT\n", 1)
	writeGitDiffTestFile(t, repo, "f.txt", content)
	return repo
}

func splitHunks(t *testing.T, patch string) []string {
	t.Helper()
	var hunks []string
	for _, part := range strings.Split(patch, "\n@@ ")[1:] {
		hunks = append(hunks, "@@ "+strings.TrimRight(part, "\n")+"\n")
	}
	return hunks
}

func workingPatch(t *testing.T, repo, path string, staged bool) string {
	t.Helper()
	args := []string{"diff", "--no-color"}
	if staged {
		args = append(args, "--cached")
	}
	return runGitDiffTestGit(t, repo, append(args, "--", path)...)
}

func fileContent(t *testing.T, repo, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGitStageAndUnstageFiles(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	writeGitDiffTestFile(t, repo, "dir/new file.txt", "new\n")

	if _, err := gitStage(repo, []string{"a.txt", "dir/new file.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "a.txt\ndir/new file.txt" {
		t.Fatalf("staged = %q", got)
	}
	if _, err := gitUnstage(repo, []string{"a.txt", "dir/new file.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("still staged: %q", got)
	}
	if got := fileContent(t, repo, "a.txt"); got != "two\n" {
		t.Fatalf("unstage touched the working tree: %q", got)
	}
}

func TestGitStageRenameAndUnstageBothSides(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "old.txt", "some content\nthat is long enough\nto be detected as a rename\n")
	commitAll(t, repo, "init")
	if err := os.Rename(filepath.Join(repo, "old.txt"), filepath.Join(repo, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := gitStage(repo, []string{"new.txt", "old.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-status", "-M"); !strings.HasPrefix(got, "R") {
		t.Fatalf("staged rename = %q", got)
	}
	if _, err := gitUnstage(repo, []string{"new.txt", "old.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("still staged: %q", got)
	}
}

func TestGitUnstageOnUnbornHead(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	if _, err := gitStage(repo, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitUnstage(repo, []string{"a.txt"}); err != nil {
		t.Fatalf("unstage on an unborn branch: %v", err)
	}
	if got := runGitDiffTestGit(t, repo, "ls-files"); got != "" {
		t.Fatalf("index = %q", got)
	}
	if got := fileContent(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestGitHunkStageUnstageDiscardApply(t *testing.T) {
	repo := twoHunkRepo(t)
	hunks := splitHunks(t, workingPatch(t, repo, "f.txt", false))
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d", len(hunks))
	}

	if _, err := gitHunk(repo, "f.txt", "unstaged", "stage", hunks[0]); err != nil {
		t.Fatal(err)
	}
	staged := workingPatch(t, repo, "f.txt", true)
	if !strings.Contains(staged, "+LINE TWO") || strings.Contains(staged, "TWENTY-EIGHT") {
		t.Fatalf("only the first hunk should be staged:\n%s", staged)
	}
	if rest := workingPatch(t, repo, "f.txt", false); !strings.Contains(rest, "TWENTY-EIGHT") || strings.Contains(rest, "LINE TWO") {
		t.Fatalf("only the second hunk should remain unstaged:\n%s", rest)
	}

	stagedHunks := splitHunks(t, staged)
	if _, err := gitHunk(repo, "f.txt", "staged", "unstage", stagedHunks[0]); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("hunk still staged: %q", got)
	}
	if !strings.Contains(fileContent(t, repo, "f.txt"), "LINE TWO") {
		t.Fatal("unstaging a hunk must keep the working tree")
	}

	hunks = splitHunks(t, workingPatch(t, repo, "f.txt", false))
	if _, err := gitHunk(repo, "f.txt", "unstaged", "discard", hunks[1]); err != nil {
		t.Fatal(err)
	}
	content := fileContent(t, repo, "f.txt")
	if strings.Contains(content, "TWENTY-EIGHT") || !strings.Contains(content, "LINE TWO") {
		t.Fatalf("discard should drop only the chosen hunk:\n%s", content)
	}
	if _, err := gitHunk(repo, "f.txt", "unstaged", "apply", hunks[1]); err != nil {
		t.Fatalf("undo of a hunk discard: %v", err)
	}
	if content := fileContent(t, repo, "f.txt"); !strings.Contains(content, "TWENTY-EIGHT") || !strings.Contains(content, "LINE TWO") {
		t.Fatalf("apply should bring the hunk back:\n%s", content)
	}
}

func TestGitHunkUndoOfLastHunkDiscard(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\ntwo\nthree\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "a.txt", "one\nTWO\nthree\n")
	hunk := splitHunks(t, workingPatch(t, repo, "a.txt", false))[0]
	if _, err := gitHunk(repo, "a.txt", "unstaged", "discard", hunk); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("tree should be clean, got %q", got)
	}
	if _, err := gitHunk(repo, "a.txt", "unstaged", "apply", hunk); err != nil {
		t.Fatalf("undo: %v", err)
	}
	if got := fileContent(t, repo, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestGitHunkStaleIsRejectedPlainly(t *testing.T) {
	repo := twoHunkRepo(t)
	hunks := splitHunks(t, workingPatch(t, repo, "f.txt", false))
	// The agent edits the same lines again after the diff was read.
	content := strings.Replace(fileContent(t, repo, "f.txt"), "LINE TWO", "SOMETHING ELSE", 1)
	writeGitDiffTestFile(t, repo, "f.txt", content)
	_, err := gitHunk(repo, "f.txt", "unstaged", "discard", hunks[0])
	if err == nil || !strings.HasPrefix(err.Error(), "This change no longer applies:") {
		t.Fatalf("stale hunk error = %v", err)
	}
	if strings.Contains(err.Error(), "\x1b") || strings.HasPrefix(err.Error(), "error:") {
		t.Fatalf("message is not plain: %q", err)
	}
	if got := fileContent(t, repo, "f.txt"); got != content {
		t.Fatal("a rejected hunk must not touch the file")
	}
}

func TestGitHunkCannotReachOtherFiles(t *testing.T) {
	repo := twoHunkRepo(t)
	writeGitDiffTestFile(t, repo, "other.txt", "changed\n")
	hunks := splitHunks(t, workingPatch(t, repo, "f.txt", false))

	smuggled := hunks[0] + "--- a/other.txt\n+++ b/other.txt\n@@ -1 +1 @@\n-other\n+pwned\n"
	if _, err := gitHunk(repo, "f.txt", "unstaged", "stage", smuggled); err == nil {
		t.Fatal("a second file header after the hunk must be refused")
	}
	if _, err := gitHunk(repo, "f.txt", "unstaged", "stage", "diff --git a/other.txt b/other.txt\n"+hunks[0]); err == nil {
		t.Fatal("a client-supplied diff header must be refused")
	}
	// A hunk of f.txt named as another path does not apply to it.
	if _, err := gitHunk(repo, "other.txt", "unstaged", "stage", hunks[0]); err == nil {
		t.Fatal("a hunk for another file must not apply")
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("nothing should be staged: %q", got)
	}
	for _, path := range []string{"../f.txt", "/etc/passwd", ".git/config"} {
		if _, err := gitHunk(repo, path, "unstaged", "stage", hunks[0]); err == nil {
			t.Fatalf("path %q accepted", path)
		}
	}
}

func TestGitDiscardWorkingKeepsStagedAndUndoes(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "a.txt", "staged\n")
	runGitDiffTestGit(t, repo, "add", "a.txt")
	writeGitDiffTestFile(t, repo, "a.txt", "working\n")

	result, err := gitDiscard(repo, []string{"a.txt"}, "working")
	if err != nil {
		t.Fatal(err)
	}
	if got := fileContent(t, repo, "a.txt"); got != "staged\n" {
		t.Fatalf("working discard should return to the index, got %q", got)
	}
	if got := runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only"); got != "a.txt" {
		t.Fatalf("staged change lost: %q", got)
	}
	undo := result["undo"].([]gitDiscardUndo)
	if len(undo) != 1 || undo[0].Blob == "" || undo[0].After == "" || undo[0].Mode != "100644" || undo[0].Staged {
		t.Fatalf("undo token = %+v", undo)
	}
	if _, err := gitDiscardUndoFiles(repo, undo); err != nil {
		t.Fatal(err)
	}
	if got := fileContent(t, repo, "a.txt"); got != "working\n" {
		t.Fatalf("undo restored %q", got)
	}
}

func TestGitDiscardAllResetsToHeadAndUndoRestages(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	runGitDiffTestGit(t, repo, "add", "a.txt")

	result, err := gitDiscard(repo, []string{"a.txt"}, "all")
	if err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "status", "--porcelain"); got != "" {
		t.Fatalf("tree should be clean: %q", got)
	}
	if _, err := gitDiscardUndoFiles(repo, result["undo"].([]gitDiscardUndo)); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "status", "--porcelain"); got != "M  a.txt" {
		t.Fatalf("undo should restore the staged change, got %q", got)
	}
}

func TestGitDiscardStagedAdditionRemovesFileAndUndoes(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "sub/new.txt", "new\n")
	runGitDiffTestGit(t, repo, "add", "sub/new.txt")

	result, err := gitDiscard(repo, []string{"sub/new.txt"}, "all")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "sub")); !os.IsNotExist(err) {
		t.Fatal("empty folder should be removed with its last file")
	}
	if _, err := gitDiscardUndoFiles(repo, result["undo"].([]gitDiscardUndo)); err != nil {
		t.Fatal(err)
	}
	if got := runGitDiffTestGit(t, repo, "status", "--porcelain"); got != "A  sub/new.txt" {
		t.Fatalf("status = %q", got)
	}
}

func TestGitDiscardUntrackedAndDeletedFiles(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "new.txt", "new\n")
	if err := os.Remove(filepath.Join(repo, "a.txt")); err != nil {
		t.Fatal(err)
	}

	result, err := gitDiscard(repo, []string{"new.txt", "a.txt"}, "working")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked file should be deleted")
	}
	if got := fileContent(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("deleted file should come back, got %q", got)
	}
	if _, err := gitDiscardUndoFiles(repo, result["undo"].([]gitDiscardUndo)); err != nil {
		t.Fatal(err)
	}
	if got := fileContent(t, repo, "new.txt"); got != "new\n" {
		t.Fatalf("undo of untracked discard = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("undo of a discarded deletion should delete again")
	}
}

func TestGitDiscardUndoRefusesWhenFileChanged(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	result, err := gitDiscard(repo, []string{"a.txt"}, "working")
	if err != nil {
		t.Fatal(err)
	}
	writeGitDiffTestFile(t, repo, "a.txt", "the agent wrote this\n")
	_, err = gitDiscardUndoFiles(repo, result["undo"].([]gitDiscardUndo))
	if err == nil || err.Error() != "a.txt changed since it was discarded." {
		t.Fatalf("err = %v", err)
	}
	if got := fileContent(t, repo, "a.txt"); got != "the agent wrote this\n" {
		t.Fatalf("newer work was overwritten: %q", got)
	}
}

func TestGitDiscardUndoRejectsBadTokens(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	for _, token := range []gitDiscardUndo{
		{Path: "../x", Blob: strings.Repeat("a", 40)},
		{Path: ".git/hooks/pre-commit", Blob: strings.Repeat("a", 40)},
		{Path: "x", Blob: "not-a-sha"},
		{Path: "x", Blob: strings.Repeat("a", 40)}, // well formed, but no such object
	} {
		if _, err := gitDiscardUndoFiles(repo, []gitDiscardUndo{token}); err == nil {
			t.Fatalf("token %+v accepted", token)
		}
	}
}

func TestGitDiscardRefusesSymlinks(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	if err := os.Symlink("a.txt", filepath.Join(repo, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := gitDiscard(repo, []string{"link"}, "working"); err == nil {
		t.Fatal("symlink discard should be refused")
	}
}

func TestGitCommit(t *testing.T) {
	repo := newActionRepo(t)
	if _, err := gitCommit(repo, "   "); err == nil || err.Error() != "Write a commit message." {
		t.Fatalf("empty message err = %v", err)
	}
	if _, err := gitCommit(repo, "first"); err == nil || err.Error() != "Nothing is staged." {
		t.Fatalf("nothing staged err = %v", err)
	}
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	writeGitDiffTestFile(t, repo, "b.txt", "unstaged\n")
	runGitDiffTestGit(t, repo, "add", "a.txt")
	result, err := gitCommit(repo, "Add a\n\nbody line")
	if err != nil {
		t.Fatal(err)
	}
	if result["subject"] != "Add a" || result["sha"] != runGitDiffTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("result = %v", result)
	}
	if got := runGitDiffTestGit(t, repo, "log", "-1", "--format=%B"); got != "Add a\n\nbody line" {
		t.Fatalf("message = %q", got)
	}
	if got := runGitDiffTestGit(t, repo, "status", "--porcelain"); got != "?? b.txt" {
		t.Fatalf("only staged files should be committed: %q", got)
	}
}

func TestGitCommitFailureIsPlain(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	runGitDiffTestGit(t, repo, "add", "a.txt")
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho lint failed >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := gitCommit(repo, "x")
	if err == nil || !strings.Contains(err.Error(), "lint failed") {
		t.Fatalf("hook output should reach the message: %v", err)
	}
}

func TestGitPushFirstTimeSetsUpstream(t *testing.T) {
	repo := newActionRepo(t)
	bare := t.TempDir()
	runGitDiffTestGit(t, bare, "init", "-q", "--bare", "-b", "main")
	if _, err := gitPush(repo); err == nil {
		t.Fatal("pushing an empty repo should fail plainly")
	}
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	if _, err := gitPush(repo); err == nil || err.Error() != "This repository has no remote." {
		t.Fatalf("no remote err = %v", err)
	}
	runGitDiffTestGit(t, repo, "remote", "add", "origin", bare)
	result, err := gitPush(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result["set_upstream"] != true || result["branch"] != "main" {
		t.Fatalf("result = %v", result)
	}
	if got := runGitDiffTestGit(t, bare, "rev-parse", "main"); got != runGitDiffTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatal("remote did not receive the commit")
	}
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	commitAll(t, repo, "second")
	if result, err = gitPush(repo); err != nil || result["set_upstream"] != false {
		t.Fatalf("second push: %v %v", result, err)
	}
}

func TestGitPushRejectedNonFastForward(t *testing.T) {
	repo, bare := newRemotePair(t)
	other := cloneRemote(t, bare)
	writeGitDiffTestFile(t, other, "b.txt", "theirs\n")
	commitAll(t, other, "theirs")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "main")

	writeGitDiffTestFile(t, repo, "c.txt", "mine\n")
	commitAll(t, repo, "mine")
	_, err := gitPush(repo)
	if err == nil {
		t.Fatal("non-fast-forward push should fail")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("message = %q", err)
	}
	if strings.Contains(err.Error(), "hint:") || strings.Count(err.Error(), "\n") > 1 {
		t.Fatalf("message should be short and hint-free: %q", err)
	}
}

func TestGitPushFailsPlainlyWhenRemoteIsGone(t *testing.T) {
	repo, bare := newRemotePair(t)
	if err := os.RemoveAll(bare); err != nil {
		t.Fatal(err)
	}
	writeGitDiffTestFile(t, repo, "a.txt", "two\n")
	commitAll(t, repo, "two")
	_, err := gitPush(repo)
	if err == nil || strings.TrimSpace(err.Error()) == "" || strings.Contains(err.Error(), "hint:") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitPullFastForward(t *testing.T) {
	repo, bare := newRemotePair(t)
	other := cloneRemote(t, bare)
	writeGitDiffTestFile(t, other, "b.txt", "theirs\n")
	commitAll(t, other, "theirs")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "main")

	result, err := gitPull(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result["updated"] != true || fileContent(t, repo, "b.txt") != "theirs\n" {
		t.Fatalf("result = %v", result)
	}
	if result, err = gitPull(repo); err != nil || result["updated"] != false {
		t.Fatalf("second pull: %v %v", result, err)
	}
}

func TestGitPullDivergedAndDirtyConflict(t *testing.T) {
	repo, bare := newRemotePair(t)
	other := cloneRemote(t, bare)
	writeGitDiffTestFile(t, other, "a.txt", "theirs\n")
	commitAll(t, other, "theirs")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "main")

	// A local edit to the file the remote changed: pull must refuse, not merge.
	writeGitDiffTestFile(t, repo, "a.txt", "dirty\n")
	_, err := gitPull(repo)
	if err == nil || !strings.Contains(err.Error(), "overwritten") {
		t.Fatalf("dirty pull err = %v", err)
	}
	if fileContent(t, repo, "a.txt") != "dirty\n" {
		t.Fatal("a refused pull must keep local edits")
	}

	runGitDiffTestGit(t, repo, "checkout", "-q", "--", "a.txt")
	writeGitDiffTestFile(t, repo, "mine.txt", "mine\n")
	commitAll(t, repo, "mine")
	_, err = gitPull(repo)
	if err == nil || !strings.Contains(err.Error(), "fast-forward") || strings.Contains(err.Error(), "hint:") {
		t.Fatalf("diverged pull err = %v", err)
	}
}

func TestGitFetchMakesBehindVisibleWithoutTouchingTheTree(t *testing.T) {
	repo, bare := newRemotePair(t)
	other := cloneRemote(t, bare)
	writeGitDiffTestFile(t, other, "b.txt", "theirs\n")
	commitAll(t, other, "theirs")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "main")
	writeGitDiffTestFile(t, repo, "a.txt", "dirty\n")

	status := gitDiffStatusPayload{}
	status.fillBranchState(repo)
	if status.Behind != 0 {
		t.Fatalf("behind before fetch = %d", status.Behind)
	}
	if _, err := gitFetch(repo); err != nil {
		t.Fatal(err)
	}
	status.fillBranchState(repo)
	if status.Behind != 1 {
		t.Fatalf("behind after fetch = %d", status.Behind)
	}
	if fileContent(t, repo, "a.txt") != "dirty\n" {
		t.Fatal("fetch must leave the working tree alone")
	}
}

func TestGitFetchWithoutRemoteIsQuiet(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	result, err := gitFetch(repo)
	if err != nil || result["message"] != "" {
		t.Fatalf("result = %v, err = %v", result, err)
	}
}

func TestGitPullWithoutUpstream(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	if _, err := gitPull(repo); err == nil || err.Error() != "This branch has no upstream to pull from." {
		t.Fatalf("err = %v", err)
	}
}

func TestGitSwitchAndBranches(t *testing.T) {
	repo, bare := newRemotePair(t)
	runGitDiffTestGit(t, repo, "branch", "feature")
	other := cloneRemote(t, bare)
	runGitDiffTestGit(t, other, "checkout", "-q", "-b", "remote-only")
	writeGitDiffTestFile(t, other, "r.txt", "r\n")
	commitAll(t, other, "remote work")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "remote-only")
	runGitDiffTestGit(t, repo, "fetch", "-q", "origin")

	list, err := gitBranches(repo)
	if err != nil {
		t.Fatal(err)
	}
	if list.Current != "main" || list.Detached || list.Branches[0].Name != "main" || !list.Branches[0].Current {
		t.Fatalf("branches = %+v", list)
	}
	byName := map[string]gitBranchInfo{}
	for _, b := range list.Branches {
		byName[b.Name] = b
	}
	if byName["feature"].Remote || byName["feature"].Name == "" {
		t.Fatalf("feature = %+v", byName["feature"])
	}
	if r := byName["remote-only"]; !r.Remote || r.RemoteRef != "origin/remote-only" || r.Subject != "remote work" {
		t.Fatalf("remote-only = %+v", r)
	}
	if _, dup := byName["HEAD"]; dup || len(list.Branches) != 3 {
		t.Fatalf("expected main, feature, remote-only; got %+v", list.Branches)
	}
	if byName["main"].Upstream != "origin/main" {
		t.Fatalf("main upstream = %q", byName["main"].Upstream)
	}

	if _, err := gitSwitch(repo, "feature", false); err != nil {
		t.Fatal(err)
	}
	if got := gitCurrentBranch(repo); got != "feature" {
		t.Fatalf("branch = %q", got)
	}
	if _, err := gitSwitch(repo, "remote-only", false); err != nil {
		t.Fatalf("switching to a remote-only branch: %v", err)
	}
	if got := gitUpstream(repo); got != "origin/remote-only" {
		t.Fatalf("upstream = %q", got)
	}
	if _, err := gitSwitch(repo, "fresh", true); err != nil || gitCurrentBranch(repo) != "fresh" {
		t.Fatalf("create: %v", err)
	}
	for _, name := range []string{"-bad", "has space", "a..b", ""} {
		if _, err := gitSwitch(repo, name, false); err == nil {
			t.Fatalf("branch name %q accepted", name)
		}
	}
}

func TestGitSwitchBlockedByLocalChanges(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	runGitDiffTestGit(t, repo, "branch", "other")
	runGitDiffTestGit(t, repo, "checkout", "-q", "other")
	writeGitDiffTestFile(t, repo, "a.txt", "other\n")
	commitAll(t, repo, "other")
	runGitDiffTestGit(t, repo, "checkout", "-q", "main")
	writeGitDiffTestFile(t, repo, "a.txt", "dirty\n")

	_, err := gitSwitch(repo, "other", false)
	if err == nil || !strings.Contains(err.Error(), "overwritten") {
		t.Fatalf("err = %v", err)
	}
	if gitCurrentBranch(repo) != "main" || fileContent(t, repo, "a.txt") != "dirty\n" {
		t.Fatal("a blocked switch must change nothing")
	}
}

func TestGitBranchesOnUnbornRepo(t *testing.T) {
	repo := newActionRepo(t)
	list, err := gitBranches(repo)
	if err != nil || list.Current != "main" || len(list.Branches) != 1 || !list.Branches[0].Current {
		t.Fatalf("%+v %v", list, err)
	}
	log, err := gitLog(repo, 10, 0)
	if err != nil || len(log.Commits) != 0 {
		t.Fatalf("%+v %v", log, err)
	}
}

func TestGitLogAndShow(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\ntwo\n")
	commitAll(t, repo, "first")
	writeGitDiffTestFile(t, repo, "a.txt", "one\nTWO\nthree\n")
	writeGitDiffTestFile(t, repo, "b.txt", "new\n")
	runGitDiffTestGit(t, repo, "add", "-A")
	runGitDiffTestGit(t, repo, "commit", "-q", "-m", "second subject", "-m", "second body")
	runGitDiffTestGit(t, repo, "tag", "v1")
	writeGitDiffTestFile(t, repo, "c.txt", "c\n")
	commitAll(t, repo, "third")

	log, err := gitLog(repo, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Commits) != 2 || !log.HasMore || log.Commits[0].Subject != "third" || log.Commits[0].Author != "Test User" {
		t.Fatalf("log = %+v", log)
	}
	if got := strings.Join(log.Commits[0].Refs, ","); !strings.Contains(got, "HEAD -> main") {
		t.Fatalf("refs = %q", got)
	}
	if log.Commits[1].Parents != 1 || log.Commits[1].Date == 0 || len(log.Commits[1].Short) < 7 {
		t.Fatalf("commit = %+v", log.Commits[1])
	}
	page2, err := gitLog(repo, 2, 2)
	if err != nil || len(page2.Commits) != 1 || page2.HasMore || page2.Commits[0].Subject != "first" || page2.Commits[0].Parents != 0 {
		t.Fatalf("page2 = %+v %v", page2, err)
	}

	show, err := gitShow(repo, log.Commits[1].SHA)
	if err != nil {
		t.Fatal(err)
	}
	if show.Subject != "second subject" || show.Body != "second body" || show.Truncated {
		t.Fatalf("show = %+v", show)
	}
	if len(show.Files) != 2 {
		t.Fatalf("files = %+v", show.Files)
	}
	a, b := show.Files[0], show.Files[1]
	if a.Path != "a.txt" || a.Status != "modified" || a.Additions != 2 || a.Deletions != 1 {
		t.Fatalf("a = %+v", a)
	}
	if b.Path != "b.txt" || b.Status != "added" || b.Additions != 1 {
		t.Fatalf("b = %+v", b)
	}
	if !strings.Contains(show.Patch, "diff --git a/a.txt b/a.txt") || !strings.Contains(show.Patch, "+three") {
		t.Fatalf("patch = %q", show.Patch)
	}
	// A short sha works; garbage and option-looking input do not.
	if _, err := gitShow(repo, log.Commits[1].Short); err != nil {
		t.Fatalf("short sha: %v", err)
	}
	for _, bad := range []string{"--output=/tmp/x", "zzzzzzz", "main", "0000000000000000000000000000000000000000"} {
		if _, err := gitShow(repo, bad); err == nil {
			t.Fatalf("sha %q accepted", bad)
		}
	}
}

func TestGitShowRootCommitAndTruncation(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "small.txt", "small\n")
	writeGitDiffTestFile(t, repo, "zz-big.txt", strings.Repeat("0123456789abcdef\n", 400*1024))
	commitAll(t, repo, "root")
	sha := runGitDiffTestGit(t, repo, "rev-parse", "HEAD")
	show, err := gitShow(repo, sha)
	if err != nil {
		t.Fatal(err)
	}
	if !show.Truncated || len(show.Patch) > gitShowPatchMax {
		t.Fatalf("truncated=%v len=%d", show.Truncated, len(show.Patch))
	}
	if !strings.Contains(show.Patch, "small.txt") || strings.Contains(show.Patch, "zz-big.txt") {
		t.Fatal("truncation must cut at a file boundary")
	}
	if len(show.Files) != 2 {
		t.Fatalf("the file list stays complete: %+v", show.Files)
	}
}

func TestGitShowMergeUsesFirstParent(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	runGitDiffTestGit(t, repo, "checkout", "-q", "-b", "side")
	writeGitDiffTestFile(t, repo, "side.txt", "side\n")
	commitAll(t, repo, "side")
	runGitDiffTestGit(t, repo, "checkout", "-q", "main")
	writeGitDiffTestFile(t, repo, "main.txt", "main\n")
	commitAll(t, repo, "main")
	runGitDiffTestGit(t, repo, "merge", "-q", "--no-ff", "-m", "merge", "side")
	show, err := gitShow(repo, runGitDiffTestGit(t, repo, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if len(show.Files) != 1 || show.Files[0].Path != "side.txt" {
		t.Fatalf("merge diff should be against the first parent: %+v", show.Files)
	}
}

func TestGitActionRepoRootGuardAndValidation(t *testing.T) {
	repo := newActionRepo(t)
	other := newActionRepo(t)
	srv := &Server{}
	if _, err := srv.resolveGitActionRepo(clientMessage{Cwd: repo, GitRepoRoot: repo}); err != nil {
		t.Fatalf("matching root: %v", err)
	}
	if _, err := srv.resolveGitActionRepo(clientMessage{Cwd: repo}); err != nil {
		t.Fatalf("no root given: %v", err)
	}
	_, err := srv.resolveGitActionRepo(clientMessage{Cwd: other, GitRepoRoot: repo})
	if err == nil || err.Error() != "This terminal is no longer in "+filepath.Base(repo)+"." {
		t.Fatalf("mismatch err = %v", err)
	}
	if _, err := srv.resolveGitActionRepo(clientMessage{Cwd: t.TempDir()}); err == nil {
		t.Fatal("a folder outside any repository must be refused")
	}

	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	for _, path := range []string{"../a.txt", "/etc/passwd", ".git/config", "x/../../y", "sub/.git/hooks/x", ""} {
		if _, err := gitStage(repo, []string{path}); err == nil {
			t.Fatalf("stage accepted %q", path)
		}
		if _, err := gitDiscard(repo, []string{path}, "working"); err == nil {
			t.Fatalf("discard accepted %q", path)
		}
	}
	if _, err := gitStage(repo, nil); err == nil {
		t.Fatal("empty path list accepted")
	}
	if _, err := gitDiscard(repo, []string{"a.txt"}, "bogus"); err == nil {
		t.Fatal("bad discard scope accepted")
	}
}

func TestGitBusyRepositoryIsPlain(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	if err := os.WriteFile(filepath.Join(repo, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := gitStage(repo, []string{"a.txt"})
	if err != errGitBusy {
		t.Fatalf("err = %v", err)
	}
}

func TestGitStatusAheadBehindAndUpstream(t *testing.T) {
	repo, bare := newRemotePair(t)
	other := cloneRemote(t, bare)
	writeGitDiffTestFile(t, other, "b.txt", "theirs\n")
	commitAll(t, other, "theirs")
	runGitDiffTestGit(t, other, "push", "-q", "origin", "main")
	writeGitDiffTestFile(t, repo, "c.txt", "mine\n")
	commitAll(t, repo, "mine")
	writeGitDiffTestFile(t, repo, "c.txt", "dirty\n")
	runGitDiffTestGit(t, repo, "fetch", "-q", "origin")

	status, err := (&Server{}).buildGitDiffStatus("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Upstream != "origin/main" || status.Ahead != 1 || status.Behind != 1 || status.Detached || !status.HasCommits || status.Operation != "" {
		t.Fatalf("status = %+v", status)
	}

	runGitDiffTestGit(t, repo, "checkout", "-q", "--detach")
	status, _ = (&Server{}).buildGitDiffStatus("", repo)
	if !status.Detached || status.Upstream != "" {
		t.Fatalf("detached status = %+v", status)
	}
}

func TestGitStatusUnbornAndMergeOperation(t *testing.T) {
	repo := newActionRepo(t)
	status, err := (&Server{}).buildGitDiffStatus("", repo)
	if err != nil || status.HasCommits || status.Detached || status.Branch != "main" {
		t.Fatalf("unborn status = %+v %v", status, err)
	}

	writeGitDiffTestFile(t, repo, "a.txt", "base\n")
	commitAll(t, repo, "init")
	runGitDiffTestGit(t, repo, "checkout", "-q", "-b", "side")
	writeGitDiffTestFile(t, repo, "a.txt", "side\n")
	commitAll(t, repo, "side")
	runGitDiffTestGit(t, repo, "checkout", "-q", "main")
	writeGitDiffTestFile(t, repo, "a.txt", "main\n")
	commitAll(t, repo, "main")
	merge := gitCommand("-C", repo, "merge", "side")
	_ = merge.Run() // conflicts: exit 1

	status, err = (&Server{}).buildGitDiffStatus("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Operation != "merge" {
		t.Fatalf("operation = %q", status.Operation)
	}
	if len(status.Files) != 1 || status.Files[0].Status != "conflict" {
		t.Fatalf("files = %+v", status.Files)
	}
}

func TestPlainGitMessage(t *testing.T) {
	raw := "To /tmp/remote\n \x1b[31m! [rejected]\x1b[m        main -> main (fetch first)\nerror: failed to push some refs to '/tmp/remote'\nhint: Updates were rejected\nhint: more\n"
	got := plainGitMessage(raw, 2)
	want := "[rejected]        main -> main (fetch first)\nfailed to push some refs to '/tmp/remote'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestValidateGitHunks(t *testing.T) {
	ok := "@@ -1,2 +1,2 @@\n a\n-b\n+c\n"
	if _, err := validateGitHunks(ok); err != nil {
		t.Fatal(err)
	}
	if _, err := validateGitHunks(ok + "\\ No newline at end of file\n"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"", "diff --git a/x b/x\n" + ok, "@@ -1,2 +1,2 @@\n a\n-b\n", ok + "--- a/y\n+++ b/y\n", ok + "garbage\n",
		"@@ -1,2 +1,2 @@\n a\n-b\n+c\nindex 1..2\n",
	} {
		if _, err := validateGitHunks(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	two := ok + "@@ -10 +10 @@\n-x\n+y\n"
	if _, err := validateGitHunks(two); err != nil {
		t.Fatalf("two hunks: %v", err)
	}
}

func TestGitHunkHitsTheRightFileWithNoPrefixConfig(t *testing.T) {
	repo := newActionRepo(t)
	runGitDiffTestGit(t, repo, "config", "diff.noprefix", "true")
	writeGitDiffTestFile(t, repo, "x.txt", "root\n")
	writeGitDiffTestFile(t, repo, "src/x.txt", numberedLines("line", 10))
	commitAll(t, repo, "init")
	writeGitDiffTestFile(t, repo, "src/x.txt", strings.Replace(numberedLines("line", 10), "line05\n", "CHANGED\n", 1))
	writeGitDiffTestFile(t, repo, "x.txt", "root edited\n")
	hunk := splitHunks(t, runGitDiffTestGit(t, repo, "diff", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--", "src/x.txt"))[0]

	if _, err := gitHunk(repo, "src/x.txt", "unstaged", "stage", hunk); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGitDiffTestGit(t, repo, "diff", "--cached", "--name-only")); got != "src/x.txt" {
		t.Fatalf("staged files = %q, want only src/x.txt", got)
	}
	if _, err := gitHunk(repo, "src/x.txt", "staged", "unstage", hunk); err != nil {
		t.Fatal(err)
	}
	if _, err := gitHunk(repo, "src/x.txt", "unstaged", "discard", hunk); err != nil {
		t.Fatal(err)
	}
	if fileContent(t, repo, "src/x.txt") != numberedLines("line", 10) || fileContent(t, repo, "x.txt") != "root edited\n" {
		t.Fatal("discard must change src/x.txt and leave x.txt alone")
	}
}

func TestGitHunkOnCRLFFile(t *testing.T) {
	repo := newActionRepo(t)
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	writeGitDiffTestFile(t, repo, "w.txt", crlf(numberedLines("row", 30)))
	commitAll(t, repo, "init")
	edited := strings.Replace(numberedLines("row", 30), "row03\n", "ROW THREE\n", 1)
	edited = strings.Replace(edited, "row27\n", "ROW TWENTY-SEVEN\n", 1)
	writeGitDiffTestFile(t, repo, "w.txt", crlf(edited))
	// The shared helper trims its output, which would eat the last line's CR.
	rawHunks := func() []string {
		out, err := exec.Command("git", "-C", repo, "diff", "--no-color", "--", "w.txt").Output()
		if err != nil {
			t.Fatal(err)
		}
		return splitHunks(t, string(out))
	}
	hunks := rawHunks()
	if len(hunks) != 2 {
		t.Fatalf("hunks = %d", len(hunks))
	}
	if _, err := gitHunk(repo, "w.txt", "unstaged", "stage", hunks[0]); err != nil {
		t.Fatal(err)
	}
	staged := runGitDiffTestGit(t, repo, "show", ":w.txt")
	if !strings.Contains(staged, "ROW THREE\r\n") || strings.Contains(staged, "TWENTY-SEVEN") {
		t.Fatalf("the staged copy must keep CRLF and only the first hunk: %q", staged)
	}
	if _, err := gitHunk(repo, "w.txt", "unstaged", "discard", rawHunks()[0]); err != nil {
		t.Fatal(err)
	}
	if got := fileContent(t, repo, "w.txt"); strings.Contains(got, "TWENTY-SEVEN") || !strings.Contains(got, "ROW THREE\r\n") {
		t.Fatalf("discard on a CRLF file: %q", got)
	}
}

func TestGitPathsAreOneSpellingAndPlain(t *testing.T) {
	for in, want := range map[string]string{"a/./b": "a/b", "a//b": "a/b", "./a/b": "a/b", "a/b/": "a/b"} {
		if got, err := cleanGitPath(in); err != nil || got != want {
			t.Errorf("cleanGitPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", ".", "a/../b", "../a", "/etc/passwd", "a\nb", "a\rb", "a\x00b", ".git/HEAD", "a/.GIT/x"} {
		if _, err := cleanGitPath(bad); err == nil {
			t.Errorf("cleanGitPath(%q) was accepted", bad)
		}
	}
}

func TestGitSymlinkIntoDotGitIsRefused(t *testing.T) {
	repo := newActionRepo(t)
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	commitAll(t, repo, "init")
	if err := os.Symlink(".git", filepath.Join(repo, "g")); err != nil {
		t.Skip("symlinks are unavailable:", err)
	}
	head := fileContent(t, repo, ".git/HEAD")

	if _, err := gitDiscard(repo, []string{"g/HEAD"}, "working"); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("discard through the symlink: %v", err)
	}
	if fileContent(t, repo, ".git/HEAD") != head {
		t.Fatal(".git/HEAD changed")
	}
	blob := strings.TrimSpace(runGitDiffTestGit(t, repo, "hash-object", "-w", "a.txt"))
	_, err := gitDiscardUndoFiles(repo, []gitDiscardUndo{{Path: "g/hooks/pre-commit", Blob: blob, Mode: "100755"}})
	if err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("undo through the symlink: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "hooks", "pre-commit")); statErr == nil {
		t.Fatal("a hook was written into .git/hooks")
	}
}

func TestGitSSHChoiceIsNotOverridden(t *testing.T) {
	if gitUsesOwnSSH("", "", "") {
		t.Fatal("nothing chosen: the daemon default applies")
	}
	for _, own := range [][3]string{{"ssh -i k", "", ""}, {"", "/usr/bin/mysh", ""}, {"", "", "ssh -i key"}} {
		if !gitUsesOwnSSH(own[0], own[1], own[2]) {
			t.Fatalf("%v should count as the person's choice", own)
		}
	}
	repo := newActionRepo(t)
	t.Setenv("GIT_SSH_COMMAND", "")
	t.Setenv("GIT_SSH", "")
	has := func(env []string) bool {
		for _, entry := range env {
			if strings.HasPrefix(entry, "GIT_SSH_COMMAND=ssh -o BatchMode=yes") {
				return true
			}
		}
		return false
	}
	if !has(gitActionEnv(repo, true)) {
		t.Fatal("a network command with no choice gets BatchMode")
	}
	if has(gitActionEnv(repo, false)) {
		t.Fatal("a local command needs no ssh setting")
	}
	runGitDiffTestGit(t, repo, "config", "core.sshCommand", "ssh -i /somewhere/key")
	if has(gitActionEnv(repo, true)) {
		t.Fatal("core.sshCommand must not be overridden")
	}
}

func TestGitStaleUpstreamIsNotRecreatedByPush(t *testing.T) {
	repo, bare := newRemotePair(t)
	runGitDiffTestGit(t, repo, "switch", "-q", "-c", "feat")
	writeGitDiffTestFile(t, repo, "f.txt", "feat\n")
	commitAll(t, repo, "feat")
	if _, err := gitPush(repo); err != nil {
		t.Fatal(err)
	}
	runGitDiffTestGit(t, repo, "push", "-q", "origin", ":feat")
	if _, err := gitFetch(repo); err != nil {
		t.Fatal(err)
	}

	status := gitDiffStatusPayload{}
	status.fillBranchState(repo)
	if status.Upstream != "origin/feat" || status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("status upstream %q ahead %d behind %d", status.Upstream, status.Ahead, status.Behind)
	}
	if _, err := gitPush(repo); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("push err = %v", err)
	}
	if out := runGitDiffTestGit(t, repo, "ls-remote", bare, "refs/heads/feat"); strings.TrimSpace(out) != "" {
		t.Fatalf("push recreated the deleted branch: %q", out)
	}
	if _, err := gitPull(repo); err == nil || err.Error() != "The upstream branch no longer exists." {
		t.Fatalf("pull err = %v", err)
	}
}

func TestGitCommitAndSwitchHaveRoomForHooks(t *testing.T) {
	if gitHookTimeout < 5*time.Minute || gitHookTimeout <= gitNetworkTimeout {
		t.Fatalf("hook timeout = %v", gitHookTimeout)
	}
}

func TestGitTimeoutStopsTheProcessGroupAndSaysWhy(t *testing.T) {
	repo := newActionRepo(t)
	hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
	marker := filepath.Join(repo, "hook-started")
	script := "#!/bin/sh\ntouch '" + marker + "'\nsleep 30\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitDiffTestFile(t, repo, "a.txt", "one\n")
	runGitDiffTestGit(t, repo, "add", "a.txt")
	start := time.Now()
	_, err := gitRun(repo, gitRunOptions{timeout: 700 * time.Millisecond}, "commit", "-q", "-m", "x")
	if err == nil || strings.Contains(err.Error(), "connection") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("the hook was not stopped: took %v", time.Since(start))
	}
	if _, statErr := os.Stat(filepath.Join(repo, ".git", "index.lock")); statErr == nil {
		t.Fatal("a stale index.lock was left behind")
	}
}
