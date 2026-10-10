package watcher

import (
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/workerproc"
)

type processInfo struct {
	pid       int
	ppid      int
	pgid      int
	tpgid     int
	startedAt time.Time
	comm      string
	args      string
}

func snapshotProcesses() map[int]processInfo {
	command := exec.Command("ps", "-eo", "pid=,ppid=,pgid=,tpgid=,lstart=,comm=,args=")
	command.Env = append(command.Environ(), "LC_ALL=C")
	out, err := command.Output()
	if err != nil {
		return nil
	}
	return parseProcessSnapshot(out, time.Local)
}

func parseProcessSnapshot(out []byte, location *time.Location) map[int]processInfo {
	if location == nil {
		location = time.Local
	}
	processes := make(map[int]processInfo)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		tpgid, err4 := strconv.Atoi(fields[3])
		startedAt, err5 := time.ParseInLocation(
			"Mon Jan 2 15:04:05 2006",
			strings.Join(fields[4:9], " "),
			location,
		)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}

		args := ""
		if len(fields) > 10 {
			args = strings.Join(fields[10:], " ")
		}

		processes[pid] = processInfo{
			pid:       pid,
			ppid:      ppid,
			pgid:      pgid,
			tpgid:     tpgid,
			startedAt: startedAt,
			comm:      fields[9],
			args:      args,
		}
	}
	return processes
}

type foregroundTargetAuthority struct {
	command    string
	foreground processInfo
	provider   processInfo
}

// resolveForegroundTargetProcess binds terminal mutation authority to both the
// kernel's foreground process-group leader and one coherent Provider process
// in that leader's same-PGID descendant lineage. Other process groups cannot
// influence the result; conflicting or indeterminate foreground Providers
// fail closed.
func resolveForegroundTargetProcess(panePID int, processes map[int]processInfo) (foregroundTargetAuthority, bool) {
	paneProcess, ok := processes[panePID]
	if !ok || paneProcess.startedAt.IsZero() || paneProcess.tpgid <= 0 {
		return foregroundTargetAuthority{}, false
	}
	foregroundPID := paneProcess.tpgid
	foreground, ok := processes[foregroundPID]
	if !ok ||
		foreground.pid != foregroundPID ||
		foreground.pgid != foregroundPID ||
		foreground.startedAt.IsZero() ||
		!processDescendsFrom(panePID, foregroundPID, processes) {
		return foregroundTargetAuthority{}, false
	}

	providerFamily := ""
	bestScore := -1
	bestCommand := ""
	bestProcess := processInfo{}
	bestTied := false
	resumeCommand := ""
	var providerLineage []processInfo
	for _, process := range processes {
		if process.pgid != foregroundPID {
			continue
		}
		detected := workerCommandFromProcess(process)
		if detected == "" {
			continue
		}
		if process.startedAt.IsZero() ||
			!processDescendsFrom(foregroundPID, process.pid, processes) {
			return foregroundTargetAuthority{}, false
		}
		family := workerProviderFamily(detected)
		if family == "" {
			return foregroundTargetAuthority{}, false
		}
		for _, previous := range providerLineage {
			if !processDescendsFrom(previous.pid, process.pid, processes) &&
				!processDescendsFrom(process.pid, previous.pid, processes) {
				// Live-control Codex launches legitimately place two sibling
				// subtrees in the pane's foreground process group: the headless
				// app-server support process and the interactive --remote TUI
				// client (each half may itself be a node wrapper plus its
				// native descendant, and the app-server half is forked by the
				// pane shell before it execs the TUI). Only that
				// support/client sibling combination is determinate; every
				// other sibling provider combination is indeterminate and
				// fails closed.
				if family != "codex" ||
					isCodexAppServerProcess(previous.args) == isCodexAppServerProcess(process.args) {
					return foregroundTargetAuthority{}, false
				}
			}
		}
		providerLineage = append(providerLineage, process)
		if providerFamily == "" {
			providerFamily = family
		} else if providerFamily != family {
			return foregroundTargetAuthority{}, false
		}
		if resume, _ := commandResumeArg(detected); resume {
			if resumeCommand != "" && resumeCommand != detected {
				return foregroundTargetAuthority{}, false
			}
			resumeCommand = detected
		}
		score := workerProcessScore(process, detected)
		switch {
		case score > bestScore:
			bestScore = score
			bestCommand = detected
			bestProcess = process
			bestTied = false
		case score == bestScore && process.pid != bestProcess.pid:
			bestTied = true
		}
	}
	if providerFamily != "" {
		if bestTied || bestCommand == "" || bestProcess.pid <= 0 {
			return foregroundTargetAuthority{}, false
		}
		if providerFamily == "codex" {
			// A headless Codex app-server support process is never the
			// interactive pane identity: without a provable TUI client the pane
			// is not a sendable agent surface. This also covers the brief launch
			// window in which the native app-server half out-scores the node
			// wrapper TUI before the native TUI descendant exists.
			if isCodexAppServerProcess(bestProcess.args) {
				return foregroundTargetAuthority{}, false
			}
			// The installed Codex CLI is a node launcher whose native binary is
			// the interactive process (node rewrites its comm, e.g. "MainThread").
			// While the best candidate is still the launcher, the pane identity
			// is converging: fail closed so the ready-wait keeps polling instead
			// of freezing on an identity that the mutation-boundary re-proof
			// cannot reproduce once the native descendant exists.
			if normalizeCommand(bestProcess.comm) != "codex" {
				return foregroundTargetAuthority{}, false
			}
		}
		if resumeCommand != "" && workerProviderFamily(bestCommand) == providerFamily {
			bestCommand = resumeCommand
		}
		return foregroundTargetAuthority{
			command:    bestCommand,
			foreground: foreground,
			provider:   bestProcess,
		}, true
	}

	command := normalizeCommand(foreground.comm)
	if command == "" {
		return foregroundTargetAuthority{}, false
	}
	return foregroundTargetAuthority{
		command:    command,
		foreground: foreground,
		provider:   foreground,
	}, true
}

