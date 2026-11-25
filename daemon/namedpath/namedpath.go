// Package namedpath resolves a path an agent named in a reply (a Chat link, a preview, a page) on the computer
// that runs the Session: relative ones against the Session's folder, "~" against the daemon account's home, and a
// folder named with its closing separator to its index.html.
package namedpath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxReferenceBytes bounds a reference.
const MaxReferenceBytes = 4096

// ErrMissing: the referenced file does not exist.
var ErrMissing = errors.New("Session file is missing")

// Resolve: the live Session's canonical cwd and the canonical path a reference names, relative ones
// against that cwd and "~/…" against the daemon account's home.
func Resolve(cwd, reference string) (string, string, error) {
	if strings.TrimSpace(reference) == "" {
		return "", "", fmt.Errorf("file reference is required")
	}
	if len(reference) > MaxReferenceBytes || strings.ContainsRune(reference, 0) {
		return "", "", fmt.Errorf("file reference is invalid")
	}

	referencePath := filepath.FromSlash(reference)
	// "~/…" (a path an agent names in a reply; "~\…" on Windows) is the home of the account the daemon runs as, not
	// a folder of the cwd.
	if len(referencePath) > 1 && referencePath[0] == '~' && os.IsPathSeparator(referencePath[1]) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve home folder: %w", err)
		}
		referencePath = filepath.Join(home, referencePath[2:])
	}
	// The Session's folder, for a relative reference and for the path shown beside the file. An ended run's
	// folder can be gone (a worktree or scratch collected): its absolute references still resolve.
	canonicalCWD, cwdErr := canonicalFolder(cwd)
	if cwdErr != nil && !filepath.IsAbs(referencePath) {
		if os.IsNotExist(errors.Unwrap(cwdErr)) {
			return "", "", ErrMissing
		}
		return "", "", cwdErr
	}
	candidatePath := referencePath
	if !filepath.IsAbs(referencePath) {
		// canonicalCWD names the exact live directory inode. Keep the reference
		// components intact until EvalSymlinks so aliases followed by ".." have
		// host filesystem semantics instead of filepath.Join's lexical semantics.
		candidatePath = canonicalCWD + string(filepath.Separator) + referencePath
	}
	canonicalPath, err := filepath.EvalSymlinks(candidatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ErrMissing
		}
		return "", "", fmt.Errorf("resolve Session file: %w", err)
	}

	return canonicalCWD, canonicalPath, nil
}

// FolderPage: for a reference that names a folder ("reports/q3/", with its slash; `reports\q3\` on Windows), the
// folder's index.html, the page a folder report opens with (through any symlink); otherwise canonicalPath as it is.
func FolderPage(canonicalPath, reference string) string {
	if reference == "" || !os.IsPathSeparator(reference[len(reference)-1]) {
		return canonicalPath
	}
	if info, err := os.Stat(canonicalPath); err != nil || !info.IsDir() {
		return canonicalPath
	}
	page, err := filepath.EvalSymlinks(filepath.Join(canonicalPath, "index.html"))
	if err != nil {
		return canonicalPath
	}
	return page
}

// canonicalFolder: the Session's folder through any symlink; an error when it is unknown, gone or not a folder.
func canonicalFolder(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("live Session CWD is unavailable")
	}
	cwdPath := filepath.FromSlash(cwd)
	if !filepath.IsAbs(cwdPath) {
		var err error
		if cwdPath, err = filepath.Abs(cwdPath); err != nil {
			return "", fmt.Errorf("resolve live Session CWD: %w", err)
		}
	}
	canonical, err := filepath.EvalSymlinks(cwdPath)
	if err != nil {
		return "", fmt.Errorf("resolve live Session CWD: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect live Session CWD: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("live Session CWD is not a directory")
	}
	return canonical, nil
}
