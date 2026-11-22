package codex

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/codextrust"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// A Codex Worker is a TUI attached to an app-server of its own: the
// app-server runs the thread and its turns, the TUI only shows them. When the
// Worker's program ends and Mewla starts it again (agent relaunch), only what
// died comes back:
//   - the app-server still answers on the Worker's control socket: a TUI is
//     attached to it again (`codex resume <thread> --remote <socket>`) and the
//     app-server and its turn are left alone;
//   - it does not: a new app-server is started and the TUI resumes the same
//     thread through it, the way a fresh Codex Worker is launched.
// Never a bare `codex resume`, which would run the thread outside any
// app-server Mewla can send to, approve through or interrupt.

// controlAnswerTimeout bounds the check that the old app-server still listens.
const controlAnswerTimeout = time.Second

// PrepareRelaunch wraps command, `codex resume <thread> [flags]`, for the
// Worker whose control socket was control. Launch.Reattached says the TUI
// attaches to the app-server that is still running.
func (a Adapter) PrepareRelaunch(command, control string) (agentevents.Launch, error) {
	if a.Inline {
		command = InlineTUI(command, ReadConfig(a.ConfigPath))
	}
	program, thread, rest, err := resumeParts(command)
	if err != nil {
		return agentevents.Launch{}, err
	}
	tui, server := splitResumeArgs(rest)
	control = strings.TrimSpace(control)
	if control != "" && controlAnswers(control) {
		return agentevents.Launch{
			Command: "exec " + resumeTUI(program, thread, control, tui),
			Control: control, Reattached: true,
		}, nil
	}
	if a.ControlDir == "" {
		return agentevents.Launch{}, fmt.Errorf("Codex has no control directory to start its app-server in")
	}
	socket := platform.SocketPath(filepath.Join(a.ControlDir, "codex-ctl-"+uuid.NewString()+".sock"))
	if err := platform.EnsureSocketDir(socket); err != nil {
		return agentevents.Launch{}, err
	}
	launch := program + " app-server --listen unix://" + shellquote.Word(socket) +
		codextrust.Forwarded(rest) + forwardedMCP(rest) + server
	wrapped := launch + " > " + shellquote.Word(ControlLogPath(socket)) + " 2>&1 & echo $! > " + shellquote.Word(ControlPidPath(socket)) + "; "
	if a.Mewla != "" {
		wrapped += shellquote.Word(a.Mewla) + " codex-server-wait " + shellquote.Word(socket) + "; "
	}
	wrapped += "exec " + resumeTUI(program, thread, socket, tui)
	if runtime.GOOS != "windows" {
		wrapped = "set +m; " + wrapped
	}
	return agentevents.Launch{Command: wrapped, Control: socket}, nil
}

func resumeTUI(program, thread, socket, args string) string {
	out := program + " resume " + shellquote.Word(thread) + " --remote unix://" + shellquote.Word(socket)
	if args != "" {
		out += " " + args
	}
	return out
}

// resumeParts splits `codex resume <thread> [flags]`.
func resumeParts(command string) (program, thread, rest string, err error) {
	fields, ok := shellquote.Fields(strings.TrimSpace(command))
	if !ok || len(fields) < 3 || filepath.Base(fields[0]) != "codex" || fields[1] != "resume" || strings.HasPrefix(fields[2], "-") {
		return "", "", "", fmt.Errorf("a Codex relaunch needs `codex resume <thread>`, not %q", command)
	}
	for _, field := range fields[3:] {
		if field == "--remote" || strings.HasPrefix(field, "--remote=") {
			return "", "", "", fmt.Errorf("a Codex relaunch chooses its own app-server")
		}
	}
	words := make([]string, 0, len(fields)-3)
	for _, field := range fields[3:] {
		words = append(words, shellquote.Word(field))
	}
	return fields[0], fields[2], strings.Join(words, " "), nil
}

// splitResumeArgs keeps on the TUI what a remote resume accepts and turns the
// permission flags it refuses ("Permission overrides are not supported when
// resuming a remote task", "--add-dir is not supported with --remote") into
// the new app-server's config. A resumed thread keeps the permissions it ran
// with; the server's config only stands in where it has none.
func splitResumeArgs(rest string) (tui, server string) {
	fields, ok := shellquote.Fields(rest)
	if !ok {
		return rest, ""
	}
	var keep []string
	var config []string
	value := func(index *int, field, flag string) (string, bool) {
		if v, found := strings.CutPrefix(field, flag+"="); found {
			return v, true
		}
		if field == flag && *index+1 < len(fields) {
			*index++
			return fields[*index], true
		}
		return "", false
	}
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		switch {
		case field == "--dangerously-bypass-approvals-and-sandbox" || field == "--yolo":
			config = append(config, `approval_policy="never"`, `sandbox_mode="danger-full-access"`)
			continue
		case field == "--full-auto":
			config = append(config, `approval_policy="on-request"`, `sandbox_mode="workspace-write"`)
			continue
		}
		matched := false
		for _, flag := range []string{"--ask-for-approval", "-a"} {
			if v, ok := value(&index, field, flag); ok {
				config, matched = append(config, `approval_policy="`+v+`"`), true
				break
			}
		}
		for _, flag := range []string{"--sandbox", "-s"} {
			if matched {
				break
			}
			if v, ok := value(&index, field, flag); ok {
				config, matched = append(config, `sandbox_mode="`+v+`"`), true
			}
		}
		if !matched {
			if v, ok := value(&index, field, "--add-dir"); ok {
				config, matched = append(config, `sandbox_workspace_write.writable_roots=["`+v+`"]`), true
			}
		}
		if !matched {
			keep = append(keep, shellquote.Word(field))
		}
	}
	for _, entry := range config {
		server += " -c " + shellquote.Word(entry)
	}
	return strings.Join(keep, " "), server
}

// controlAnswers says an app-server still listens on socket.
func controlAnswers(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, controlAnswerTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
