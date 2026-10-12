package watch

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RuntimeProjectID is the built-in project that owns the daemon's own log
// when no checkout of the Mewla repository is known on this computer.
const RuntimeProjectID = "mewla"

// runtimeRepoSlug identifies a checkout of the Mewla repository by remote.
const runtimeRepoSlug = "daoleno/mewla"

// projectID derives a stable ID from a repository root.
func projectID(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return "p_" + hex.EncodeToString(sum[:])[:10]
}

// repoRoot finds the repository that owns cwd. A linked worktree (including
// Mewla's own under the worktree root) maps to its main repository; a cwd
// that no longer exists is resolved through its nearest existing ancestor.
// gitDir is the repository's common git directory.
func repoRoot(cwd string) (root, gitDir string, ok bool) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", "", false
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		marker := filepath.Join(dir, ".git")
		info, err := os.Stat(marker)
		if err == nil {
			if info.IsDir() {
				return dir, marker, true
			}
			if common, ok := linkedCommonDir(marker); ok {
				if filepath.Base(common) == ".git" {
					return filepath.Dir(common), common, true
				}
				return dir, common, true
			}
			return dir, "", true
		}
		if parent := filepath.Dir(dir); parent == dir {
			return "", "", false
		}
	}
}

// linkedCommonDir reads a ".git" file ("gitdir: …") and returns the common
// git directory it belongs to.
func linkedCommonDir(marker string) (string, bool) {
	raw, err := os.ReadFile(marker)
	if err != nil {
		return "", false
	}
	value, found := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !found {
		return "", false
	}
	gitDir := strings.TrimSpace(value)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(filepath.Dir(marker), gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(raw))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		return filepath.Clean(common), true
	}
	// Worktree metadata lives at <common>/worktrees/<name>.
	if parent := filepath.Dir(gitDir); filepath.Base(parent) == "worktrees" {
		return filepath.Dir(parent), true
	}
	return gitDir, true
}

var remoteSection = regexp.MustCompile(`^\[remote\s+"([^"]+)"\]`)

// readRemotes parses remote URLs from the repository's git config.
func readRemotes(gitDir string) []Remote {
	if gitDir == "" {
		return nil
	}
	file, err := os.Open(filepath.Join(gitDir, "config"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var remotes []Remote
	current := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			current = ""
			if match := remoteSection.FindStringSubmatch(line); match != nil {
				current = match[1]
			}
			continue
		}
		if current == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "url" {
			remotes = append(remotes, Remote{Name: current, URL: Redact(strings.TrimSpace(value))})
		}
	}
	return remotes
}

var remoteURL = regexp.MustCompile(`(?i)^(?:[a-z+]+://)?(?:[^@/]+@)?([^:/@]+)(?::\d+)?[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// githubSlug returns the owner and repository named by a GitHub remote URL.
// hostName resolves an SSH host alias (e.g. "github-work") to its real host;
// nil treats every host literally.
func githubSlug(remote string, hostName func(string) string) (owner, repo string, ok bool) {
	remote = strings.Replace(strings.TrimSpace(remote), redacted+"@", "", 1)
	match := remoteURL.FindStringSubmatch(remote)
	if match == nil {
		return "", "", false
	}
	host := strings.ToLower(match[1])
	if host != "github.com" && hostName != nil {
		host = strings.ToLower(hostName(host))
	}
	if host != "github.com" {
		return "", "", false
	}
	return match[2], match[3], true
}

// githubRemoteSlug prefers origin, then upstream, then any GitHub remote.
func githubRemoteSlug(remotes []Remote, hostName func(string) string) (owner, repo string, ok bool) {
	for _, preferred := range []string{"origin", "upstream", ""} {
		for _, remote := range remotes {
			if preferred != "" && remote.Name != preferred {
				continue
			}
			if owner, repo, ok := githubSlug(remote.URL, hostName); ok {
				return owner, repo, true
			}
		}
	}
	return "", "", false
}

// sshHostNames reads Host → HostName pairs from the user's SSH config, so a
// remote such as git@github-work:org/repo.git is recognized as GitHub.
func sshHostNames(home string) func(string) string {
	names := map[string]string{}
	file, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err == nil {
		defer file.Close()
		var hosts []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(strings.TrimSpace(scanner.Text()))
			if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "host":
				hosts = fields[1:]
			case "hostname":
				for _, host := range hosts {
					if !strings.ContainsAny(host, "*?") {
						names[strings.ToLower(host)] = fields[1]
					}
				}
			}
		}
	}
	return func(alias string) string {
		if name, ok := names[strings.ToLower(alias)]; ok {
			return name
		}
		return alias
	}
}
