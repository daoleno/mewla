package agentuse

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/hostdeps"
)

// chromeInstall is the user's everyday Google Chrome on this computer.
type chromeInstall struct {
	Executable  string
	UserDataDir string
}

// findChrome looks only where Google Chrome installs itself. Chrome from a
// sandboxed package (Flatpak, Snap) cannot be reached by the extension's
// local connection, so it is reported as missing.
func findChrome(home string) (chromeInstall, bool) {
	switch runtime.GOOS {
	case "darwin":
		candidates := []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"))
		}
		for _, candidate := range candidates {
			if isExecutable(candidate) {
				return chromeInstall{candidate, filepath.Join(home, "Library", "Application Support", "Google", "Chrome")}, true
			}
		}
	case "linux":
		config := os.Getenv("CHROME_CONFIG_HOME")
		if config == "" {
			config = os.Getenv("XDG_CONFIG_HOME")
		}
		if config == "" && home != "" {
			config = filepath.Join(home, ".config")
		}
		for _, candidate := range []string{"/opt/google/chrome/chrome", "google-chrome-stable", "google-chrome"} {
			path := candidate
			if !filepath.IsAbs(candidate) {
				found, err := exec.LookPath(candidate)
				if err != nil {
					continue
				}
				path = found
			}
			if isExecutable(path) {
				return chromeInstall{path, filepath.Join(config, "google-chrome")}, true
			}
		}
	}
	return chromeInstall{}, false
}

var profileDirName = regexp.MustCompile(`^(Default|Profile \d+)$`)

// extensionProfile picks the Chrome profile that has the extension, the
// last-used one first, the same choice the Playwright MCP server makes.
func extensionProfile(userDataDir string) (string, bool) {
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return "", false
	}
	var profiles []string
	for _, entry := range entries {
		if entry.IsDir() && profileDirName.MatchString(entry.Name()) {
			profiles = append(profiles, entry.Name())
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profileRank(profiles[i]) < profileRank(profiles[j]) })
	if last := lastUsedProfile(userDataDir); last != "" {
		for i, profile := range profiles {
			if profile == last {
				profiles = append([]string{last}, append(profiles[:i:i], profiles[i+1:]...)...)
				break
			}
		}
	}
	for _, profile := range profiles {
		if extensionInstalled(filepath.Join(userDataDir, profile)) {
			return profile, true
		}
	}
	return "", false
}

func profileRank(profile string) int {
	if profile == "Default" {
		return -1
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(profile, "Profile "))
	return n
}

func lastUsedProfile(userDataDir string) string {
	raw, err := os.ReadFile(filepath.Join(userDataDir, "Local State"))
	if err != nil {
		return ""
	}
	var state struct {
		Profile struct {
			LastUsed string `json:"last_used"`
		} `json:"profile"`
	}
	if json.Unmarshal(raw, &state) != nil {
		return ""
	}
	return state.Profile.LastUsed
}

func extensionInstalled(profileDir string) bool {
	if info, err := os.Stat(filepath.Join(profileDir, "Extensions", ExtensionID)); err == nil && info.IsDir() {
		return true
	}
	for _, name := range []string{"Preferences", "Secure Preferences"} {
		raw, err := os.ReadFile(filepath.Join(profileDir, name))
		if err != nil {
			continue
		}
		var prefs struct {
			Extensions struct {
				Settings map[string]json.RawMessage `json:"settings"`
			} `json:"extensions"`
		}
		if json.Unmarshal(raw, &prefs) == nil {
			if _, ok := prefs.Extensions.Settings[ExtensionID]; ok {
				return true
			}
		}
	}
	return false
}

// nodeMajorMinimum is what the pinned Playwright MCP server needs.
const nodeMajorMinimum = 20

// findNode finds a Node.js new enough for the browser tools: on PATH, where
// installers put it, and in version managers' install trees, which a
// service started at login does not have on PATH.
func findNode(home string) (node string, npm string, ok bool) {
	var candidates []string
	if path, err := hostdeps.LookPath("node"); err == nil {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, "/opt/homebrew/bin/node", "/usr/local/bin/node", "/usr/bin/node")
	if home != "" {
		for _, pattern := range []string{
			".local/share/mise/installs/node/*/bin/node",
			".nvm/versions/node/*/bin/node",
			".volta/bin/node",
			".local/share/fnm/node-versions/*/installation/bin/node",
			"Library/Application Support/fnm/node-versions/*/installation/bin/node",
		} {
			matches, _ := filepath.Glob(filepath.Join(home, pattern))
			sort.Slice(matches, func(i, j int) bool { return nodeVersionFromPath(matches[i]) > nodeVersionFromPath(matches[j]) })
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if !isExecutable(candidate) {
			continue
		}
		if major := nodeMajor(candidate); major < nodeMajorMinimum {
			continue
		}
		npm := filepath.Join(filepath.Dir(candidate), "npm")
		if !isExecutable(npm) {
			continue
		}
		return candidate, npm, true
	}
	return "", "", false
}

func nodeMajor(node string) int {
	out, err := exec.Command(node, "--version").Output()
	if err != nil {
		return 0
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	major, _ := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	return major
}

// nodeVersionFromPath orders version-manager installs newest first.
func nodeVersionFromPath(path string) int {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		fields := strings.Split(strings.TrimPrefix(part, "v"), ".")
		if len(fields) != 3 {
			continue
		}
		score := 0
		for _, field := range fields {
			n, err := strconv.Atoi(field)
			if err != nil {
				score = -1
				break
			}
			score = score*1000 + n
		}
		if score >= 0 {
			return score
		}
	}
	return 0
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
