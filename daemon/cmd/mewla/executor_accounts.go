package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/hostdeps"
	"github.com/daoleno/mewla/daemon/setup"
	"github.com/daoleno/mewla/daemon/work"
)

const executorAccountsUsage = `Usage: mewla brain executors add <name> -like claude|codex [-config-dir DIR]
       mewla brain executors login <name>

Use a second Claude Code or Codex sign-in as its own executor, next to the
default one. Brain routes Workers to it like any executor.

  add     Write the executor into executors.toml with its own config directory
          (CLAUDE_CONFIG_DIR for Claude Code, CODEX_HOME for Codex).
  login   Sign that executor's CLI in to its account, in this terminal.

Restart mewla after add so the new executor appears. Run mewla doctor to see
which account each executor is signed in as.
`

var executorNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// runBrainExecutorAccounts handles "add" and "login"; it reports handled=false
// for anything else so the listing keeps working.
func runBrainExecutorAccounts(args []string, stderr io.Writer) (handled bool, err error) {
	if len(args) == 0 || (args[0] != "add" && args[0] != "login") {
		return false, nil
	}
	action, rest := args[0], args[1:]
	name := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		name, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("mewla brain executors "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, executorAccountsUsage) }
	var stateDir, like, configDir, command string
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	if action == "add" {
		fs.StringVar(&like, "like", "", "claude or codex")
		fs.StringVar(&configDir, "config-dir", "", "config directory for this sign-in (default: <state dir>/accounts/<name>)")
		fs.StringVar(&command, "command", "", "launch command (default: the program itself)")
	}
	if err := fs.Parse(rest); err != nil {
		return true, err
	}
	if fs.NArg() != 0 || name == "" {
		fs.Usage()
		return true, errors.New("give the executor's name")
	}
	if action == "add" {
		return true, addExecutorAccount(name, like, configDir, command, resolveCLIStateDir(stateDir), os.Stdout)
	}
	return true, loginExecutorAccount(name, os.Stdout, stderr)
}

func addExecutorAccount(name, like, configDir, command, stateDir string, out io.Writer) error {
	if !executorNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not a usable name: use lowercase letters, digits, - and _", name)
	}
	cfg, err := work.LoadExecutors(mustExecutorsPath())
	if err != nil {
		return err
	}
	if existing, ok := cfg.ByName[name]; ok && existing.ConfigDir == "" {
		return fmt.Errorf("%q is already an executor with the default sign-in; pick another name", name)
	}
	provider := work.InferWorkerProvider(like)
	if provider != work.WorkerProviderClaude && provider != work.WorkerProviderCodex {
		return errors.New("-like must be claude or codex: only those two keep their sign-in in a config directory")
	}
	if strings.TrimSpace(command) == "" {
		command = like
	}
	if work.InferWorkerProvider(command) != provider {
		return fmt.Errorf("-command %q does not start %s", command, like)
	}
	if strings.TrimSpace(configDir) == "" {
		configDir = filepath.Join(stateDir, "accounts", name)
	}
	absolute := work.ExpandConfigDir(configDir)
	if absolute == "" {
		return fmt.Errorf("-config-dir %q must be an absolute path (or start with ~/)", configDir)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return err
	}
	backup, err := setup.AddExecutor(mustExecutorsPath(), work.Executor{Name: name, Command: command, ConfigDir: absolute}, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added executor %q with its own sign-in in %s.\n", name, absolute)
	if backup != "" {
		fmt.Fprintf(out, "The previous executors.toml is at %s.\n", backup)
	}
	fmt.Fprintf(out, "Next: mewla brain executors login %s\nThen restart mewla so Brain can use it. The default %s sign-in is unchanged.\n", name, like)
	return nil
}

func mustExecutorsPath() string {
	path, err := work.DefaultExecutorsPath()
	if err != nil {
		return filepath.Join(".mewla", "executors.toml")
	}
	return path
}

func loginExecutorAccount(name string, out, stderr io.Writer) error {
	cfg, err := work.LoadExecutors(mustExecutorsPath())
	if err != nil {
		return err
	}
	worker, ok := cfg.WorkerExecutor(name)
	if !ok {
		return fmt.Errorf("no executor named %q; see mewla doctor", name)
	}
	variable := work.ConfigDirEnv(worker.Provider)
	if worker.ConfigDir == "" || variable == "" {
		return fmt.Errorf("%q has no config_dir of its own; its CLI signs in the usual way (claude auth login, codex login)", name)
	}
	fields := strings.Fields(worker.BareCommand)
	if len(fields) == 0 {
		return fmt.Errorf("%q has no command", name)
	}
	program, err := hostdeps.LookPath(fields[0])
	if err != nil {
		return fmt.Errorf("%s is not on PATH: %w", fields[0], err)
	}
	args := []string{"auth", "login"}
	if worker.Provider == work.WorkerProviderCodex {
		args = []string{"login"}
	}
	if err := os.MkdirAll(worker.ConfigDir, 0o700); err != nil {
		return err
	}
	fmt.Fprintf(out, "Signing %s in to its own account (%s=%s).\n", name, variable, worker.ConfigDir)
	command := exec.Command(program, args...)
	command.Env = append(os.Environ(), variable+"="+worker.ConfigDir)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, stderr
	return command.Run()
}
