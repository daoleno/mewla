package connections

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	Profile string `json:"profile"`
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
}

func newCloudRuntime(dir string) *cloudRuntime {
	return &cloudRuntime{profileFiles: awsProfileFiles, dir: dir, providers: map[string]cloudProvider{"aws": awsCLI{}}, run: runCloudProcess, checked: map[string]time.Time{}}
}
func (c *cloudRuntime) executable(id string) (string, error) {
	p := c.providers[id]
	if p == nil {
		return "", errors.New("Cloud CLI is not supported")
	}
	if path, err := exec.LookPath(p.binary()); err == nil {
		return path, nil
	}
	path := filepath.Join(c.dir, id, "bin", p.binary())
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
		return path, nil
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
	return m.cloud.run(ctx, binary, p.commandArgs(profile, args), p.environment())
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
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.connectFlows {
		if f.cli != nil {
			f.cli.cancel()
		}
	}
}
