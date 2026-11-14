package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// A checkout with a big untracked build tree (the owner's 11-14 freeze: an old
// 1.8 GB app/ of 14,883 files made a 4 MB status the app froze on). The status
// reports the folder whole, as `git status` does, small and fast.
func TestGitDiffStatusReportsAnUntrackedTreeAsOneFolder(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "README.md", "hello\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	for i := 0; i < 3000; i++ {
		writeGitDiffTestFile(t, repo, fmt.Sprintf("app/node_modules/pkg%03d/lib/file%02d.js", i/20, i%20), "module.exports = 1;\n")
	}
	writeGitDiffTestFile(t, repo, "notes.txt", "one\ntwo\n")
	writeGitDiffTestFile(t, repo, "README.md", "hello\nworld\n")

	start := time.Now()
	payload, err := (&Server{}).buildGitDiffStatus("", repo)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	frame, err := json.Marshal(map[string]any{"type": "git_diff_status", "status": payload})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Files) != 3 || payload.FileCount != 3 || payload.UntrackedFileCount != 2 || payload.Truncated {
		t.Fatalf("files = %+v (count %d, untracked %d, truncated %v)", payload.Files, payload.FileCount, payload.UntrackedFileCount, payload.Truncated)
	}
	byPath := map[string]gitDiffFileInfo{}
	for _, file := range payload.Files {
		byPath[file.Path] = file
	}
	folder, ok := byPath["app/"]
	if !ok || !folder.Directory || !folder.Untracked || folder.Additions != 0 {
		t.Fatalf("app/ = %+v", folder)
	}
	if notes := byPath["notes.txt"]; notes.Directory || notes.Additions != 2 {
		t.Fatalf("notes.txt = %+v", notes)
	}
	if len(frame) > 2048 {
		t.Fatalf("status frame is %d bytes", len(frame))
	}
	if took > 3*time.Second {
		t.Fatalf("status took %s", took)
	}

	// The folder has no diff of its own and no whole-file sides; neither fails the page.
	patch, err := (&Server{}).buildGitDiffPatch("", repo, "app/")
	if err != nil || len(patch.Sections) != 0 {
		t.Fatalf("app/ patch = %+v, %v", patch, err)
	}
	if _, err := (&Server{}).buildGitDiffFileContent("", repo, "app/", ""); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("app/ content error = %v", err)
	}
	// Staging the folder takes its files, as `git add app/` does.
	if clean, err := cleanGitPath("app/"); err != nil || clean != "app" {
		t.Fatalf("cleanGitPath(app/) = %q, %v", clean, err)
	}
}

// A vast change of tracked files still lists a bounded page, tracked changes
// before untracked ones, with every count and total over all of them.
func TestGitDiffStatusListsABoundedPageWithHonestCounts(t *testing.T) {
	previous := gitDiffStatusMaxFiles
	gitDiffStatusMaxFiles = 5
	t.Cleanup(func() { gitDiffStatusMaxFiles = previous })

	repo := initGitDiffTestRepo(t)
	for i := 0; i < 8; i++ {
		writeGitDiffTestFile(t, repo, fmt.Sprintf("src/f%d.go", i), "a\n")
	}
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	for i := 0; i < 8; i++ {
		writeGitDiffTestFile(t, repo, fmt.Sprintf("src/f%d.go", i), "a\nb\n")
	}
	for i := 0; i < 3; i++ {
		writeGitDiffTestFile(t, repo, fmt.Sprintf("a%d.txt", i), "x\ny\nz\n")
	}

	payload, err := (&Server{}).buildGitDiffStatus("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !payload.Truncated || len(payload.Files) != 5 || payload.FileCount != 11 {
		t.Fatalf("truncated %v, listed %d of %d", payload.Truncated, len(payload.Files), payload.FileCount)
	}
	if payload.UnstagedFileCount != 8 || payload.UntrackedFileCount != 3 || payload.Additions != 8+9 {
		t.Fatalf("counts: unstaged %d untracked %d additions %d", payload.UnstagedFileCount, payload.UntrackedFileCount, payload.Additions)
	}
	for i, file := range payload.Files {
		if file.Untracked {
			t.Fatalf("untracked %s listed before the tracked changes", file.Path)
		}
		if i > 0 && payload.Files[i-1].Path >= file.Path {
			t.Fatalf("not in path order: %s then %s", payload.Files[i-1].Path, file.Path)
		}
	}
}

// A Session's header asks only whether its folder is a repository: no status is
// read and no file is listed, however big the checkout.
func TestGitRepositoryStatusAnswersWithoutFiles(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "README.md", "hello\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	writeGitDiffTestFile(t, repo, "README.md", "changed\n")
	payload, err := (&Server{}).buildGitRepositoryStatus("", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !payload.Available || payload.RepoRoot == "" || len(payload.Files) != 0 || payload.FileCount != 0 {
		t.Fatalf("repository status = %+v", payload)
	}
	outside, err := (&Server{}).buildGitRepositoryStatus("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if outside.Available || outside.Reason != gitDiffReasonNotGitRepo {
		t.Fatalf("outside a repository = %+v", outside)
	}
}
