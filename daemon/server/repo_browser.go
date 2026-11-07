package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type gitRepoFileContentPayload struct {
	RepoRoot string                 `json:"repo_root"`
	Path     string                 `json:"path"`
	Snapshot gitDiffContentSnapshot `json:"snapshot"`
}

func (s *Server) buildGitRepoFileContent(targetID, cwd, path string) (gitRepoFileContentPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitRepoFileContentPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository browser is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitRepoFileContentPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	relativePath, absolutePath, err := resolveRepoBrowserPath(repoRoot, path)
	if err != nil {
		return gitRepoFileContentPayload{}, err
	}
	if relativePath == "" {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository file path is required")
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return gitRepoFileContentPayload{}, fmt.Errorf("open repository file: %w", err)
	}
	if info.IsDir() {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository path is a directory: %s", relativePath)
	}

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return gitRepoFileContentPayload{}, fmt.Errorf("read repository file: %w", err)
	}

	return gitRepoFileContentPayload{
		RepoRoot: repoRoot,
		Path:     relativePath,
		Snapshot: buildGitDiffContentSnapshot("Working tree", content),
	}, nil
}

func resolveRepoBrowserPath(repoRoot, rawPath string) (relativePath string, absolutePath string, err error) {
	cleanRoot := filepath.Clean(repoRoot)
	trimmed := rawPath

	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", cleanRoot, nil
	}

	candidate := filepath.FromSlash(trimmed)
	if filepath.IsAbs(candidate) {
		absolutePath = filepath.Clean(candidate)
	} else {
		absolutePath = filepath.Join(cleanRoot, candidate)
	}

	relativePath, err = filepath.Rel(cleanRoot, absolutePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository path: %w", err)
	}

	relativePath = filepath.Clean(relativePath)
	if relativePath == "." {
		return "", cleanRoot, nil
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("repository path escapes root")
	}

	return filepath.ToSlash(relativePath), absolutePath, nil
}
