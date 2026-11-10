package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/ghosttyvt"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/watcher"
	"github.com/daoleno/mewla/daemon/work"
	"github.com/daoleno/mewla/daemon/workerproc"
)

// tmuxMoveInterval is how often the one-time move from tmux looks again.
const tmuxMoveInterval = 5 * time.Second

// tmuxMoveIdleChecks is how many looks in a row a Session must be idle before
// it moves, so a turn that is just starting is never cut.
const tmuxMoveIdleChecks = 2

// moveSessionsFromTmux moves the Sessions an earlier Mewla started in tmux to
// the session host, each when it is idle, resumed into the same conversation
// under the same Session id. A Session whose agent cannot resume stays in
// tmux, no longer Mewla's. Until a Session moves it stays visible and takes
// input in tmux. Nothing is touched unless the session host runs and can
// start Sessions. conversation names the provider conversation Mewla
// recorded for a Session, for an agent whose live process no longer proves it.
func moveSessionsFromTmux(ctx context.Context, w *watcher.Watcher, conversation func(sessionID string) (string, bool)) {
	if !ghosttyvt.Available() {
		if len(w.TmuxSessions()) > 0 {
			log.Printf("tmux move: this build has no session host engine; Sessions in tmux stay where they are")
		}
		return
	}
	ready := false
	idle := map[string]int{}
	ticker := time.NewTicker(tmuxMoveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		sessions := w.TmuxSessions()
		if len(sessions) == 0 {
			continue
		}
		if !ready {
			if err := w.SessionHostReady(); err != nil {
				log.Printf("tmux move: waiting for the session host: %v", err)
				continue
			}
			ready = true
		}
		seen := map[string]bool{}
		for _, worker := range sessions {
			seen[worker.ID] = true
			plan := planTmuxMove(worker, workerproc.Args(worker.ProcessID), conversation)
			if plan.leave != "" && plan.final {
				log.Printf("tmux move: %s (%s) stays in tmux: %s", worker.ID, worker.Name, plan.leave)
				w.LeaveInTmux(worker.ID, plan.leave)
				continue
			}
			if !tmuxSessionIdle(worker) {
				idle[worker.ID] = 0
				continue
			}
			idle[worker.ID]++
			if idle[worker.ID] < tmuxMoveIdleChecks {
				continue
			}
			if plan.leave != "" {
				// What could still change mid-turn (a conversation id not yet
				// known) is settled once the agent is idle.
				log.Printf("tmux move: %s (%s) stays in tmux: %s", worker.ID, worker.Name, plan.leave)
				w.LeaveInTmux(worker.ID, plan.leave)
				continue
			}
			move, err := plan.prepare(worker)
			if err != nil {
				log.Printf("tmux move: %s (%s) stays in tmux: %v", worker.ID, worker.Name, err)
				w.LeaveInTmux(worker.ID, err.Error())
				continue
			}
			if err := w.MoveToHost(worker.ID, move); err != nil {
				log.Printf("tmux move: %s (%s): %v", worker.ID, worker.Name, err)
				continue
			}
			log.Printf("tmux move: %s (%s) now runs in the session host: %s", worker.ID, worker.Name, move.Command)
			delete(idle, worker.ID)
		}
		for id := range idle {
			if !seen[id] {
				delete(idle, id)
			}
		}
	}
}

// tmuxSessionIdle is a Session whose agent finished its turn and asks
// nothing.
func tmuxSessionIdle(worker classifier.Worker) bool {
	return worker.State == classifier.StateDone && worker.Prompt == nil && worker.ProcessID > 0
}

type tmuxMovePlan struct {
	provider string
	// leave says why the Session cannot resume; empty when it can.
	leave string
	// final says leave cannot change while the agent runs on: its provider
	// cannot resume at all, or its command names no session.
	final bool
	// resume is the agent's command resumed into its conversation.
	resume string
	// codexControl is the socket of the app-server a Codex TUI talked to.
	codexControl string
}

