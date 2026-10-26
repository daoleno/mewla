package friends

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// Machine says where to look for this computer's private names.
type Machine struct {
	// StateDir is the Mewla state directory (default ~/.mewla).
	StateDir string
	HomeDir  string
	// ProjectRoots are directories whose children are projects, such as
	// ~/workspace.
	ProjectRoots []string
	Hostname     func() (string, error)
	// Owner is the person's public handle, which stays allowed.
	Owner string
}

// CollectVocabulary reads names from this computer: host, user, home, Work
// project folders, project directories and their git remotes, connected
// accounts, paired computers and saved addresses. It only reads the fields it
// needs; credentials files are never opened.
func CollectVocabulary(m Machine) *Vocabulary {
	v := &Vocabulary{}
	add := func(terms ...string) {
		for _, t := range terms {
			if t = strings.TrimSpace(t); t != "" {
				v.Terms = append(v.Terms, t)
			}
		}
	}
	hostname := m.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	if name, err := hostname(); err == nil {
		add(name)
		if short, _, found := strings.Cut(name, "."); found {
			add(short)
		}
	}
	if current, err := user.Current(); err == nil {
		add(current.Username, current.Name)
	}
	if m.HomeDir != "" {
		add(filepath.Base(m.HomeDir))
	}
	if m.Owner != "" {
		v.Allow = append(v.Allow, m.Owner)
	}

	if entries, err := os.ReadDir(filepath.Join(m.StateDir, "work")); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				add(entry.Name())
			}
		}
	}
	for _, root := range m.ProjectRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			add(entry.Name())
			add(remoteNames(filepath.Join(root, entry.Name(), ".git", "config"))...)
		}
	}

	// Watch knows every project directory this computer has worked in,
	// wherever it lives, with its remotes and GitHub owner and repository.
	var projects []struct {
		Name    string `json:"name"`
		Root    string `json:"root"`
		Remotes []struct {
			URL string `json:"url"`
		} `json:"remotes"`
		GitHub *struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
		} `json:"github"`
	}
	if readJSON(filepath.Join(m.StateDir, "watch", "projects.json"), &projects) {
		for _, project := range projects {
			add(project.Name)
			if project.Root != "" {
				add(filepath.Base(project.Root))
			}
			for _, remote := range project.Remotes {
				add(remoteURLNames(remote.URL)...)
			}
			if project.GitHub != nil {
				add(project.GitHub.Owner, project.GitHub.Repo)
			}
		}
	}

	var integrations map[string]struct {
		Account struct {
			Name     string `json:"name"`
			Identity string `json:"identity"`
		} `json:"account"`
	}
	if readJSON(filepath.Join(m.StateDir, "integrations.json"), &integrations) {
		for _, record := range integrations {
			add(record.Account.Name, record.Account.Identity)
		}
	}
	var computers struct {
		Peers []struct {
			Name string   `json:"name"`
			URLs []string `json:"urls"`
		} `json:"peers"`
	}
	if readJSON(filepath.Join(m.StateDir, "computers", "computers.json"), &computers) {
		for _, peer := range computers.Peers {
			add(peer.Name)
			for _, raw := range peer.URLs {
				add(hostLabels(raw)...)
			}
		}
	}
	var addresses struct {
		Entries []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if readJSON(filepath.Join(m.StateDir, "addresses.json"), &addresses) {
		for _, entry := range addresses.Entries {
			add(hostLabels(entry.URL)...)
		}
	}
	return v
}

func readJSON(path string, out any) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// hostLabels returns the host and each dotted label of a URL's host. IP
// literals are left to the format rules.
func hostLabels(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	host := parsed.Hostname()
	if strings.Count(host, ":") > 0 || strings.Trim(host, "0123456789.") == "" {
		return nil
	}
	return strings.Split(host, ".")
}

// remoteNames returns owner and repo names from the url lines of a git config.
func remoteNames(configPath string) []string {
	file, err := os.Open(configPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	var names []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		value, found := strings.CutPrefix(line, "url")
		if !found {
			continue
		}
		names = append(names, remoteURLNames(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "=")))...)
	}
	return names
}

// remoteURLNames returns the repository and owner of a git remote URL.
func remoteURLNames(value string) []string {
	value = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(value), "/"), ".git")
	i := strings.LastIndexAny(value, "/:")
	if i < 0 {
		return nil
	}
	names := []string{value[i+1:]}
	if j := strings.LastIndexAny(value[:i], "/:"); j >= 0 {
		names = append(names, value[:i][j+1:])
	}
	return names
}
