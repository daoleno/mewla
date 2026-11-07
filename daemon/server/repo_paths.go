package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// gitRepoPathsMax bounds the reply: a tree view needs every path, but a
// monorepo with millions of files must not stall the phone.
var gitRepoPathsMax = 100000

const gitRepoPathsTimeout = 20 * time.Second

type gitRepoPathsPayload struct {
	RepoRoot  string   `json:"repo_root"`
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated"`
}

// buildGitRepoPaths lists the files a developer sees in the repository: tracked
// and untracked-but-not-ignored, sorted, with tracked files deleted from the
// working tree left out (they have nothing to open).
func (s *Server) buildGitRepoPaths(targetID, cwd string) (gitRepoPathsPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitRepoPathsPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitRepoPathsPayload{}, fmt.Errorf("repository browser is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitRepoPathsPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitRepoPathsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return gitRepoPathsPayload{}, gitFailure(err, stderr.Bytes())
	}

	seen := make(map[string]struct{})
	paths := make([]string, 0, 1024)
	for _, raw := range bytes.Split(out, []byte{0}) {
		path := string(raw)
		if path == "" || path == ".git" || strings.HasPrefix(path, ".git/") {
			continue
		}
		if _, dup := seen[path]; dup {
			continue
		}
		seen[path] = struct{}{}
		if _, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(path))); err != nil {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)

	truncated := len(paths) > gitRepoPathsMax
	if truncated {
		paths = paths[:gitRepoPathsMax]
	}
	return gitRepoPathsPayload{RepoRoot: repoRoot, Paths: paths, Truncated: truncated}, nil
}
