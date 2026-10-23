package server

import (
	"errors"
	"os/exec"
	"testing"
)

func TestGitFailureRecognisesNotARepositoryUnderAnyLocale(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LANGUAGE", "de")
	t.Setenv("LC_MESSAGES", "de_DE.UTF-8")
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", dir+"/..")
	_, err := gitCommandOutput(dir, false, "rev-parse", "--show-toplevel")
	if !errors.Is(err, errNotGitRepository) {
		t.Fatalf("outside a repository: err = %v, want errNotGitRepository", err)
	}
	if err := gitFailure(errors.New("boom"), []byte("fatal: not a git repository")); errors.Is(err, errNotGitRepository) {
		t.Fatal("a failure that is not a git exit 128 must not read as 'not a repository'")
	}
}
