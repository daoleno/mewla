package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/daoleno/mewla/daemon/statedir"
	"github.com/daoleno/mewla/daemon/work"
)

const defaultListenAddr = "127.0.0.1:9876"

// Options configures a doctor run. All fields are optional; defaults match the
// production daemon. Tests inject LookPath/RunCommand/HTTPGet/Listen for
// deterministic PATH/home isolation.
type Options struct {
	StateDir      string
	Addr          string
	ExecutorsPath string
	Home          string
	PathEnv       string
	Now           func() time.Time
	LookPath      func(file string) (string, error)
	RunCommand    func(ctx context.Context, name string, args ...string) ([]byte, error)
	HTTPGet       func(ctx context.Context, url string) (status int, body []byte, err error)
	Listen        func(network, address string) (io.Closer, error)
	ProbeTimeout  time.Duration
}

type env struct {
	opts Options
}

func newEnv(opts Options) env {
	e := env{opts: opts}
	if e.opts.Now == nil {
		e.opts.Now = time.Now
	}
	if e.opts.Addr == "" {
		e.opts.Addr = defaultListenAddr
	}
	if e.opts.ProbeTimeout <= 0 {
		e.opts.ProbeTimeout = 3 * time.Second
	}
	if e.opts.LookPath == nil {
		e.opts.LookPath = e.defaultLookPath
	}
	if e.opts.RunCommand == nil {
		e.opts.RunCommand = e.defaultRunCommand
	}
	if e.opts.HTTPGet == nil {
		e.opts.HTTPGet = e.defaultHTTPGet
	}
	if e.opts.Listen == nil {
		e.opts.Listen = func(network, address string) (io.Closer, error) {
			return net.Listen(network, address)
		}
	}
	return e
}

// CheckTmux runs only the tmux check.
func CheckTmux(opts Options) TmuxCheck {
	return newEnv(opts).checkTmux()
}

// CheckExecutors runs only the executor checks, including auth probes.
func CheckExecutors(opts Options) ExecutorsCheck {
	return newEnv(opts).checkExecutors()
}

// Run performs a full non-destructive environment diagnosis.
func Run(opts Options) (Report, error) {
	e := newEnv(opts)
	report := Report{
		GeneratedAt: e.opts.Now().UTC(),
	}
	report.Platform = e.checkPlatform()
	report.Tmux = e.checkTmux()
	report.StateDir = e.checkStateDir()
	report.Listen = e.checkListen(report.StateDir.Path)
	report.Executors = e.checkExecutors()
	report.Addresses = e.checkAddresses(report.StateDir.Path)
	report.Remote = e.checkRemote(report.StateDir.Path)
	report.Network = e.checkNetwork()

	report.Checks = []NamedCheck{
		{ID: "platform", Status: report.Platform.Status, Remediation: report.Platform.Remediation, Summary: report.Platform.Summary},
		{ID: "tmux", Status: report.Tmux.Status, Remediation: report.Tmux.Remediation, Summary: report.Tmux.Summary},
		{ID: "state_dir", Status: report.StateDir.Status, Remediation: report.StateDir.Remediation, Summary: report.StateDir.Summary},
		{ID: "listen", Status: report.Listen.Status, Remediation: report.Listen.Remediation, Summary: report.Listen.Summary},
		{ID: "executors", Status: report.Executors.Status, Remediation: report.Executors.Remediation, Summary: report.Executors.Summary},
	}
	report.Remediations = collectRemediations(report)
	report.Warnings = collectWarnings(report)
	report.Ready = isReady(report)
	return report, nil
}

