// Package tmuxguard keeps a plain `tmux` typed inside a Mewla-spawned pane from
// reaching the user's live default server.
//
// tmux 3.6a ignores a TMUX_TMPDIR whose directory does not exist and falls back
// to /tmp/tmux-<uid>/default, so `TMUX_TMPDIR=<deleted scratch> tmux kill-server`
// silently kills the live server. The daemon puts a small `tmux` shim first on
// PATH in every Worker and Brain pane; the shim runs Decide and then execs the
// real tmux, or refuses with exit 1. It is accident prevention for agents that
// write `tmux ...` in cleanups, not a sandbox: a caller that execs the real
// binary by absolute path is not stopped. The daemon never goes through it
// (see RealTmux).
package tmuxguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// ScratchEnv names the directory tree that is the pane's own tmux scratch.
	// A socket may be killed, and an implicit socket directory may be used,
	// only below it.
	ScratchEnv = "MEWLA_TMUX_SCRATCH"
	// DirEnv carries the shim directory the launch shell puts first on PATH.
	DirEnv = "MEWLA_TMUX_GUARD_DIR"

	killServer = "kill-server"
)

// Env is everything Decide reads from the caller's world, injectable so the
// decision table needs no tmux and no filesystem.
type Env struct {
	TmuxTmpdir string // $TMUX_TMPDIR
	Tmux       string // $TMUX ("socket,pid,session"), set only inside a tmux client
	Scratch    string // $MEWLA_TMUX_SCRATCH
	Cwd        string
	UID        int
	// IsDir reports whether path exists and is a directory.
	IsDir func(path string) bool
	// Resolve returns path with symlinks resolved: the path itself when it
	// exists, otherwise its deepest existing ancestor plus the remainder.
	Resolve func(path string) string
}

// Verdict is the guard's decision for one tmux invocation.
type Verdict struct {
	Allow  bool
	Reason string
}

func allow() Verdict { return Verdict{Allow: true} }

func refuse(format string, args ...any) Verdict {
	return Verdict{Reason: fmt.Sprintf(format, args...)}
}

// SystemEnv reads the real process environment and filesystem.
func SystemEnv() Env {
	cwd, _ := os.Getwd()
	return Env{
		TmuxTmpdir: os.Getenv("TMUX_TMPDIR"),
		Tmux:       os.Getenv("TMUX"),
		Scratch:    os.Getenv(ScratchEnv),
		Cwd:        cwd,
		UID:        os.Getuid(),
		IsDir: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.IsDir()
		},
		Resolve: resolveExisting,
	}
}

// resolveExisting resolves symlinks of the longest existing prefix of path and
// re-appends the rest, so a socket that does not exist yet still lands where
// the kernel would put it.
func resolveExisting(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for p := path; ; p = filepath.Dir(p) {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		if parent := filepath.Dir(p); parent == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
	}
}

type invocation struct {
	socket     string // -S value
	hasSocket  bool
	label      string // -L value
	hasLabel   bool
	version    bool // -V
	commands   [][]string
	parseError string
}

// tmux's getopt string is "2c:CDdf:lL:NqS:T:uUvV".
func parse(args []string) invocation {
	var inv invocation
	i := 0
parsing:
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		for j := 1; j < len(arg); j++ {
			switch c := arg[j]; c {
			case '2', 'C', 'D', 'd', 'l', 'N', 'q', 'u', 'U', 'v':
			case 'V':
				inv.version = true
			case 'c', 'f', 'L', 'S', 'T':
				value := arg[j+1:]
				if value == "" {
					i++
					if i >= len(args) {
						inv.parseError = "option -" + string(c) + " needs a value"
						return inv
					}
					value = args[i]
				}
				switch c {
				case 'S':
					inv.socket, inv.hasSocket = value, true
				case 'L':
					inv.label, inv.hasLabel = value, true
				}
				i++
				continue parsing
			default:
				inv.parseError = "unknown tmux option -" + string(c)
				return inv
			}
		}
		i++
	}
	inv.commands = splitCommands(args[i:])
	return inv
}

