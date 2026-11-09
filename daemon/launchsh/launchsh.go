// Package launchsh runs the daemon's generated launch scripts where there is
// no POSIX shell: `mewla sh -c SCRIPT` interprets the script in-process
// (mvdan.cc/sh) and starts its programs directly.
//
// The daemon builds every Session's launch as a POSIX shell string (an
// origin marker, environment set-up, then `exec` of the agent, often through
// `$SHELL -i -l -c`). On Unix /bin/sh runs it. Windows has no /bin/sh, so the
// session host runs this instead. A nested `sh`, `bash` or `zsh … -c SCRIPT`
// is interpreted in the same process, without -i or -l: there are no rc
// files to read on Windows. The last program's exit status is the exit
// status.
package launchsh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// Starter starts one program with its environment and directory and waits
// for it; it returns the program's exit code. Tests record instead.
type Starter func(ctx context.Context, argv, env []string, dir string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

// Options configure one run.
type Options struct {
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Start  Starter
}

// Run interprets script and returns its exit status.
func Run(ctx context.Context, script string, opts Options) (int, error) {
	if opts.Start == nil {
		opts.Start = startProgram
	}
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	if opts.Dir == "" {
		opts.Dir, _ = os.Getwd()
	}
	return run(ctx, script, expand.ListEnviron(opts.Env...), opts.Dir, opts)
}

func run(ctx context.Context, script string, env expand.Environ, dir string, opts Options) (int, error) {
	file, err := syntax.NewParser().Parse(strings.NewReader(script), "")
	if err != nil {
		return 2, fmt.Errorf("parse launch script: %w", err)
	}
	runner, err := interp.New(
		interp.StdIO(opts.Stdin, opts.Stdout, opts.Stderr),
		interp.Env(env),
		interp.Dir(dir),
		interp.ExecHandlers(func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
			return func(ctx context.Context, args []string) error {
				hc := interp.HandlerCtx(ctx)
				if script, ok := nestedShell(args); ok {
					code, err := run(ctx, script, hc.Env, hc.Dir, opts)
					if err != nil {
						fmt.Fprintf(hc.Stderr, "mewla sh: %v\n", err)
					}
					return interp.ExitStatus(uint8(code))
				}
				code, err := opts.Start(ctx, args, exported(hc.Env), hc.Dir, hc.Stdin, hc.Stdout, hc.Stderr)
				if err != nil {
					fmt.Fprintf(hc.Stderr, "mewla sh: %s: %v\n", args[0], err)
					return interp.ExitStatus(127)
				}
				return interp.ExitStatus(uint8(code))
			}
		}),
	)
	if err != nil {
		return 2, err
	}
	err = runner.Run(ctx, file)
	var status interp.ExitStatus
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &status):
		return int(status), nil
	default:
		return 1, err
	}
}

// nestedShell recognizes `sh|bash|zsh [-i] [-l] … -c SCRIPT`.
func nestedShell(args []string) (string, bool) {
	name := strings.ToLower(filepath.Base(args[0]))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "sh", "bash", "zsh":
	default:
		return "", false
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-c":
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		case "-i", "-l", "--login", "-il", "-li":
		default:
			return "", false
		}
	}
	return "", false
}

// exported is the environment a started program gets. Each walks every
// layer (a variable unset in a nested script still shows its outer value),
// so each name is read back through Get, which sees the innermost one.
func exported(env expand.Environ) []string {
	seen := map[string]bool{}
	var list []string
	env.Each(func(name string, _ expand.Variable) bool {
		if seen[name] {
			return true
		}
		seen[name] = true
		if vr := env.Get(name); vr.Exported && vr.IsSet() {
			list = append(list, name+"="+vr.String())
		}
		return true
	})
	return list
}

// startProgram runs argv[0] (found on the script's PATH) and waits for it.
func startProgram(ctx context.Context, argv, env []string, dir string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd, err := command(argv, env)
	if err != nil {
		return 127, err
	}
	cmd.Env, cmd.Dir = env, dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Start(); err != nil {
		return 127, err
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
