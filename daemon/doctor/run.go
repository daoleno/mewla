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
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/opencodeapi"
	"github.com/daoleno/mewla/daemon/platform"
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
	// ExecutorID limits an executor check to one configured agent. An empty ID
	// keeps the full explicit doctor diagnosis.
	ExecutorID string
	// Passive reports installation without starting an agent command.
	Passive      bool
	Home         string
	PathEnv      string
	Now          func() time.Time
	LookPath     func(file string) (string, error)
	RunCommand   func(ctx context.Context, name string, args ...string) ([]byte, error)
	HTTPGet      func(ctx context.Context, url string) (status int, body []byte, err error)
	Listen       func(network, address string) (io.Closer, error)
	ProbeTimeout time.Duration
	// GOOS is the platform diagnosed; empty means this one.
	GOOS string
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
	if e.opts.GOOS == "" {
		e.opts.GOOS = runtime.GOOS
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

// CheckExecutors runs only the executor checks. Sign-in is never checked.
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
	report.StateDir = e.checkStateDir()
	report.Listen = e.checkListen(report.StateDir.Path)
	report.Executors = e.checkExecutors()
	report.Addresses = e.checkAddresses(report.StateDir.Path)
	report.Remote = e.checkRemote(report.StateDir.Path)
	report.Network = e.checkNetwork()
	report.Screen = e.checkScreen()

	report.Checks = []NamedCheck{
		{ID: "platform", Status: report.Platform.Status, Remediation: report.Platform.Remediation, Summary: report.Platform.Summary},
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
	if report.StateDir.Status != StatusOK {
		return false
	}
	if report.Listen.Status != StatusOK {
		return false
	}
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
		OS:   e.opts.GOOS,
		Arch: runtime.GOARCH,
	}
	switch e.opts.GOOS {
	case "linux", "darwin":
		check.Supported = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%s/%s is supported", check.OS, check.Arch)
	case "windows":
		check.Supported = true
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%s/%s is supported; Sessions run in Mewla's session host", check.OS, check.Arch)
	default:
		check.Supported = false
		check.Status = StatusFail
		check.Remediation = RemediationUnsupportedPlatform
		check.Summary = fmt.Sprintf("%s/%s is not supported; Mewla requires Linux, macOS or Windows", check.OS, check.Arch)
	}
	return check
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
		if e.opts.ExecutorID != "" && name != e.opts.ExecutorID {
			continue
		}
		executor := cfg.ByName[name]
		check.Items = append(check.Items, e.probeExecutor(name, executor))
	}

	var usable []ExecutorCheck
	for _, item := range check.Items {
		if item.Usable {
			usable = append(usable, item)
			check.UsableCount++
		}
	}

	check.RecommendedHost = recommendHost(usable)

	if check.UsableCount > 0 {
		check.Status = StatusOK
		check.Summary = fmt.Sprintf("%d usable executor(s); host=%s", check.UsableCount, check.RecommendedHost)
	} else {
		check.Status = StatusFail
		check.Remediation = RemediationInstallExecutor
		check.Summary = "no usable executor found; install at least one of Claude Code, Codex, Cursor Agent, Grok, Pi, OpenCode, or Antigravity"
	}
	return check
}

func (e env) probeExecutor(name string, executor work.Executor) ExecutorCheck {
	worker := work.NewWorkerExecutor(name, executor)
	item := ExecutorCheck{
		ID:           worker.ID,
		Name:         worker.Name,
		Provider:     worker.Provider,
		Configured:   true,
		Command:      worker.Command,
		ConfigDir:    worker.ConfigDir,
		Capabilities: worker.Capabilities,
	}

	bin := firstCommandToken(worker.BareCommand)
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
	if e.opts.Passive {
		item.Runnable = true
		item.Usable = true
		item.Status = StatusWarn
		item.Summary = fmt.Sprintf("%s installed; version not checked", item.ID)
		return item
	}
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
	if worker.Provider == work.WorkerProviderOpenCode {
		e.probeOpenCodeRuntime(path, &item)
	}
	item.Runnable = true
	item.Usable = true
	item.Status = StatusOK
	item.Summary = fmt.Sprintf("%s ready (%s)", item.ID, item.Provider)
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

// versionKey identifies one build of an agent CLI: a program that has not been
// replaced prints the same version, so it is asked once.
type versionKey struct {
	path    string
	size    int64
	modTime int64
}

var knownVersions = struct {
	sync.Mutex
	byBuild map[versionKey]string
}{byBuild: map[versionKey]string{}}

func (e env) probeVersion(binaryPath, provider string) string {
	info, statErr := os.Stat(binaryPath)
	if statErr != nil {
		return e.askVersion(binaryPath)
	}
	key := versionKey{path: binaryPath, size: info.Size(), modTime: info.ModTime().UnixNano()}
	knownVersions.Lock()
	version, known := knownVersions.byBuild[key]
	knownVersions.Unlock()
	if known {
		return version
	}
	version = e.askVersion(binaryPath)
	if version != "" {
		knownVersions.Lock()
		if len(knownVersions.byBuild) >= 64 {
			knownVersions.byBuild = map[versionKey]string{}
		}
		knownVersions.byBuild[key] = version
		knownVersions.Unlock()
	}
	return version
}

func (e env) askVersion(binaryPath string) string {
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
	"agy":      work.WorkerProviderAntigravity,
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
	cmd := platform.WithoutConsoleWindow(exec.CommandContext(ctx, name, args...))
	if pathEnv := strings.TrimSpace(e.opts.PathEnv); pathEnv != "" {
		cmd.Env = append(os.Environ(), "PATH="+pathEnv)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	// A CLI whose --version leaves a child holding its output (a wrapper
	// that starts a server) would keep Wait reading forever after ctx kills
	// it, and with it every caller: Brain's host checks, and through them the
	// daemon's shutdown and its update.
	cmd.WaitDelay = probeWaitDelay
	err := cmd.Run()
	return buf.Bytes(), err
}

// probeWaitDelay bounds how long a finished or killed probe waits for its
// output to close.
const probeWaitDelay = 2 * time.Second

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