func (e env) checkNetwork() NetworkCheck {
	check := NetworkCheck{Status: StatusUnknown, Summary: "Tailscale netcheck unavailable"}
	path, err := e.opts.LookPath("tailscale")
	if err != nil || path == "" {
		return check
	}
	check.TailscaleFound = true
	// A complete netcheck takes about five seconds even on a healthy host.
	// Its JSON format is unstable and stderr includes diagnostic prefixes.
	timeout := max(e.opts.ProbeTimeout, 15*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := e.opts.RunCommand(ctx, path, "netcheck")
	if err != nil {
		check.Status = StatusWarn
		check.Summary = "Tailscale found, but netcheck failed"
		if ctx.Err() != nil {
			check.Summary = "Tailscale netcheck timed out"
		}
		return check
	}
	udp, mapping, relay, ok := parseNetcheckText(string(output))
	if !ok {
		check.Status = StatusWarn
		check.Summary = "Tailscale netcheck returned an unreadable result"
		return check
	}
	check.Direct = udp
	check.Relay = relay
	if mapping == "true" {
		check.NATType = "symmetric"
	} else if udp && mapping == "false" {
		check.NATType = "cone/endpoint-independent"
	} else if !udp {
		check.NATType = "blocked"
	} else {
		check.NATType = "unknown"
	}
	if check.Direct {
		check.Status = StatusOK
		check.Summary = "Tailscale can attempt a direct UDP path"
	} else {
		check.Status = StatusWarn
		check.Summary = "Tailscale will use DERP relay; direct UDP is unavailable"
	}
	return check
}

// parseNetcheckText reads only named report fields, ignoring diagnostic logs.
func parseNetcheckText(output string) (udp bool, mapping, relay string, ok bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "UDP":
			if value == "true" || value == "false" {
				udp, ok = value == "true", true
			}
		case "MappingVariesByDestIP":
			mapping = value
		case "Nearest DERP":
			relay = value
		}
	}
	// Human output gives the region name; its latency row supplies the code.
	if relay != "" {
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") && strings.HasSuffix(line, "("+relay+")") {
				code, _, _ := strings.Cut(strings.TrimPrefix(line, "- "), ":")
				relay = code
				break
			}
		}
	}
	return
}

func (e env) checkAddresses(stateDir string) []AddressCheck {
	book, err := addressbook.New(stateDir)
	if err != nil {
		return nil
	}
	entries, err := book.List()
	if err != nil {
		return nil
	}
	checks := make([]AddressCheck, 0, len(entries))
	for _, entry := range entries {
		started := time.Now()
		status, _, probeErr := e.opts.HTTPGet(context.Background(), strings.TrimRight(entry.URL, "/")+"/health")
		check := AddressCheck{URL: entry.URL, LatencyMS: time.Since(started).Milliseconds()}
		parsedTLS := strings.HasPrefix(entry.URL, "https://")
		check.TLS = parsedTLS
		if probeErr == nil && status == http.StatusOK {
			check.Reachable = true
			check.Status = StatusOK
			if parsedTLS {
				check.Summary = "reachable and TLS verified"
			} else {
				check.Summary = "reachable over local HTTP"
			}
		} else {
			check.Status = StatusWarn
			check.Summary = "unreachable"
			if parsedTLS {
				check.Summary = "unreachable or TLS failed"
			}
		}
		checks = append(checks, check)
	}
	return checks
}