// planTmuxMove names how the agent in a tmux Session resumes in a new
// process, from its conversation id: the one its launch declared, or the one
// its live process proves. argv is the running agent's whole command line,
// with every flag its launch gave it; the Worker's command keeps only what
// names the conversation.
func planTmuxMove(worker classifier.Worker, argv []string, conversation func(string) (string, bool)) tmuxMovePlan {
	provider := work.InferWorkerProvider(worker.Command, worker.Name)
	plan := tmuxMovePlan{provider: provider}
	if strings.TrimSpace(worker.Command) == "" {
		plan.leave, plan.final = "no program is known", true
		return plan
	}
	switch provider {
	case work.WorkerProviderClaude, work.WorkerProviderCodex, work.WorkerProviderPi, work.WorkerProviderOpenCode, work.WorkerProviderDSH:
	default:
		if provider == "" {
			provider = work.WorkerProviderCustom
		}
		plan.leave, plan.final = fmt.Sprintf("Mewla cannot resume %s", provider), true
		return plan
	}
	if len(argv) == 0 {
		plan.leave = "its command line cannot be read"
		return plan
	}
	if provider == work.WorkerProviderPi {
		// Pi renames its process, so its command line no longer names the
		// session file; the Worker's command does.
		argv = strings.Fields(worker.Command)
	}
	command := resumableCommandLine(provider, argv)
	declared, present, err := work.ProviderResumeToken(provider, command)
	if err != nil {
		plan.leave = fmt.Sprintf("its %s command cannot be read: %v", provider, err)
		return plan
	}
	if !present && provider != work.WorkerProviderPi {
		// The Worker's command may name the conversation where the command
		// line does not (a Codex launched without an id, then bound).
		if named, ok, err := work.ProviderResumeToken(provider, worker.Command); err == nil && ok {
			declared, present = named, true
		}
	}
	switch provider {
	case work.WorkerProviderClaude, work.WorkerProviderCodex:
		identity, proven, err := work.ResolveLiveHostTranscriptIdentity(worker, provider)
		token, transcript := "", ""
		if proven && err == nil {
			token, transcript = strings.TrimSpace(identity.SessionID), strings.TrimSpace(identity.Path)
		}
		switch {
		case token != "" && present && declared != token:
			plan.leave = fmt.Sprintf("its %s conversation is %s but its command names %s", provider, token, declared)
			return plan
		case token == "" && present:
			token = declared
		case token == "":
			if conversation != nil {
				if recorded, ok := conversation(worker.ID); ok {
					token = strings.TrimSpace(recorded)
				}
			}
			if token == "" {
				plan.leave = fmt.Sprintf("its %s conversation id is unknown", provider)
				return plan
			}
		}
		if provider == work.WorkerProviderCodex {
			plan.codexControl = codexControlSocket(argv)
			// A resumed Codex that finds a newer release stops on an update
			// prompt and waits for a key. Only a Codex launched with config
			// overrides is told not to look: one launched without runs
			// against Codex's own app-server daemon, which keeps the
			// conversation for a minute after the old Codex ends; given an
			// override, the resumed one runs its own server and finds the
			// conversation open in another app.
			if !strings.Contains(command, "check_for_update_on_startup") && codexConfigOverridden(argv) {
				command += " -c check_for_update_on_startup=false"
			}
			plan.resume, err = work.CodexNativeResumeCommand(command, token)
		} else {
			if transcript == "" {
				transcript = work.ClaudeTranscriptPath(worker.ProcessID, worker.Cwd, token)
			}
			if transcript == "" {
				plan.leave = "its claude conversation cannot be found"
				return plan
			}
			if _, statErr := os.Stat(transcript); statErr != nil {
				// Claude writes a conversation at its first message; one that
				// never had any cannot be resumed, and starting the same
				// command again loses nothing.
				plan.resume = command
				return plan
			}
			plan.resume, err = work.WithProviderResumeToken(provider, command, token)
		}
		if err != nil {
			plan.leave = fmt.Sprintf("its %s command cannot resume: %v", provider, err)
			return plan
		}
	case work.WorkerProviderPi, work.WorkerProviderOpenCode, work.WorkerProviderDSH:
		// These name their session in the launch; the same command reopens it.
		if !present {
			plan.leave, plan.final = fmt.Sprintf("its %s session is not named in its command", provider), true
			return plan
		}
		plan.resume = command
	default:
		plan.leave = fmt.Sprintf("Mewla cannot resume %s", provider)
		return plan
	}
	return plan
}