// splitCommands follows tmux's own rule: a lone ";" or an argument ending in an
// unescaped ";" ends a command. "\;" is a literal ";" argument.
func splitCommands(args []string) [][]string {
	var commands [][]string
	var current []string
	flush := func() {
		if len(current) > 0 {
			commands = append(commands, current)
		}
		current = nil
	}
	for _, arg := range args {
		switch {
		case arg == ";":
			flush()
		case strings.HasSuffix(arg, `\;`):
			current = append(current, strings.TrimSuffix(arg, `\;`)+";")
		case strings.HasSuffix(arg, ";"):
			if rest := strings.TrimSuffix(arg, ";"); rest != "" {
				current = append(current, rest)
			}
			flush()
		default:
			current = append(current, arg)
		}
	}
	flush()
	return commands
}

// killsServer reports whether a command name resolves to kill-server. tmux
// accepts any unique prefix of a command name, and "kill-ser" is the shortest
// one not shared with kill-session.
func killsServer(name string) bool {
	const shortest = len("kill-ser")
	return len(name) >= shortest && strings.HasPrefix(killServer, name)
}

// Decide is the whole policy:
//
//   - tmux -V never touches a server.
//   - Without -S, the socket tmux would pick ($TMUX, else $TMUX_TMPDIR/tmux-<uid>)
//     must be under the pane's own scratch. An unset or missing TMUX_TMPDIR
//     makes tmux fall back to /tmp, the live server, so it is refused.
//   - kill-server (in any command of a ; sequence) needs an explicit -S below
//     the pane's own scratch.
//
// Every other explicit -S is passed through untouched.
func Decide(args []string, env Env) Verdict {
	inv := parse(args)
	if inv.parseError != "" {
		return refuse("%s", inv.parseError)
	}
	if inv.version {
		return allow()
	}

	kills := false
	for _, command := range inv.commands {
		if killsServer(command[0]) {
			kills = true
		}
	}

	scratch := ""
	if strings.TrimSpace(env.Scratch) != "" && env.IsDir(env.Scratch) {
		scratch = env.Resolve(env.Scratch)
	}
	under := func(path string) bool {
		if scratch == "" {
			return false
		}
		rel, err := filepath.Rel(scratch, env.Resolve(path))
		return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	absolute := func(path string) string {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Join(env.Cwd, path)
	}

	if inv.hasSocket {
		if strings.TrimSpace(inv.socket) == "" {
			return refuse("-S is empty (an unset variable?); name the socket explicitly")
		}
		if kills && !under(absolute(inv.socket)) {
			return refuse("kill-server is only allowed on a socket under your own scratch (%s); -S %s is not", scratchLabel(env), inv.socket)
		}
		return allow()
	}

	if kills {
		return refuse("kill-server needs an explicit -S <socket under %s>; without -S it can reach the live server", scratchLabel(env))
	}

	// No -S: find the socket tmux would use.
	var socket string
	switch {
	case !inv.hasLabel && strings.TrimSpace(env.Tmux) != "":
		socket, _, _ = strings.Cut(env.Tmux, ",")
	default:
		dir := strings.TrimSpace(env.TmuxTmpdir)
		if dir == "" {
			return refuse("TMUX_TMPDIR is unset, so tmux falls back to /tmp/tmux-%d/default, the live server; use tmux -S <socket under %s>", env.UID, scratchLabel(env))
		}
		if !env.IsDir(dir) {
			return refuse("TMUX_TMPDIR=%s does not exist, so tmux falls back to /tmp/tmux-%d/default, the live server; use tmux -S <socket under %s>", dir, env.UID, scratchLabel(env))
		}
		label := "default"
		if inv.hasLabel {
			label = inv.label
		}
		socket = filepath.Join(dir, "tmux-"+strconv.Itoa(env.UID), label)
	}
	if !under(absolute(socket)) {
		return refuse("the socket tmux would use (%s) is outside your own scratch (%s); use tmux -S <socket under it>", socket, scratchLabel(env))
	}
	return allow()
}

func scratchLabel(env Env) string {
	if strings.TrimSpace(env.Scratch) == "" {
		return "$" + ScratchEnv + " (unset)"
	}
	return env.Scratch
}