// checkRemote never blocks readiness: remote access is optional.
func (e env) checkRemote(stateDir string) RemoteCheck {
	config, ok, err := remote.LoadRemote(stateDir)
	switch {
	case err != nil:
		return RemoteCheck{Status: StatusWarn, Summary: "remote access config is unreadable: " + err.Error()}
	case !ok:
		if account, _, _ := remote.LoadAccount(stateDir); account.Machine.AccountLogin != "" {
			return RemoteCheck{Status: StatusOK, Summary: "off (signed in as " + account.Machine.AccountLogin + " for Friends; turn on with mewla login)"}
		}
		return RemoteCheck{Status: StatusOK, Summary: "off (turn on with mewla login)"}
	}
	check := RemoteCheck{SignedIn: true, URL: config.URL(), Account: config.Machine.AccountLogin}
	status, running := remote.LoadStatus(stateDir)
	check.Phase, check.Relay, check.CertNotAfter = status.Phase, status.Relay, status.CertNotAfter
	switch {
	case !running:
		check.Status, check.Summary = StatusWarn, config.URL()+": Mewla is not running"
	case status.Phase != "connected":
		check.Status, check.Summary = StatusWarn, config.URL()+": relay "+status.Phase+" "+status.LastError
	case status.CertError != "":
		check.Status, check.Summary = StatusWarn, config.URL()+": certificate: "+status.CertError
	case status.CertNotAfter.IsZero():
		check.Status, check.Summary = StatusWarn, config.URL()+": connected, certificate not issued yet"
	case time.Until(status.CertNotAfter) < 14*24*time.Hour:
		check.Status, check.Summary = StatusWarn, config.URL()+": certificate expires "+status.CertNotAfter.Format("2006-01-02")
	default:
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%s via relay %s, certificate until %s", config.URL(), status.Relay, status.CertNotAfter.Format("2006-01-02"))
	}
	check.Summary = strings.TrimSpace(check.Summary)
	return check
}

func isReady(report Report) bool {
	if report.Platform.Status != StatusOK {
		return false
	}
	if report.Tmux.Status != StatusOK {
		return false
	}
	if report.StateDir.Status != StatusOK {
		return false
	}
	if report.Listen.Status != StatusOK {
		return false
	}
	// StatusWarn is allowed: auth-unknown runnable candidates still satisfy readiness.
	return report.Executors.UsableCount > 0 && report.Executors.Status != StatusFail
}

func collectWarnings(report Report) []string {
	seen := map[string]bool{}
	var out []string
	add := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" || seen[msg] {
			return
		}
		seen[msg] = true
		out = append(out, msg)
	}
	for _, msg := range report.Executors.Warnings {
		add(msg)
	}
	if report.Executors.Status == StatusWarn {
		add(report.Executors.Summary)
	}
	for _, item := range report.Executors.Items {
		if item.Status == StatusWarn && item.Summary != "" {
			add(item.Summary)
		}
	}
	return out
}

func collectRemediations(report Report) []Remediation {
	seen := map[Remediation]bool{}
	var out []Remediation
	add := func(code Remediation) {
		if code == "" || seen[code] {
			return
		}
		seen[code] = true
		out = append(out, code)
	}
	add(report.Platform.Remediation)
	add(report.Tmux.Remediation)
	add(report.StateDir.Remediation)
	add(report.Listen.Remediation)
	add(report.Executors.Remediation)
	for _, item := range report.Executors.Items {
		add(item.Remediation)
	}
	return out
}

func (e env) checkPlatform() PlatformCheck {
	check := PlatformCheck{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		check.Supported = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%s/%s is supported", check.OS, check.Arch)
	default:
		check.Supported = false
		check.Status = StatusFail
		check.Remediation = RemediationUnsupportedPlatform
		check.Summary = fmt.Sprintf("%s/%s is not supported; Mewla requires Linux or macOS", check.OS, check.Arch)
	}
	return check
}

func (e env) checkTmux() TmuxCheck {
	check := TmuxCheck{
		InstallHints: TmuxInstallHints(),
	}
	path, err := e.opts.LookPath("tmux")
	if err != nil || strings.TrimSpace(path) == "" {
		check.Status = StatusFail
		check.Remediation = RemediationInstallTmux
		check.Summary = "tmux not found on PATH"
		return check
	}
	check.Found = true
	check.Path = path

	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()
	out, err := e.opts.RunCommand(ctx, path, "-V")
	if err == nil {
		check.Version = normalizeVersion(string(out))
	}

	if e.probeTmux(path) {
		check.Functional = true
		check.Status = StatusOK
		if check.Version != "" {
			check.Summary = fmt.Sprintf("tmux %s works", check.Version)
		} else {
			check.Summary = "tmux works"
		}
		return check
	}

	check.Status = StatusFail
	check.Remediation = RemediationFixTmux
	check.Summary = "tmux found but functional probe failed"
	return check
}