// resumableCommandLine joins argv into a shell command line. A Codex TUI that
// talked to an app-server beside it in the pane loses that server with the
// pane, so it resumes on its own.
func resumableCommandLine(provider string, argv []string) string {
	words := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if provider == work.WorkerProviderCodex {
			if arg == "--remote" && i+1 < len(argv) {
				i++
				continue
			}
			if strings.HasPrefix(arg, "--remote=") {
				continue
			}
		}
		words = append(words, shellquote.Word(arg))
	}
	return strings.Join(words, " ")
}

// codexControlSocket is the unix socket of the app-server a Codex TUI's
// --remote names, empty when it names none.
func codexControlSocket(argv []string) string {
	for i, arg := range argv {
		value, ok := strings.CutPrefix(arg, "--remote=")
		if !ok && arg == "--remote" && i+1 < len(argv) {
			value, ok = argv[i+1], true
		}
		if ok {
			if path, unix := strings.CutPrefix(value, "unix://"); unix {
				return path
			}
			return ""
		}
	}
	return ""
}

// codexConfigOverridden reports a Codex command line that sets config with
// -c or --config.
func codexConfigOverridden(argv []string) bool {
	for _, arg := range argv {
		if arg == "-c" || arg == "--config" || strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "--config=") {
			return true
		}
	}
	return false
}

// prepare reads what the resumed program needs from the running one: its
// environment and directory. It fails, leaving the Session in tmux, when the
// program could not start again there.
func (plan tmuxMovePlan) prepare(worker classifier.Worker) (watcher.TmuxMove, error) {
	cwd := strings.TrimSpace(worker.Cwd)
	if info, err := os.Stat(cwd); cwd == "" || err != nil || !info.IsDir() {
		return watcher.TmuxMove{}, fmt.Errorf("its directory %q is gone", cwd)
	}
	env := tmuxMoveEnvironment(workerproc.Environ(worker.ProcessID), watcher.ExecutablePath())
	if len(env) == 0 {
		return watcher.TmuxMove{}, fmt.Errorf("the environment of process %d cannot be read", worker.ProcessID)
	}
	if err := commandOnPath(plan.resume, env, cwd); err != nil {
		return watcher.TmuxMove{}, err
	}
	return watcher.TmuxMove{Command: "exec " + plan.resume, Env: env, Cwd: cwd, ProcessID: worker.ProcessID, CodexControl: plan.codexControl}, nil
}

// tmuxMoveEnvironment is the running program's environment without what
// named its tmux pane; the host sets the Session's own id and terminal. The
// agent reports to the Mewla that now runs, mewla, not the one that started
// it.
func tmuxMoveEnvironment(environ []string, mewla string) []string {
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		switch key {
		case "TMUX", "TMUX_PANE", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "COLORTERM",
			"MEWLA_SESSION_ID", "MEWLA_TMUX_GUARD_DIR", "_", "SHLVL", "OLDPWD", "PWD",
			"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY":
			continue
		case "MEWLA_WORKER_PROGRESS_CMD":
			entry = key + "=" + mewla
		}
		out = append(out, entry)
	}
	return out
}

// commandOnPath checks that the resumed command's program can be found with
// the program's own PATH.
func commandOnPath(command string, env []string, cwd string) error {
	program := resumedProgram(command)
	if program == "" {
		return fmt.Errorf("the resumed command %q names no program", command)
	}
	if strings.Contains(program, "/") {
		if !filepath.IsAbs(program) {
			program = filepath.Join(cwd, program)
		}
		if info, err := os.Stat(program); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return fmt.Errorf("%s cannot be run", program)
		}
		return nil
	}
	path := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	for _, dir := range filepath.SplitList(path) {
		candidate := filepath.Join(dir, program)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return nil
		}
	}
	if _, err := exec.LookPath(program); err == nil && path == "" {
		return nil
	}
	return fmt.Errorf("%s is not on its PATH", program)
}

// resumedProgram is the program a command line runs, past an env prefix.
func resumedProgram(command string) string {
	fields := strings.Fields(command)
	i := 0
	if i < len(fields) && fields[i] == "env" {
		i++
		for i < len(fields) && (strings.Contains(fields[i], "=") || strings.HasPrefix(fields[i], "-")) {
			i++
		}
	}
	for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "/") {
		i++
	}
	if i >= len(fields) {
		return ""
	}
	return strings.Trim(fields[i], `'"`)
}
