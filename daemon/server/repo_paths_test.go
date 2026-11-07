package server

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildGitRepoPathsListsTrackedAndUntrackedButNotIgnored(t *testing.T) {
	repoRoot := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repoRoot, ".gitignore", "ignored.log\nbuild/\n")
	writeGitDiffTestFile(t, repoRoot, "README.md", "readme\n")
	writeGitDiffTestFile(t, repoRoot, "src/app/main.go", "package main\n")
	writeGitDiffTestFile(t, repoRoot, "docs/with space/notes é.md", "notes\n")
	writeGitDiffTestFile(t, repoRoot, "gone.txt", "gone\n")
	runGitDiffTestGit(t, repoRoot, "add", ".")
	runGitDiffTestGit(t, repoRoot, "commit", "-m", "initial")

	writeGitDiffTestFile(t, repoRoot, "src/app/new.go", "package main\n")
	writeGitDiffTestFile(t, repoRoot, "ignored.log", "x\n")
	writeGitDiffTestFile(t, repoRoot, "build/out.bin", "x\n")
	if err := os.Remove(filepath.Join(repoRoot, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	payload, err := (&Server{}).buildGitRepoPaths("", repoRoot)
	if err != nil {
		t.Fatalf("buildGitRepoPaths returned error: %v", err)
	}
	want := []string{".gitignore", "README.md", "docs/with space/notes é.md", "src/app/main.go", "src/app/new.go"}
	if !reflect.DeepEqual(payload.Paths, want) {
		t.Fatalf("paths = %q, want %q", payload.Paths, want)
	}
	if payload.Truncated {
		t.Fatal("small repository reported truncated")
	}
	for _, path := range payload.Paths {
		if path == ".git" || strings.HasPrefix(path, ".git/") {
			t.Fatalf("paths include git internals: %q", path)
		}
	}
	if payload.RepoRoot == "" {
		t.Fatal("repo_root is empty")
	}
}

func TestBuildGitRepoPathsTruncatesAtTheCap(t *testing.T) {
	repoRoot := initGitDiffTestRepo(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		writeGitDiffTestFile(t, repoRoot, name, name)
	}
	previous := gitRepoPathsMax
	gitRepoPathsMax = 3
	t.Cleanup(func() { gitRepoPathsMax = previous })

	payload, err := (&Server{}).buildGitRepoPaths("", repoRoot)
	if err != nil {
		t.Fatalf("buildGitRepoPaths returned error: %v", err)
	}
	if !payload.Truncated || !reflect.DeepEqual(payload.Paths, []string{"a.txt", "b.txt", "c.txt"}) {
		t.Fatalf("payload = %+v, want the first three sorted paths and truncated", payload)
	}
}

func TestBuildGitRepoPathsRejectsMissingCwdAndNonRepository(t *testing.T) {
	if _, err := (&Server{}).buildGitRepoPaths("", ""); err == nil || !strings.Contains(err.Error(), "no cwd") {
		t.Fatalf("no cwd: err = %v, want a no-cwd error", err)
	}
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if _, err := (&Server{}).buildGitRepoPaths("", dir); err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("non-repository: err = %v, want a not-a-repository error", err)
	}
}