func (e env) probeTmux(tmuxPath string) bool {
	name := fmt.Sprintf("mewla-doctor-%d", e.opts.Now().UnixNano()%1_000_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	if _, err := e.opts.RunCommand(ctx, tmuxPath, "new-session", "-d", "-s", name, "true"); err != nil {
		return false
	}
	_, _ = e.opts.RunCommand(ctx, tmuxPath, "kill-session", "-t", name)
	return true
}

func (e env) checkStateDir() StateDirCheck {
	path, created, err := e.resolveStateDir()
	check := StateDirCheck{Path: path, Created: created}
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationStateDirUnwritable
		check.Summary = "state directory is not writable"
		return check
	}
	probe := filepath.Join(path, ".mewla-doctor-write-probe")
	if writeErr := os.WriteFile(probe, []byte("ok"), 0o600); writeErr != nil {
		check.Status = StatusFail
		check.Remediation = RemediationStateDirUnwritable
		check.Summary = "state directory is not writable"
		return check
	}
	_ = os.Remove(probe)
	check.Writable = true
	check.Status = StatusOK
	check.Summary = fmt.Sprintf("state directory ready at %s", path)
	return check
}

func (e env) resolveStateDir() (string, bool, error) {
	dir := strings.TrimSpace(e.opts.StateDir)
	if dir == "" {
		if home := strings.TrimSpace(e.opts.Home); home != "" {
			dir = statedir.Default(home)
		} else {
			var err error
			dir, err = auth.DefaultStorageDir()
			if err != nil {
				return "", false, err
			}
		}
	}
	_, err := os.Stat(dir)
	created := errors.Is(err, os.ErrNotExist)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return dir, created, err
	}
	return dir, created, nil
}

func (e env) checkListen(stateDir string) ListenCheck {
	check := ListenCheck{Addr: e.opts.Addr}
	if daemonID, ok := e.detectRunningDaemon(stateDir); ok {
		check.DaemonRunning = true
		check.DaemonID = daemonID
		check.Available = false
		check.Status = StatusOK
		check.Summary = "Mewla daemon already running"
		return check
	}

	closer, err := e.opts.Listen("tcp", e.opts.Addr)
	if err == nil {
		_ = closer.Close()
		check.Available = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("listen address %s is available", e.opts.Addr)
		return check
	}

	check.Available = false
	check.Status = StatusFail
	check.Remediation = RemediationPortInUse
	check.Summary = fmt.Sprintf("listen address %s is in use by a non-Mewla process", e.opts.Addr)
	return check
}