func foregroundTargetProcess(panePID int, processes map[int]processInfo) (string, time.Time, int, bool) {
	authority, ok := resolveForegroundTargetProcess(panePID, processes)
	if !ok {
		return "", time.Time{}, 0, false
	}
	return authority.command, authority.provider.startedAt, authority.provider.pid, true
}

func processDescendsFrom(rootPID, processID int, processes map[int]processInfo) bool {
	if rootPID <= 0 || processID <= 0 {
		return false
	}
	visited := make(map[int]struct{})
	for current := processID; current > 0; {
		if current == rootPID {
			return true
		}
		if _, duplicate := visited[current]; duplicate {
			return false
		}
		visited[current] = struct{}{}
		process, ok := processes[current]
		if !ok || process.ppid == current {
			return false
		}
		current = process.ppid
	}
	return false
}

func detectWorkerProcess(baseCommand string, panePID int, processes map[int]processInfo, fallbackAt time.Time) (string, time.Time, int) {
	command := normalizeCommand(baseCommand)
	baseStartedAt := time.Time{}
	basePID := 0
	if proc, ok := processes[panePID]; ok {
		baseStartedAt = proc.startedAt
		basePID = proc.pid
	}

	if panePID > 0 && len(processes) > 0 {
		scan := descendantProcesses(panePID, processes)
		if proc, ok := processes[panePID]; ok {
			scan = append([]processInfo{proc}, scan...)
		}
		codexResumeSeen := false
		grokResumeSeen := false
		codexResumeCommand := ""
		grokResumeCommand := ""
		bestScore := -1
		var bestCommand string
		var bestProcess processInfo
		for _, proc := range scan {
			if detected := workerCommandFromProcess(proc); detected != "" {
				if isCodexResumeCommandLine(detected) {
					codexResumeSeen = true
					codexResumeCommand = detected
				}
				if isGrokResumeCommandLine(detected) {
					grokResumeSeen = true
					grokResumeCommand = detected
				}
				score := workerProcessScore(proc, detected)
				if bestScore == -1 || score > bestScore {
					bestScore = score
					bestCommand = detected
					bestProcess = proc
				}
			}
		}
		if bestCommand != "" {
			if bestCommand == "codex" && codexResumeSeen {
				if codexResumeCommand != "" {
					bestCommand = codexResumeCommand
				} else {
					bestCommand = "codex resume"
				}
			}
			if bestCommand == "grok" && grokResumeSeen {
				if grokResumeCommand != "" {
					bestCommand = grokResumeCommand
				} else {
					bestCommand = "grok resume"
				}
			}
			return bestCommand, firstNonZeroTime(bestProcess.startedAt, fallbackAt), bestProcess.pid
		}
	}

	if isWorkerCommand(command) {
		return command, fallbackAt, basePID
	}
	return command, baseStartedAt, basePID
}

