// Package agentuse lets agents on this computer use it the way the user does:
// the user's own Chrome, with its logins, through the Playwright extension.
// It needs no configuration. One switch per computer, on by default, turns it
// off; when something is missing the status says what in one line and, where
// Mewla can, offers the one action that fixes it.
package agentuse

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

//go:embed playwright/package.json
var playwrightPackage []byte

//go:embed playwright/package-lock.json
var playwrightLock []byte

// Browser states, from the user's side.
const (
	BrowserReady       = "ready"
	BrowserOff         = "off"
	BrowserNoChrome    = "no_chrome"
	BrowserNoExtension = "no_extension"
	BrowserNoKey       = "no_key"
	BrowserNoNode      = "no_node"
	BrowserNoDesktop   = "no_desktop"
	BrowserPreparing   = "preparing"
	BrowserFailed      = "failed"
)

// Fixes the app can ask this computer to perform.
const (
	FixInstallExtension = "install_extension"
	FixOpenExtension    = "open_extension"
	FixRetry            = "retry"
)

// ExtensionID is the Playwright Extension in the Chrome Web Store. Its
// manifest carries a fixed key, so an unpacked build has the same ID.
const ExtensionID = "mmlmfjhmonkocbjadbfplnigmagldckm"

const (
	storeURL         = "https://chromewebstore.google.com/detail/playwright-extension/" + ExtensionID
	extensionPageURL = "chrome-extension://" + ExtensionID + "/status.html"
	statusTTL        = 15 * time.Second
	retryInstall     = 10 * time.Minute
	installTimeout   = 5 * time.Minute
	// recheckEvery is how often the daemon looks again while the browser
	// isn't ready, so an extension or key added while Mewla runs is picked
	// up without anyone asking.
	recheckEvery = 30 * time.Second
)

type BrowserStatus struct {
	State string `json:"state"`
	// Summary is the one line the app shows.
	Summary string `json:"summary"`
	// Fix is the one action that resolves State, when Mewla can do it.
	Fix     string `json:"fix,omitempty"`
	Profile string `json:"profile,omitempty"`
	// KeyPasted is set when the extension's key was pasted instead of read.
	KeyPasted bool `json:"key_pasted,omitempty"`
}

type Status struct {
	Enabled  bool            `json:"enabled"`
	Browser  BrowserStatus   `json:"browser"`
	Computer *ComputerStatus `json:"computer,omitempty"`
	// CodexComputerUse is set when the user's Codex config gives Codex its
	// own computer use. Codex Workers here get it while the switch is on.
	CodexComputerUse bool `json:"codex_computer_use,omitempty"`
}

type settings struct {
	// Off is stored instead of "enabled" so a missing file means on.
	Off bool   `json:"off,omitempty"`
	Key string `json:"pasted_key,omitempty"`
}

type Service struct {
	dir  string
	home string
	// prepare lets this process download the browser tools. Only the daemon
	// does; a short-lived command would leave a half-finished install.
	prepare bool
	// Lookups, replaced in tests.
	lookChrome  func(home string) (chromeInstall, bool)
	lookNode    func(home string) (node, npm string, ok bool)
	lookDesktop func() ([]string, bool)
	lookCodex   func(home string) bool
	recheck     time.Duration

	mu           sync.Mutex
	settings     settings
	cached       Status
	cachedAt     time.Time
	installing   bool
	installErr   error
	installErrAt time.Time
	computer     *ComputerHooks
}

// New reads this computer's switch from stateDir. It never touches Chrome.
// The daemon passes prepare=true so it can fetch the browser tools.
func New(stateDir string, prepare bool) (*Service, error) {
	home, _ := os.UserHomeDir()
	s := &Service{dir: filepath.Join(stateDir, "agent-use"), home: home, prepare: prepare, lookChrome: findChrome, lookNode: findNode, lookDesktop: findDesktop, lookCodex: findCodexComputerUse, recheck: recheckEvery}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// loadLocked rereads the switch: the file is shared by the daemon and the
// mewla command, so it is the one truth.
func (s *Service) loadLocked() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		s.settings = settings{}
		return nil
	}
	if err != nil {
		return err
	}
	var next settings
	if err := json.Unmarshal(raw, &next); err != nil {
		return fmt.Errorf("decode agent-use settings: %w", err)
	}
	s.settings = next
	return nil
}

// Status reports what agents can use now. It is cached briefly: reading the
// extension's key copies a small database.
func (s *Service) Status() Status {
	s.mu.Lock()
	if time.Since(s.cachedAt) < statusTTL {
		status := s.cached
		s.mu.Unlock()
		return s.withComputer(status)
	}
	s.mu.Unlock()
	return s.Refresh()
}

// BrowserReady says whether to give a new agent the user's Chrome.
func (s *Service) BrowserReady() bool {
	return s.Status().Browser.State == BrowserReady
}

func (s *Service) Refresh() Status {
	return s.withComputer(s.refreshBrowser())
}

