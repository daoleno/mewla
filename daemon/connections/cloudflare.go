package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CloudflareVersion is the release of Cloudflare's cf CLI that Mewla installs.
// cf is in beta: its commands can change between releases, so a newer one is
// adopted on purpose, not by installing whatever is latest.
const CloudflareVersion = "1.0.0-beta.16"

// cf needs Node.js 22.18 or later, and runs on Node, not Bun.
const cloudflareNodeMinimum = "22.18.0"

// cf's device sign-in gives this long to approve; Mewla stops waiting after it.
const cloudflareLoginWindow = 5*time.Minute + 15*time.Second

// CloudflareStatus is this machine's cf CLI: whether it's installed, and the
// Cloudflare sign-in every Brain and Worker shell on the machine shares. It is
// read from cf itself each time, never stored by Mewla.
type CloudflareStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Pinned    string `json:"pinned"`
	Path      string `json:"path,omitempty"`
	// OnPath: agents' shells find this cf by name. False when npm installed it
	// into a directory that is not on the daemon's PATH.
	OnPath     bool `json:"on_path"`
	Installing bool `json:"installing,omitempty"`
	// Problem is what stops installing or signing in, such as a missing Node.js.
	Problem  string              `json:"problem,omitempty"`
	SignedIn bool                `json:"signed_in"`
	Email    string              `json:"email,omitempty"`
	Accounts []CloudflareAccount `json:"accounts,omitempty"`
	// Token: the sign-in is the CLOUDFLARE_API_TOKEN in the daemon's
	// environment, which cf prefers and Mewla can't sign out.
	Token bool             `json:"token,omitempty"`
	Login *CloudflareLogin `json:"login,omitempty"`
	// Message is how the last install, sign-in or sign-out ended.
	Message string `json:"message,omitempty"`
}

type CloudflareAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloudflareLogin is a device sign-in waiting for approval at URL with Code.
type CloudflareLogin struct {
	URL     string    `json:"url"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
}

type cloudflareWhoami struct {
	at       time.Time
	signedIn bool
	email    string
	accounts []CloudflareAccount
	token    bool
	expired  bool
}

// cloudflareCLI runs cf for the Extensions page. Install and sign-in outlive
// the request that starts them and run outside the plugin manager's lock.
type cloudflareCLI struct {
	mu         sync.Mutex
	dir        string // cf's working directory: cf caches the chosen account there
	installing bool
	login      *cloudflareLoginProcess
	whoami     *cloudflareWhoami
	message    string
}

type cloudflareLoginProcess struct {
	CloudflareLogin
	cmd    *exec.Cmd
	output *lockedBuffer
	done   chan struct{}
}

// shown is the sign-in once cf has printed its link and code. Callers hold c.mu.
func (p *cloudflareLoginProcess) shown() *CloudflareLogin {
	if p.Code == "" {
		if link, code, ok := parseCloudflareLogin(p.output.String()); ok {
			p.URL, p.Code, p.Expires = link, code, time.Now().Add(5*time.Minute)
		}
	}
	if p.Code == "" {
		return nil
	}
	login := p.CloudflareLogin
	return &login
}

func newCloudflareCLI(dir string) *cloudflareCLI { return &cloudflareCLI{dir: dir} }

func (c *cloudflareCLI) handle(ctx context.Context, q Request) (Response, error) {
	if c == nil {
		return Response{}, errors.New("Cloudflare is unavailable on this server")
	}
	switch q.Action {
	case "cloudflare_status":
	case "cloudflare_install":
		if err := c.install(ctx); err != nil {
			return Response{}, err
		}
	case "cloudflare_login":
		if err := c.startLogin(ctx); err != nil {
			return Response{}, err
		}
	case "cloudflare_cancel":
		c.cancelLogin()
	case "cloudflare_logout":
		if err := c.logout(ctx); err != nil {
			return Response{}, err
		}
	default:
		return Response{}, errors.New("unknown Cloudflare action")
	}
	status := c.status(ctx, q.Refresh)
	return Response{Cloudflare: &status}, nil
}

// locate finds cf: on PATH as agents' shells would, else in npm's global bin.
// A cf that doesn't print a bare version is another program by that name,
// such as Cloud Foundry's.
func (c *cloudflareCLI) locate(ctx context.Context) (path, version string, onPath bool) {
	type candidate struct {
		path   string
		onPath bool
	}
	candidates := []candidate{}
	if found, err := exec.LookPath("cf"); err == nil {
		candidates = append(candidates, candidate{found, true})
	}
	if prefix, err := c.output(ctx, 10*time.Second, "npm", "prefix", "--global"); err == nil && strings.TrimSpace(prefix) != "" {
		candidates = append(candidates, candidate{filepath.Join(strings.TrimSpace(prefix), "bin", "cf"), false})
	}
	for _, found := range candidates {
		out, err := c.output(ctx, 15*time.Second, found.path, "--version")
		if v := strings.TrimSpace(out); err == nil && cloudflareVersionPattern.MatchString(v) {
			return found.path, v, found.onPath
		}
	}
	return "", "", false
}

var cloudflareVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)

func (c *cloudflareCLI) status(ctx context.Context, refresh bool) CloudflareStatus {
	s := CloudflareStatus{Pinned: CloudflareVersion}
	s.Path, s.Version, s.OnPath = c.locate(ctx)
	s.Installed = s.Path != ""
	if !s.Installed {
		s.Problem = c.nodeProblem(ctx)
	}
	c.mu.Lock()
	s.Installing = c.installing
	s.Message = c.message
	if c.login != nil {
		s.Login = c.login.shown()
	}
	cached := c.whoami
	c.mu.Unlock()
	if !s.Installed || s.Installing {
		return s
	}
	if refresh || cached == nil || time.Since(cached.at) > time.Minute {
		if fresh, err := c.readWhoami(ctx, s.Path); err == nil {
			c.mu.Lock()
			c.whoami = fresh
			c.mu.Unlock()
			cached = fresh
		}
	}
	if cached != nil {
		s.SignedIn, s.Email, s.Accounts, s.Token = cached.signedIn, cached.email, cached.accounts, cached.token
		if cached.expired && s.Message == "" && s.Login == nil {
			s.Message = "cf has a Cloudflare sign-in, but Cloudflare didn't accept it. It may have expired, or the server may be offline. Sign in again."
		}
	}
	return s
}

// readWhoami asks cf who it is signed in as. cf exits 0 when nobody is signed
// in, so only its JSON says.
func (c *cloudflareCLI) readWhoami(ctx context.Context, path string) (*cloudflareWhoami, error) {
	out, err := c.output(ctx, 20*time.Second, path, "auth", "whoami")
	if err != nil {
		return nil, err
	}
	return parseCloudflareWhoami([]byte(out))
}

func parseCloudflareWhoami(raw []byte) (*cloudflareWhoami, error) {
	var w struct {
		Authenticated bool              `json:"authenticated"`
		AuthSource    string            `json:"authSource"`
		TokenValid    *bool             `json:"tokenValid"`
		Email         string            `json:"email"`
		Accounts      []json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.New("cf auth whoami returned unexpected output")
	}
	result := &cloudflareWhoami{at: time.Now()}
	if !w.Authenticated {
		return result, nil
	}
	if w.TokenValid != nil && !*w.TokenValid {
		result.expired = true
		return result, nil
	}
	result.signedIn = true
	result.email = w.Email
	result.token = strings.Contains(w.AuthSource, "CLOUDFLARE_API_TOKEN")
	for _, item := range w.Accounts {
		var account CloudflareAccount
		if json.Unmarshal(item, &account) == nil && (account.ID != "" || account.Name != "") {
			result.accounts = append(result.accounts, account)
		}
	}
	return result, nil
}

// nodeProblem says what stops npm installing cf on this server, if anything.
func (c *cloudflareCLI) nodeProblem(ctx context.Context) string {
	const need = "Installing cf needs Node.js 22.18 or later and npm on this server."
	if _, err := exec.LookPath("npm"); err != nil {
		return need
	}
	out, err := c.output(ctx, 10*time.Second, "node", "--version")
	if err != nil || !versionAtLeast(strings.TrimPrefix(strings.TrimSpace(out), "v"), cloudflareNodeMinimum) {
		return need
	}
	return ""
}

// versionAtLeast compares the numeric major.minor.patch of two versions.
func versionAtLeast(have, need string) bool {
	parse := func(v string) [3]int {
		var out [3]int
		v, _, _ = strings.Cut(v, "-")
		for i, part := range strings.SplitN(v, ".", 3) {
			n, err := strconv.Atoi(part)
			if err != nil {
				return [3]int{-1}
			}
			out[i] = n
		}
		return out
	}
	h, n := parse(have), parse(need)
	for i := range h {
		if h[i] != n[i] {
			return h[i] > n[i]
		}
	}
	return true
}

// install puts the pinned cf on this machine with npm. It runs in the
// background; status reports Installing until it ends.
func (c *cloudflareCLI) install(ctx context.Context) error {
	if problem := c.nodeProblem(ctx); problem != "" {
		return errors.New(problem)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.installing {
		return nil
	}
	if c.login != nil {
		return errors.New("Finish or cancel the Cloudflare sign-in first")
	}
	c.installing = true
	c.message = ""
	go func() {
		out, err := c.output(context.Background(), 10*time.Minute, "npm", "install", "--global", "--no-fund", "--no-audit", "cf@"+CloudflareVersion)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.installing = false
		c.whoami = nil
		if err != nil {
			c.message = "cf couldn't be installed: " + lastLine(out, err)
			return
		}
		c.message = "Installed cf " + CloudflareVersion + "."
	}()
	return nil
}

// startLogin begins cf's device sign-in and returns once cf prints the link
// and code. cf keeps waiting for approval after that.
func (c *cloudflareCLI) startLogin(ctx context.Context) error {
	path, _, _ := c.locate(ctx)
	if path == "" {
		return errors.New("Install cf first")
	}
	c.mu.Lock()
	if c.installing {
		c.mu.Unlock()
		return errors.New("cf is still installing")
	}
	if c.login != nil {
		c.mu.Unlock()
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		c.mu.Unlock()
		return errors.New("Cloudflare sign-in could not start")
	}
	cmd := exec.Command(path, "auth", "login", "--no-browser", "--force")
	cmd.Dir = c.dir
	cmd.Stdin = nil
	cmd.SysProcAttr = cloudflareProcAttr()
	output := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return errors.New("Cloudflare sign-in could not start")
	}
	p := &cloudflareLoginProcess{cmd: cmd, output: output, done: make(chan struct{})}
	c.login = p
	c.message = ""
	c.mu.Unlock()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	go c.watchLogin(p, exited)

	// cf prints the link and code within a second or two of asking Cloudflare.
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			c.mu.Lock()
			defer c.mu.Unlock()
			if strings.HasPrefix(c.message, "Signed in") {
				return nil
			}
			return errors.New(c.message)
		case <-deadline.C:
			c.cancelLogin()
			return errors.New("Cloudflare didn't start a sign-in. Try again.")
		case <-ctx.Done():
			// The sign-in keeps waiting; the next status shows its code.
			return nil
		case <-tick.C:
			c.mu.Lock()
			shown := c.login == p && p.shown() != nil
			c.mu.Unlock()
			if shown {
				return nil
			}
		}
	}
}

// watchLogin ends a sign-in when cf exits, or stops cf once its code expires.
func (c *cloudflareCLI) watchLogin(p *cloudflareLoginProcess, exited <-chan error) {
	var err error
	select {
	case err = <-exited:
	case <-time.After(cloudflareLoginWindow):
		_ = p.cmd.Process.Kill()
		err = <-exited
		err = fmt.Errorf("expired: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.login == p {
		c.login = nil
		c.whoami = nil
		switch {
		case err == nil:
			c.message = "Signed in to Cloudflare."
		case strings.HasPrefix(err.Error(), "expired"):
			c.message = "The Cloudflare code expired before it was approved. Sign in again."
		default:
			c.message = "Cloudflare sign-in didn't finish: " + lastLine(p.output.String(), err)
		}
	}
	close(p.done)
}

func (c *cloudflareCLI) cancelLogin() {
	c.mu.Lock()
	p := c.login
	if p != nil {
		c.login = nil
		c.message = "Cloudflare sign-in cancelled."
	}
	c.mu.Unlock()
	if p != nil {
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func (c *cloudflareCLI) logout(ctx context.Context) error {
	path, _, _ := c.locate(ctx)
	if path == "" {
		return errors.New("cf is not installed")
	}
	c.cancelLogin()
	out, err := c.output(ctx, 20*time.Second, path, "auth", "logout")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.whoami = nil
	if err != nil {
		return errors.New("cf couldn't sign out: " + lastLine(out, err))
	}
	c.message = "Signed out of Cloudflare."
	return nil
}

// cloudflareLoginLink matches the page cf names; only Cloudflare's dashboard
// is ever offered to open.
var cloudflareLoginLink = regexp.MustCompile(`https://dash\.cloudflare\.com/[^\s]*`)
var cloudflareLoginCode = regexp.MustCompile(`(?s)enter the code:\s*([A-Za-z0-9-]{4,16})\s`)

func parseCloudflareLogin(output string) (link, code string, ok bool) {
	link = cloudflareLoginLink.FindString(output)
	match := cloudflareLoginCode.FindStringSubmatch(output)
	if link == "" || match == nil {
		return "", "", false
	}
	if u, err := url.Parse(link); err != nil || u.Scheme != "https" || u.Host != "dash.cloudflare.com" || u.User != nil {
		return "", "", false
	}
	return link, match[1], true
}

// output runs a short cf, npm or node command in cf's working directory.
func (c *cloudflareCLI) output(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	_ = os.MkdirAll(c.dir, 0o700)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.dir
	cmd.SysProcAttr = cloudflareProcAttr()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}

// lastLine is the last thing a failed command said, kept short.
func lastLine(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			if len(line) > 300 {
				line = line[:300]
			}
			return line
		}
	}
	if err != nil {
		return err.Error()
	}
	return "no output"
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.buf.Len() > 64<<10 {
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