// Preserve only the explicit transcript identity from the proven provider
// process. Other launch arguments (including settings/credentials) are private.
func claudeProcessCommand(command, args string) string {
	fields := splitLaunchFields(args)
	for i, field := range fields {
		flag, value, hasValue := strings.Cut(field, "=")
		switch flag {
		case "--resume", "-r", "--session-id":
			if !hasValue && i+1 < len(fields) {
				value = fields[i+1]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				continue
			}
			if flag == "-r" {
				flag = "--resume"
			}
			return command + " " + flag + " " + shellquote.Word(value)
		}
	}
	return command
}

func workerCommandFromProcess(proc processInfo) string {
	lowerComm := normalizeCommand(proc.comm)
	lowerArgs := strings.ToLower(proc.args)
	switch lowerComm {
	case "sh", "bash", "dash", "zsh", "fish":
		// A shell command line is launch intent, not proof that the provider
		// process exists. Bind only the actual child (or an exec-replaced pane).
		return ""
	}

	if lowerComm == "claude" || lowerComm == "claude-code" || lowerComm == "cc" {
		return claudeProcessCommand(lowerComm, proc.args)
	}
	// Provider identity comes from the executable, not option values. Cursor
	// can run --model claude-opus-5-5-high without becoming a Claude process.
	argsExecutable := processArgsExecutableBase(lowerArgs)
	if argsExecutable == "claude" || argsExecutable == "claude-code" || argsExecutable == "cc" {
		return claudeProcessCommand("claude", proc.args)
	}
	if lowerComm == "codex" || lowerArgs == "codex" || strings.Contains(lowerArgs, "/bin/codex") || strings.Contains(lowerArgs, " codex ") || strings.HasPrefix(lowerArgs, "codex ") {
		if resume, sessionID := commandResumeArg(lowerArgs); resume {
			if sessionID != "" {
				return "codex resume " + sessionID
			}
			return "codex resume"
		}
		return "codex"
	}
	if lowerComm == "cursor-agent" || strings.Contains(lowerArgs, "/bin/cursor-agent") || strings.Contains(lowerArgs, " cursor-agent ") || strings.HasPrefix(lowerArgs, "cursor-agent ") {
		return "cursor-agent"
	}
	if lowerComm == "grok" || strings.Contains(lowerArgs, "/bin/grok") || strings.Contains(lowerArgs, " grok ") || strings.HasPrefix(lowerArgs, "grok ") {
		if resume, sessionID := commandResumeArg(lowerArgs); resume {
			if sessionID != "" {
				return "grok --resume " + sessionID
			}
			return "grok resume"
		}
		return "grok"
	}
	// Exact basename only for pi: avoid substring false positives (pip, pixel).
	if lowerComm == "pi" || processArgsExecutableBase(lowerArgs) == "pi" {
		return "pi"
	}
	if strings.Contains(lowerArgs, " dsh-session ") {
		return proc.args
	}
	if lowerComm == "dsh" {
		return "dsh"
	}
	if lowerComm == "opencode" || processArgsExecutableBase(lowerArgs) == "opencode" ||
		strings.Contains(lowerArgs, "/bin/opencode") || strings.Contains(lowerArgs, " opencode ") ||
		strings.HasPrefix(lowerArgs, "opencode ") {
		return "opencode"
	}
	return ""
}