func (s *Service) refreshBrowser() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.loadLocked()
	status := Status{Enabled: !s.settings.Off, CodexComputerUse: s.lookCodex(s.home)}
	if status.Enabled {
		status.Browser, _ = s.browserLocked()
	} else {
		status.Browser = BrowserStatus{State: BrowserOff, Summary: "Agents can't use this computer. Turn it on to let them."}
	}
	if s.prepare && status.Browser.State != s.cached.Browser.State {
		log.Printf("agent use: your Chrome: %s", status.Browser.State)
	}
	s.cached, s.cachedAt = status, time.Now()
	return status
}

// Run looks again on its own while the browser isn't ready: the status is
// otherwise only refreshed when something asks, such as an agent starting,
// so a computer left alone would never fetch the browser tools.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(s.recheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		ready := s.cached.Browser.State == BrowserReady
		s.mu.Unlock()
		if !ready {
			s.Refresh()
		}
	}
}

// launch is everything needed to start the browser tools.
type launch struct {
	node, cli, chrome, profile, key string
	desktop                         []string
}

func (s *Service) browserLocked() (BrowserStatus, launch) {
	chrome, ok := s.lookChrome(s.home)
	if !ok {
		return BrowserStatus{State: BrowserNoChrome, Summary: "Google Chrome isn't installed on this computer."}, launch{}
	}
	desktop, ok := s.lookDesktop()
	if !ok {
		return BrowserStatus{State: BrowserNoDesktop, Summary: "Sign in to this computer's desktop so agents can open Chrome there."}, launch{}
	}
	profile, ok := extensionProfile(chrome.UserDataDir)
	if !ok {
		return BrowserStatus{State: BrowserNoExtension, Summary: "Add the Playwright extension to Chrome so agents can use your logged-in sites.", Fix: FixInstallExtension}, launch{}
	}
	status := BrowserStatus{Profile: profile}
	// The extension's key is the person's to hand over: the extension shows it
	// on its status page and checks it when the browser tools connect. Mewla
	// does not read it out of Chrome's storage.
	key := s.settings.Key
	if key == "" {
		status.State, status.Fix = BrowserNoKey, FixOpenExtension
		status.Summary = "Copy the Playwright extension's key from its page in Chrome and paste it here."
		return status, launch{}
	}
	status.KeyPasted = true
	node, npm, ok := s.lookNode(s.home)
	if !ok {
		status.State = BrowserNoNode
		status.Summary = fmt.Sprintf("Install Node.js %d or later on this computer for the browser tools.", nodeMajorMinimum)
		return status, launch{}
	}
	cli := filepath.Join(s.packageDir(), "node_modules", "@playwright", "mcp", "cli.js")
	if !s.installedLocked() {
		if s.installErr != nil && time.Since(s.installErrAt) < retryInstall {
			status.State, status.Fix = BrowserFailed, FixRetry
			status.Summary = "Couldn't download the browser tools: " + firstLine(s.installErr.Error())
			return status, launch{}
		}
		if !s.installing && s.prepare {
			s.installing = true
			go s.install(node, npm)
		}
		status.State, status.Summary = BrowserPreparing, "Getting the browser tools ready…"
		return status, launch{}
	}
	status.State = BrowserReady
	status.Summary = "Agents use your Chrome, in their own tab group."
	return status, launch{node: node, cli: cli, chrome: chrome.Executable, profile: profile, key: key, desktop: desktop}
}

func (s *Service) packageDir() string { return filepath.Join(s.dir, "playwright-mcp") }

// browserFilesBudget bounds the files agents' browser tools leave behind.
const browserFilesBudget = 256 << 20

func (s *Service) browserFilesDir() string { return filepath.Join(s.dir, "browser-files") }

func lockStamp() string {
	sum := sha256.Sum256(playwrightLock)
	return hex.EncodeToString(sum[:])
}

func (s *Service) installedLocked() bool {
	stamp, err := os.ReadFile(filepath.Join(s.packageDir(), ".mewla-installed"))
	return err == nil && strings.TrimSpace(string(stamp)) == lockStamp()
}

// install puts the pinned Playwright MCP server in Mewla's state, from the
// lockfile's integrity hashes and without running package scripts.
func (s *Service) install(node, npm string) {
	err := s.runInstall(node, npm)
	if err != nil {
		log.Printf("agent use: couldn't fetch the browser tools: %v", err)
	}
	s.mu.Lock()
	s.installing = false
	s.installErr, s.installErrAt = err, time.Now()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

func (s *Service) runInstall(node, npm string) error {
	dir := s.packageDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, ".mewla-installed"))
	if err := atomicfile.Write(filepath.Join(dir, "package.json"), playwrightPackage, 0o600); err != nil {
		return err
	}
	if err := atomicfile.Write(filepath.Join(dir, "package-lock.json"), playwrightLock, 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, npm, "ci", "--ignore-scripts", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(node)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return atomicfile.Write(filepath.Join(dir, ".mewla-installed"), []byte(lockStamp()+"\n"), 0o600)
}