func (e env) detectRunningDaemon(stateDir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	url := "http://" + e.opts.Addr + "/health"
	status, body, err := e.opts.HTTPGet(ctx, url)
	if err == nil && status == http.StatusOK {
		var payload struct {
			Status   string `json:"status"`
			DaemonID string `json:"daemon_id"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Status == "ok" && strings.TrimSpace(payload.DaemonID) != "" {
			return payload.DaemonID, true
		}
	}

	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return "", false
	}
	if _, err := os.Stat(socketPath); err != nil {
		return "", false
	}
	resp, err := control.Call(socketPath, control.Request{Type: "worker_list"})
	if err == nil && resp.OK {
		return "", true
	}
	return "", false
}

func (e env) checkExecutors() ExecutorsCheck {
	path := strings.TrimSpace(e.opts.ExecutorsPath)
	if path == "" {
		home := strings.TrimSpace(e.opts.Home)
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				home = ""
			}
		}
		if home != "" {
			path = filepath.Join(statedir.Default(home), "executors.toml")
		} else if p, err := work.DefaultExecutorsPath(); err == nil {
			path = p
		}
	}

	check := ExecutorsCheck{ConfigPath: path}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			check.ConfigExists = true
		}
	}

	cfg, err := work.LoadExecutors(path)
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationConfigureExecutor
		check.Summary = "failed to load executors.toml"
		return check
	}
	if len(cfg.DeprecatedKeys) > 0 {
		check.Warnings = append(check.Warnings, fmt.Sprintf("executors.toml: %s are retired and ignored; Brain routes Workers from its routing.md", strings.Join(cfg.DeprecatedKeys, ", ")))
	}

	for _, name := range cfg.Roles() {
		executor := cfg.ByName[name]
		check.Items = append(check.Items, e.probeExecutor(name, executor))
	}

	var usable []ExecutorCheck
	for _, item := range check.Items {
		if item.Usable {
			usable = append(usable, item)
			check.UsableCount++
		}
		if item.VerifiedAuthenticated {
			check.VerifiedCount++
		}
	}

	verified := filterByAuth(usable, AuthAuthenticated)
	unknown := filterByAuth(usable, AuthUnknown)
	recommendFrom := verified
	check.RecommendationConfidence = ConfidenceVerified
	if len(recommendFrom) == 0 {
		recommendFrom = unknown
		if len(recommendFrom) > 0 {
			check.RecommendationConfidence = ConfidenceUnverified
		} else {
			check.RecommendationConfidence = ConfidenceNone
		}
	}
	check.RecommendedHost = recommendHost(recommendFrom)

	switch {
	case check.VerifiedCount > 0:
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%d usable executor(s), %d verified; host=%s", check.UsableCount, check.VerifiedCount, check.RecommendedHost)
	case check.UsableCount > 0:
		check.Status = StatusWarn
		check.Summary = fmt.Sprintf("%d runnable executor(s) with unverified auth; host=%s", check.UsableCount, check.RecommendedHost)
		check.Warnings = append(check.Warnings, "auth state is unknown for recommended executors; Mewla did not pretend they are authenticated")
		if check.RecommendationConfidence == ConfidenceUnverified {
			check.Warnings = append(check.Warnings, "recommendations selected auth-unknown candidates because no verified authenticated executor exists")
		}
	case anyAuth(check.Items, AuthUnauthenticated):
		check.Status = StatusFail
		check.Remediation = RemediationAuthenticateExec
		check.Summary = "executor binaries found but all probed executors are unauthenticated"
		check.RecommendationConfidence = ConfidenceNone
	default:
		check.Status = StatusFail
		check.Remediation = RemediationInstallExecutor
		check.Summary = "no usable executor found; install and authenticate at least one of Claude Code, Codex, Cursor Agent, Grok, Pi, or OpenCode"
		check.RecommendationConfidence = ConfidenceNone
	}
	return check
}

func filterByAuth(items []ExecutorCheck, want AuthState) []ExecutorCheck {
	out := make([]ExecutorCheck, 0, len(items))
	for _, item := range items {
		if item.Auth == want {
			out = append(out, item)
		}
	}
	return out
}

func anyAuth(items []ExecutorCheck, want AuthState) bool {
	for _, item := range items {
		if item.BinaryFound && item.Auth == want {
			return true
		}
	}
	return false
}

func (e env) probeExecutor(name string, executor work.Executor) ExecutorCheck {
	worker := work.NewWorkerExecutor(name, executor)
	item := ExecutorCheck{
		ID:           worker.ID,
		Name:         worker.Name,
		Provider:     worker.Provider,
		Configured:   true,
		Command:      worker.Command,
		Auth:         AuthUnknown,
		Capabilities: worker.Capabilities,
	}

	bin := firstCommandToken(worker.Command)
	if bin == "" {
		item.Status = StatusFail
		item.Remediation = RemediationConfigureExecutor
		item.Summary = "executor command is empty"
		return item
	}

	path, err := e.opts.LookPath(bin)
	if err != nil || strings.TrimSpace(path) == "" {
		item.BinaryFound = false
		item.Status = StatusFail
		item.Remediation = RemediationInstallExecutor
		item.Summary = fmt.Sprintf("%s binary not found on PATH", bin)
		return item
	}
	item.BinaryFound = true
	item.BinaryPath = path
	item.Version = e.probeVersion(path, worker.Provider)
	if worker.Provider == work.WorkerProviderOpenCode {
		version := opencodeapi.ParseVersion([]byte(item.Version))
		if opencodeapi.MajorVersion(version) != opencodeapi.SupportedMajor {
			item.Status = StatusFail
			item.Remediation = RemediationInstallExecutor
			item.Summary = fmt.Sprintf("OpenCode %s found; Mewla supports OpenCode 2 only. Upgrade: %s", openCodeVersionLabel(version), hostdeps.OpenCodeInstall)
			return item
		}
	}
	item.Auth = e.probeAuth(path, worker.Provider)
	if worker.Provider == work.WorkerProviderOpenCode {
		e.probeOpenCodeRuntime(path, &item)
	}

	switch item.Auth {
	case AuthAuthenticated:
		item.Runnable = true
		item.Usable = true
		item.VerifiedAuthenticated = true
		item.Status = StatusOK
		item.Summary = fmt.Sprintf("%s ready (%s, auth verified)", item.ID, item.Provider)
	case AuthUnauthenticated:
		item.Runnable = false
		item.Usable = false
		item.Status = StatusFail
		item.Remediation = RemediationAuthenticateExec
		item.Summary = fmt.Sprintf("%s found but not authenticated", item.ID)
	default:
		// Auth unknown: treat as runnable candidate with warning, not a hard fail.
		item.Runnable = true
		item.Usable = true
		item.VerifiedAuthenticated = false
		item.Status = StatusWarn
		item.Summary = fmt.Sprintf("%s runnable (%s); auth state unknown", item.ID, item.Provider)
	}
	if worker.Provider == work.WorkerProviderOpenCode {
		item.Summary = appendOpenCodeProbeSummary(item.Summary, item.ModelsStatus, item.ServiceStatus)
	}
	return item
}

func openCodeVersionLabel(version string) string {
	if version == "" {
		return "of unknown version"
	}
	return version
}

// probeOpenCodeRuntime runs secret-safe, non-mutating OpenCode 2 probes:
// `models` and `service status` (the background server Mewla reads sessions
// through). Output bodies are never copied into the report.
func (e env) probeOpenCodeRuntime(binaryPath string, item *ExecutorCheck) {
	item.ModelsStatus = StatusUnknown
	item.ServiceStatus = StatusUnknown

	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()
	modelsOut, modelsErr := e.opts.RunCommand(ctx, binaryPath, "models")
	item.ModelsStatus = parseOpenCodeModelsProbe(modelsOut, modelsErr)

	ctx2, cancel2 := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel2()
	serviceOut, serviceErr := e.opts.RunCommand(ctx2, binaryPath, "service", "status")
	item.ServiceStatus = parseOpenCodeServiceProbe(serviceOut, serviceErr)
}

func parseOpenCodeModelsProbe(out []byte, err error) Status {
	if err != nil {
		return StatusFail
	}
	lines := 0
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "refresh") && strings.Contains(lower, "error") {
			return StatusFail
		}
		lines++
	}
	if lines == 0 {
		return StatusUnknown
	}
	return StatusOK
}

// parseOpenCodeServiceProbe accepts `opencode service status` printing the
// running server's URL.
func parseOpenCodeServiceProbe(out []byte, err error) Status {
	if err != nil {
		return StatusFail
	}
	text := strings.TrimSpace(string(out))
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return StatusOK
	}
	return StatusUnknown
}

func appendOpenCodeProbeSummary(summary string, models, service Status) string {
	parts := make([]string, 0, 2)
	switch models {
	case StatusOK:
		parts = append(parts, "models ok")
	case StatusFail:
		parts = append(parts, "models probe failed")
	case StatusUnknown:
		parts = append(parts, "models unknown")
	}
	switch service {
	case StatusOK:
		parts = append(parts, "service running")
	case StatusFail:
		parts = append(parts, "service probe failed")
	case StatusUnknown:
		parts = append(parts, "service not running")
	}
	if len(parts) == 0 {
		return summary
	}
	return summary + "; " + strings.Join(parts, ", ")
}

func (e env) probeVersion(binaryPath, provider string) string {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()
	out, err := e.opts.RunCommand(ctx, binaryPath, "--version")
	if err != nil {
		// Some CLIs use -v.
		out, err = e.opts.RunCommand(ctx, binaryPath, "-v")
		if err != nil {
			return ""
		}
	}
	return normalizeVersion(string(out))
}

func (e env) probeAuth(binaryPath, provider string) AuthState {
	ctx, cancel := context.WithTimeout(context.Background(), e.opts.ProbeTimeout)
	defer cancel()

	switch provider {
	case work.WorkerProviderCodex:
		out, err := e.opts.RunCommand(ctx, binaryPath, "login", "status")
		return parseCodexAuth(out, err)
	case work.WorkerProviderClaude:
		// Claude prints its JSON status and exits 1 when logged out.
		out, _ := e.opts.RunCommand(ctx, binaryPath, "auth", "status", "--json")
		return parseClaudeAuth(out)
	case work.WorkerProviderCursor:
		out, _ := e.opts.RunCommand(ctx, binaryPath, "status", "--format", "json")
		return parseCursorAuth(out)
	case work.WorkerProviderGrok:
		// Grok has login but no safe official non-interactive status command.
		return AuthUnknown
	case work.WorkerProviderOpenCode:
		out, _ := e.opts.RunCommand(ctx, binaryPath, "auth", "list", "--format", "json")
		return parseOpenCodeAuth(out)
	case work.WorkerProviderPi:
		return probePiAuth()
	default:
		return AuthUnknown
	}
}

// Auth probes read each CLI's structured answer where it has one: Claude,
// Cursor and OpenCode print JSON. Codex only has `login status`, which exits 0
// when logged in and 1 otherwise, saying "Not logged in" when it knows; any
// other answer is AuthUnknown, never a guess. Raw output is never returned or
// logged: it may embed API key fragments or account identifiers.

func parseCodexAuth(out []byte, err error) AuthState {
	if err == nil {
		return AuthAuthenticated
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && hasLineEqual(out, "not logged in") {
		return AuthUnauthenticated
	}
	// A failed probe (missing flag, broken config, timeout) says nothing.
	return AuthUnknown
}

// hasLineEqual reports whether one trimmed line of out equals want, ignoring case.
func hasLineEqual(out []byte, want string) bool {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), want) {
			return true
		}
	}
	return false
}

// decodeProbeJSON decodes the first JSON document in a probe's output. The
// runner merges stderr into the output, so a CLI's warning lines may come
// before the document; only a line that starts a document that decodes counts.
func decodeProbeJSON(out []byte, into any) bool {
	for offset := 0; offset < len(out); {
		end := bytes.IndexByte(out[offset:], '\n')
		line := out[offset:]
		if end >= 0 {
			line = out[offset : offset+end]
		}
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			if json.NewDecoder(bytes.NewReader(out[offset:])).Decode(into) == nil {
				return true
			}
		}
		if end < 0 {
			break
		}
		offset += end + 1
	}
	return false
}

func parseClaudeAuth(out []byte) AuthState {
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if !decodeProbeJSON(out, &status) || status.LoggedIn == nil {
		return AuthUnknown
	}
	if *status.LoggedIn {
		return AuthAuthenticated
	}
	return AuthUnauthenticated
}

func parseCursorAuth(out []byte) AuthState {
	var status struct {
		Status          string `json:"status"`
		IsAuthenticated *bool  `json:"isAuthenticated"`
	}
	if !decodeProbeJSON(out, &status) {
		return AuthUnknown
	}
	switch strings.ToLower(strings.TrimSpace(status.Status)) {
	case "authenticated":
		return AuthAuthenticated
	case "unauthenticated":
		return AuthUnauthenticated
	}
	if status.IsAuthenticated != nil {
		if *status.IsAuthenticated {
			return AuthAuthenticated
		}
		return AuthUnauthenticated
	}
	// Empty object / missing fields → unknown, never default-false.
	return AuthUnknown
}

// parseOpenCodeAuth reads `opencode auth list --format json`: a list of
// integrations, each with its connections (a stored credential or an
// environment variable). Any connection means a provider is usable; a list
// with none means OpenCode has nothing to authenticate with.
func parseOpenCodeAuth(out []byte) AuthState {
	var integrations []struct {
		Connections []json.RawMessage `json:"connections"`
	}
	if !decodeProbeJSON(out, &integrations) || integrations == nil {
		return AuthUnknown
	}
	for _, integration := range integrations {
		if len(integration.Connections) > 0 {
			return AuthAuthenticated
		}
	}
	return AuthUnauthenticated
}

func probePiAuth() AuthState {
	home, err := os.UserHomeDir()
	if err != nil {
		return AuthUnknown
	}
	authPath := filepath.Join(home, ".pi", "agent", "auth.json")
	info, err := os.Stat(authPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		if hasPiProviderEnv() {
			return AuthUnknown
		}
		return AuthUnknown
	}
	// Presence of auth.json is not proof of a usable provider credential.
	return AuthUnknown
}

func hasPiProviderEnv() bool {
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"GOOGLE_API_KEY",
		"GEMINI_API_KEY",
		"OPENROUTER_API_KEY",
		"PI_API_KEY",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// recommendHost picks Brain's host from usable executors in hostdeps'
// AgentOrder, the same order the install menu uses.
func recommendHost(usable []ExecutorCheck) string {
	if len(usable) == 0 {
		return ""
	}
	order := hostdeps.AgentOrder()
	for _, id := range order {
		for _, item := range usable {
			if item.ID == id {
				return id
			}
		}
	}
	// Executors under other ids rank by their provider.
	for _, id := range order {
		for _, item := range usable {
			if provider := agentProviders[id]; provider != "" && item.Provider == provider {
				return item.ID
			}
		}
	}
	return usable[0].ID
}

// agentProviders maps hostdeps.AgentOrder ids to their worker provider.
var agentProviders = map[string]string{
	"claude":   work.WorkerProviderClaude,
	"codex":    work.WorkerProviderCodex,
	"agent":    work.WorkerProviderCursor,
	"grok":     work.WorkerProviderGrok,
	"pi":       work.WorkerProviderPi,
	"opencode": work.WorkerProviderOpenCode,
}

func firstCommandToken(command string) string {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func normalizeVersion(raw string) string {
	line := strings.TrimSpace(raw)
	if line == "" {
		return ""
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "tmux"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "codex-cli"))
	return line
}

func (e env) defaultLookPath(file string) (string, error) {
	if pathEnv := strings.TrimSpace(e.opts.PathEnv); pathEnv != "" {
		return lookPathEnv(file, pathEnv)
	}
	return exec.LookPath(file)
}

func lookPathEnv(file, pathEnv string) (string, error) {
	if strings.Contains(file, string(os.PathSeparator)) {
		return file, nil
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, file)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func (e env) defaultRunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if pathEnv := strings.TrimSpace(e.opts.PathEnv); pathEnv != "" {
		cmd.Env = append(os.Environ(), "PATH="+pathEnv)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

func (e env) defaultHTTPGet(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	client := &http.Client{Timeout: e.opts.ProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}