// processArgsExecutableBase returns filepath.Base of the first non-shell/env
// field in a process args string. Unlike substring path checks, this rejects
// near-misses such as /bin/pip for provider "pi".
func processArgsExecutableBase(args string) string {
	fields := strings.Fields(strings.TrimSpace(args))
	for _, field := range fields {
		base := normalizeCommand(field)
		switch base {
		case "", "env", "node", "nodejs", "bun", "deno", "python", "python3", "sh", "bash", "dash", "zsh", "fish":
			continue
		default:
			return base
		}
	}
	return ""
}

func workerProcessScore(proc processInfo, detected string) int {
	lowerComm := normalizeCommand(proc.comm)
	detectedName := workerCommandName(detected)
	switch {
	case detectedName == "codex" || detectedName == "grok" || detectedName == "pi" || detectedName == "opencode":
		score := 50
		if lowerComm == "codex" || lowerComm == "grok" || lowerComm == "pi" || lowerComm == "opencode" {
			score = 100
		}
		if resume, _ := commandResumeArg(detected); resume {
			score += 10
		}
		// Live-control Codex launches put two codex processes in one pane: the
		// headless app server and the --remote TUI client. The TUI client is the
		// agent identity (pane foreground); the app server must never win the
		// tie or the watcher loses provider identity for the pane.
		if detectedName == "codex" {
			if isCodexAppServerProcess(proc.args) {
				score -= 20
			}
			if isCodexTUIClientProcess(proc.args) {
				score += 20
			}
		}
		return score
	case detectedName == "cursor-agent":
		if lowerComm == "cursor-agent" {
			return 100
		}
		return 50
	case detectedName == "claude" || detectedName == "claude-code" || detectedName == "cc":
		if lowerComm == detectedName {
			return 100
		}
		return 50
	default:
		return 0
	}
}

// isCodexAppServerProcess reports the headless `codex app-server` half of a
// live-control launch (its argv starts with `codex app-server` or carries
// `--listen`).
func isCodexAppServerProcess(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) >= 2 && normalizeCommand(fields[0]) == "codex" && normalizeCommand(fields[1]) == "app-server" {
		return true
	}
	for _, field := range fields {
		if field == "--listen" || strings.HasPrefix(field, "--listen=") {
			return true
		}
	}
	return false
}

// isCodexTUIClientProcess reports the `--remote` TUI client half of a
// live-control launch.
func isCodexTUIClientProcess(command string) bool {
	for _, field := range strings.Fields(strings.TrimSpace(command)) {
		if field == "--remote" || strings.HasPrefix(field, "--remote=") {
			return true
		}
	}
	return false
}

// refineProcessStartedAt replaces an observed second-granularity process
// start with the platform's sub-second evidence for the same PID when that
// evidence is consistent with the observation. ps lstart truncates to whole
// seconds, so the true start of the same process lies in [observed,
// observed+1s). The guard accepts the wider [observed, observed+2s) as
// fail-safe tolerance for observation scheduling; any value outside it
// (foreign or recycled pid, fake test observation, stale snapshot) keeps the
// observed value. The function never fabricates or widens precision: without
// evidence, or with contradictory evidence, the observed value is returned
// unchanged.
func refineProcessStartedAt(observed time.Time, pid int) time.Time {
	if observed.IsZero() {
		return observed
	}
	precise, ok := workerproc.StartTime(pid)
	if !ok {
		return observed
	}
	if precise.Before(observed) || !precise.Before(observed.Add(2*time.Second)) {
		return observed
	}
	return precise
}

func descendantProcesses(rootPID int, processes map[int]processInfo) []processInfo {
	if rootPID <= 0 || len(processes) == 0 {
		return nil
	}

	children := make(map[int][]processInfo)
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc)
	}

	var result []processInfo
	queue := append([]processInfo(nil), children[rootPID]...)
	for len(queue) > 0 {
		proc := queue[0]
		queue = queue[1:]
		result = append(result, proc)
		queue = append(queue, children[proc.pid]...)
	}

	return result
}