// SetEnabled is the app's one switch for this computer.
func (s *Service) SetEnabled(enabled bool) (Status, error) {
	s.mu.Lock()
	_ = s.loadLocked()
	next := s.settings
	next.Off = !enabled
	err := s.saveLocked(next)
	hooks := s.computer
	s.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	if !enabled && hooks != nil {
		// Off means now: an agent using the screen stops at once.
		hooks.Stop()
	}
	return s.Refresh(), nil
}

// SetKey stores a pasted extension key; an empty key goes back to reading it
// from Chrome.
func (s *Service) SetKey(key string) (Status, error) {
	key = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(key), "PLAYWRIGHT_MCP_EXTENSION_TOKEN="))
	if key != "" && !validToken(key) {
		return Status{}, errors.New("that doesn't look like the extension's key")
	}
	s.mu.Lock()
	_ = s.loadLocked()
	next := s.settings
	next.Key = key
	err := s.saveLocked(next)
	s.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	return s.Refresh(), nil
}

func (s *Service) saveLocked(next settings) error {
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	// The pasted key is a credential for the user's browser.
	if err := atomicfile.Write(filepath.Join(s.dir, "settings.json"), raw, 0o600); err != nil {
		return err
	}
	s.settings = next
	return nil
}

// Fix performs one of the status's fixes on this computer: it opens the
// extension's store page or its status page (where it shows its key) in the
// user's Chrome, or retries the download.
func (s *Service) Fix(fix string) (Status, error) {
	switch fix {
	case FixInstallExtension, FixOpenExtension:
		chrome, ok := s.lookChrome(s.home)
		if !ok {
			return Status{}, errors.New("Google Chrome isn't installed on this computer")
		}
		args := []string{storeURL}
		if fix == FixOpenExtension {
			args = []string{extensionPageURL}
			if profile, ok := extensionProfile(chrome.UserDataDir); ok {
				args = append([]string{"--profile-directory=" + profile}, args...)
			}
		}
		// Chrome hands the page to the running browser, or starts it.
		cmd := exec.Command(chrome.Executable, args...)
		if err := cmd.Start(); err != nil {
			return Status{}, fmt.Errorf("open Chrome: %w", err)
		}
		go func() { _ = cmd.Wait() }()
	case FixRetry:
		s.mu.Lock()
		s.installErr = nil
		s.mu.Unlock()
	case FixOpenSettings:
		s.mu.Lock()
		hooks := s.computer
		s.mu.Unlock()
		if hooks == nil {
			return Status{}, errors.New("no agent can use this computer's screen")
		}
		permission := hooks.Status().Permission
		if permission == "" {
			return s.Refresh(), nil
		}
		if err := hooks.OpenSettings(permission); err != nil {
			return Status{}, err
		}
	default:
		return Status{}, fmt.Errorf("unknown fix %q", fix)
	}
	return s.Refresh(), nil
}

// MCPServer returns the browser tools' command for an agent, or why there is
// none. The extension's key travels only in the child's environment.
func (s *Service) MCPServer() (argv []string, env []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, nil, err
	}
	if s.settings.Off {
		return nil, nil, errors.New("agents can't use this computer: it is turned off in the app")
	}
	status, l := s.browserLocked()
	if status.State != BrowserReady {
		return nil, nil, errors.New(status.Summary)
	}
	if err := os.MkdirAll(s.browserFilesDir(), 0o700); err != nil {
		return nil, nil, err
	}
	argv = []string{l.node, l.cli, "--extension", "--executable-path", l.chrome, "--profile-dir-name", l.profile,
		// An action answers with what changed, not the whole page: a busy
		// app's page is hundreds of thousands of tokens, which the agent
		// asks for, when it needs it, with browser_find or browser_snapshot.
		"--snapshot-mode", "none",
		// An action's answer doesn't repeat the Playwright code it ran: for
		// browser_run_code_unsafe that is the agent's whole script again.
		"--codegen", "none",
		// Files Playwright names itself (console logs, screenshots,
		// downloads) stay in Mewla's state, not in the Worker's project,
		// within a budget that drops the oldest first.
		"--output-dir", s.browserFilesDir(), "--output-max-size", strconv.Itoa(browserFilesBudget), "--file-paths", "absolute",
	}
	// Agents' MCP hosts pass only a few variables on; Chrome also needs the
	// desktop session, found by the daemon side.
	replaced := map[string]bool{"PATH": true}
	for _, entry := range l.desktop {
		name, _, _ := strings.Cut(entry, "=")
		replaced[name] = true
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !replaced[name] && !strings.HasPrefix(name, "PLAYWRIGHT_MCP_") {
			env = append(env, entry)
		}
	}
	env = append(env, l.desktop...)
	env = append(env,
		"PLAYWRIGHT_MCP_EXTENSION_TOKEN="+l.key,
		"PATH="+filepath.Dir(l.node)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	return argv, env, nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 160 {
		line = line[:160] + "…"
	}
	return line
}

// validToken accepts the extension's base64url keys and nothing that could
// smuggle other text into an environment variable.
func validToken(token string) bool {
	if len(token) < 16 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '=') {
			return false
		}
	}
	return true
}
