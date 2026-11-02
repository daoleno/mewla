package connections

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// cloudProvider is the vendor boundary. Login parsing/validation, environment,
// command policy and installation belong to the provider; process ownership,
// time/output bounds and account gates are shared. gcloud can register here.
type cloudProvider interface {
	binary() string
	commandArgs(string, []string) []string
	syncProfiles(*Manager, context.Context, bool) error
	profileRecord(*Manager, string) *record
	validProfile(string) bool
	environment() []string
	loginArgs(profile string) []string
	loginURL(output string) string
	validateCode(address, code string) bool
	classify(args []string) (read bool, err error)
	install(context.Context, string) error
}
type cloudAccount struct {
	LoginAt        *time.Time `json:"login_at,omitempty"`
	LoginSession   string     `json:"login_session,omitempty"`
	Account        string     `json:"account,omitempty"`
	Configuration  string     `json:"configuration,omitempty"`
	Configurations []string   `json:"configurations,omitempty"`
	Profile        string     `json:"profile"`
}
type cloudOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
}
type cloudRuntime struct {
	profileFiles func() (string, string, error)
	dir          string
	providers    map[string]cloudProvider
	run          func(context.Context, string, []string, []string) (cloudOutput, error)
	checked      map[string]time.Time
	updates      *cloudUpdates
}

func newCloudRuntime(dir string) *cloudRuntime {
	return &cloudRuntime{profileFiles: awsProfileFiles, dir: dir, providers: map[string]cloudProvider{"aws": awsCLI{}, "gcp": gcloudCLI{}}, run: runCloudProcess, checked: map[string]time.Time{}}
}
func (c *cloudRuntime) executable(id string) (string, error) {
	c.refresh(id, false)
	return c.resolveExecutable(id)
}

func (c *cloudRuntime) resolveExecutable(id string) (string, error) {
	p := c.providers[id]
	if p == nil {
		return "", errors.New("Cloud CLI is not supported")
	}
	validate := func(path string) bool {
		if provider, ok := p.(interface {
			validateExecutable(context.Context, string) error
		}); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return provider.validateExecutable(ctx, path) == nil
		}
		return true
	}
	paths := []string{filepath.Join(c.dir, id, "bin", p.binary())}
	if path, err := exec.LookPath(p.binary()); err == nil {
		paths = append(paths, path)
	}
	if provider, ok := p.(interface{ installPaths() []string }); ok {
		paths = append(paths, provider.installPaths()...)
	}
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode()&0111 != 0) && validate(path) {
			// Bind the command to an immutable generation before activation changes.
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				return resolved, nil
			}
			return path, nil
		}
	}
	return "", errors.New("Install the CLI to continue")
}

// boundedBuffer drains output without retaining unbounded data; it is also used
// by interactive login processes whose output is never logged or persisted.
type boundedBuffer struct {
	mu       sync.Mutex
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := b.limit - len(b.data)
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *boundedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }
func runCloudProcess(ctx context.Context, binary string, args, env []string) (cloudOutput, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	ownCloudProcess(cmd)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	out := &boundedBuffer{limit: MaxResultBytes}
	stderr := &boundedBuffer{limit: 16384}
	cmd.Stdout = out
	cmd.Stderr = stderr
	err := cmd.Run()
	result := cloudOutput{Stdout: out.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if out.overflow || stderr.overflow {
		return result, errors.New("CLI output exceeded the limit; narrow the command")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
			return result, nil
		}
		return result, errors.New("CLI could not start")
	}
	return result, nil
}

type cloudLogin struct {
	cancel context.CancelFunc
	stdin  io.WriteCloser
	output *boundedBuffer
	done   chan struct{}
	err    error // read only after done closes
	sent   bool  // guarded by Manager.mu
}

func startCloudLogin(binary string, args, env []string) (*cloudLogin, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd := exec.CommandContext(ctx, binary, args...)
	ownCloudProcess(cmd)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	login := &cloudLogin{cancel: cancel, stdin: input, output: &boundedBuffer{limit: 32768}, done: make(chan struct{})}
	cmd.Stdout = login.output
	cmd.Stderr = login.output
	if err = cmd.Start(); err != nil {
		cancel()
		input.Close()
		return nil, errors.New("CLI sign-in could not start")
	}
	go func() { login.err = cmd.Wait(); input.Close(); cancel(); close(login.done) }()
	return login, nil
}
func (m *Manager) cloudCommand(ctx context.Context, integration, profile string, args []string) (cloudOutput, error) {
	if m.cloud == nil {
		return cloudOutput{}, errors.New("Cloud CLI is unavailable")
	}
	binary, err := m.cloud.executable(integration)
	if err != nil {
		return cloudOutput{}, err
	}
	p := m.cloud.providers[integration]
	// The caller's argv cannot replace profile, endpoint, config or credential routing.
	argv := p.commandArgs(profile, args)
	if router, ok := p.(interface {
		accountArgs(*Manager, string, []string) []string
	}); ok {
		argv = router.accountArgs(m, profile, args)
	}
	return m.cloud.run(ctx, binary, argv, p.environment())
}
func cloudArgs(args []string) bool {
	if len(args) < 2 || len(args) > 256 {
		return false
	}
	size := 0
	for _, a := range args {
		size += len(a)
		if strings.ContainsAny(a, "\x00\r\n") {
			return false
		}
	}
	return size <= 65536
}

// No command text reaches a shell. Also reject CLI file loaders: their paths
// refer to the daemon host, not the remote caller's filesystem.
func cloudForbidden(args []string) error {
	for _, a := range args {
		k, _, _ := strings.Cut(a, "=")
		if strings.HasPrefix(k, "--") {
			for _, forbidden := range []string{"--profile", "--endpoint-url", "--no-verify-ssl", "--ca-bundle", "--debug", "--no-sign-request", "--cli-auto-prompt"} {
				if strings.HasPrefix(forbidden, k) {
					return callRefused("This option cannot override the connected account")
				}
			}
		}
		switch k {
		case "--profile", "--endpoint-url", "--no-verify-ssl", "--ca-bundle", "--debug", "--no-sign-request", "--cli-auto-prompt":
			return callRefused("This option cannot override the connected account")
		}
		if strings.Contains(a, "file://") || strings.Contains(a, "fileb://") {
			return callRefused("File arguments are unavailable on a remote CLI")
		}
	}
	return nil
}

// Close reaps all interactive logins owned by this daemon.
func (m *Manager) Close() {
	if m.snapshots != nil {
		m.snapshots.mu.Lock()
		m.snapshots.cancel()
		m.snapshots.mu.Unlock()
		m.snapshots.wg.Wait()
	}
	if m.cloud != nil && m.cloud.updates != nil {
		m.cloud.updates.mu.Lock()
		m.cloud.updates.cancel()
		m.cloud.updates.mu.Unlock()
		m.cloud.updates.wg.Wait()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.connectFlows {
		if f.cli != nil {
			f.cli.cancel()
		}
	}
}
