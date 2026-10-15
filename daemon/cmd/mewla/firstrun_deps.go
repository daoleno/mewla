package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

// depsPrompt checks the two things Brain cannot work without, tmux and one
// signed-in agent CLI, and offers the official command for what is missing.
// It never blocks a start: a "no", a failed install or a closed input prints
// the command and moves on.
type depsPrompt struct {
	in          *bufio.Reader
	w           io.Writer
	interactive bool
	platform    hostdeps.Platform
	// run executes one shown command attached to the terminal.
	run func(command string) error
	// tmux and agents re-check the machine; tests replace them.
	tmux   func() doctor.TmuxCheck
	agents func() doctor.ExecutorsCheck
}

func newDepsPrompt(in *bufio.Reader, w io.Writer, interactive bool) *depsPrompt {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	return &depsPrompt{
		in:          in,
		w:           w,
		interactive: interactive,
		platform:    hostdeps.Detect(),
		run:         runShownCommand,
		tmux:        func() doctor.TmuxCheck { return doctor.CheckTmux(options) },
		agents:      func() doctor.ExecutorsCheck { return doctor.CheckExecutors(options) },
	}
}

// runShownCommand runs exactly the command that was printed, with the
// terminal attached so sudo and installers can ask their own questions.
// Ctrl-C stops the command, not Mewla: the signal is caught (not ignored,
// which the child would inherit) while it runs.
func runShownCommand(command string) error {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ask reads a Y/n answer; Enter means yes, closed input means no.
func (d *depsPrompt) ask(question string) bool {
	if !d.interactive {
		return false
	}
	for {
		fmt.Fprintf(d.w, "%s [Y/n]: ", question)
		line, err := d.in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(d.w)
			return false
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return true
		case "n", "no":
			return false
		}
		fmt.Fprintln(d.w, "Type y or n.")
	}
}

func (d *depsPrompt) runPlan(plan hostdeps.Plan) bool {
	for _, step := range plan.Steps {
		fmt.Fprintf(d.w, "\n$ %s\n", step)
		if err := d.run(step); err != nil {
			fmt.Fprintf(d.w, "\nThat command failed (%v).\n", err)
			return false
		}
	}
	fmt.Fprintln(d.w)
	return true
}

// ensureTmux reports whether tmux works, after offering to install it.
func (d *depsPrompt) ensureTmux() bool {
	check := d.tmux()
	if check.Status == doctor.StatusOK {
		addToolDirToPath(check.Path)
		return true
	}
	if check.Found {
		fmt.Fprintf(d.w, "tmux at %s does not work (%s). Fix it, then start mewla again.\n", check.Path, check.Summary)
		return false
	}
	plan := hostdeps.TmuxPlan(d.platform)
	fmt.Fprintln(d.w, "tmux is missing. Mewla runs every agent inside tmux.")
	if len(plan.Steps) == 0 {
		fmt.Fprintf(d.w, "Install tmux for %s: %s\n", d.platform.Name(), plan.Doc)
		return false
	}
	fmt.Fprintf(d.w, "Install it on %s with:\n  %s\n", d.platform.Name(), plan.Command())
	if plan.Blocked != "" {
		fmt.Fprintf(d.w, "(%s)\n", plan.Blocked)
		return false
	}
	if !d.ask("Install tmux now?") {
		return false
	}
	d.runPlan(plan)
	check = d.tmux()
	if check.Status != doctor.StatusOK {
		fmt.Fprintln(d.w, "tmux is still missing. Run the command above, then start mewla again.")
		return false
	}
	addToolDirToPath(check.Path)
	fmt.Fprintf(d.w, "%s is ready.\n", strings.TrimSpace("tmux "+check.Version))
	return true
}

// ensureAgent reports whether one agent CLI is usable, after offering to
// install one and to sign in.
func (d *depsPrompt) ensureAgent() bool {
	check := d.agents()
	if ready := readyAgent(check); ready != "" {
		return true
	}
	agents := hostdeps.Agents(d.platform)
	if len(agents) == 0 {
		fmt.Fprintln(d.w, "Brain needs an agent CLI such as Claude Code or Codex. Install and sign in to one.")
		return false
	}
	if agent, ok := unauthenticatedAgent(check, agents); ok {
		return d.signIn(agent)
	}

	fmt.Fprintln(d.w, "Brain needs an agent CLI, and none is ready yet.")
	if !d.interactive {
		for _, agent := range agents {
			fmt.Fprintf(d.w, "  %-12s %s   then: %s\n", agent.Name+":", agent.Install.Command(), agent.Login)
		}
		return false
	}
	fmt.Fprintln(d.w, "Install one:")
	fmt.Fprintln(d.w)
	for index, agent := range agents {
		fmt.Fprintf(d.w, "  %d. %-12s %s\n", index+1, agent.Name, agent.Install.Command())
	}
	skip := len(agents) + 1
	fmt.Fprintf(d.w, "  %d. Skip for now\n", skip)
	fmt.Fprintln(d.w)
	choice := 0
	for choice == 0 {
		fmt.Fprintf(d.w, "Choose 1-%d [1]: ", skip)
		line, err := d.in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(d.w)
			choice = skip
			break
		}
		answer := strings.TrimSpace(line)
		if answer == "" {
			answer = "1"
		}
		for number := 1; number <= skip; number++ {
			if answer == fmt.Sprint(number) {
				choice = number
			}
		}
		if choice == 0 {
			fmt.Fprintf(d.w, "Type a number from 1 to %d.\n", skip)
		}
	}
	if choice == skip {
		fmt.Fprintln(d.w, "Brain starts working once one is installed and signed in.")
		return false
	}
	agent := agents[choice-1]
	if !d.runPlan(agent.Install) {
		fmt.Fprintf(d.w, "%s is not installed. Try again later: %s\n", agent.Name, agent.Install.Command())
		return false
	}
	check = d.agents()
	item, found := executorItem(check, agent.ID)
	if !found || !item.BinaryFound {
		fmt.Fprintf(d.w, "%s is not installed. Try again later: %s\n", agent.Name, agent.Install.Command())
		return false
	}
	addToolDirToPath(item.BinaryPath)
	if item.Usable && item.Auth == doctor.AuthAuthenticated {
		fmt.Fprintf(d.w, "%s is ready. Brain will use it.\n", agent.Name)
		return true
	}
	return d.signIn(agent)
}

func (d *depsPrompt) signIn(agent hostdeps.Agent) bool {
	fmt.Fprintf(d.w, "%s is installed. Sign in to it:\n  %s\n", agent.Name, agent.Login)
	if !d.ask("Sign in now?") {
		return false
	}
	fmt.Fprintf(d.w, "\n$ %s\n", agent.Login)
	if err := d.run(agent.Login); err != nil {
		fmt.Fprintf(d.w, "\nSign-in did not finish (%v). Later: %s\n", err, agent.Login)
		return false
	}
	item, _ := executorItem(d.agents(), agent.ID)
	if item.Auth == doctor.AuthUnauthenticated || !item.Usable {
		fmt.Fprintf(d.w, "%s is not signed in yet. Later: %s\n", agent.Name, agent.Login)
		return false
	}
	addToolDirToPath(item.BinaryPath)
	fmt.Fprintf(d.w, "\n%s is ready. Brain will use it.\n", agent.Name)
	return true
}

// readyAgent names a usable executor and puts its directory on PATH.
func readyAgent(check doctor.ExecutorsCheck) string {
	for _, item := range check.Items {
		if item.Usable {
			addToolDirToPath(item.BinaryPath)
		}
	}
	if check.UsableCount > 0 {
		return check.RecommendedHost
	}
	return ""
}

func unauthenticatedAgent(check doctor.ExecutorsCheck, agents []hostdeps.Agent) (hostdeps.Agent, bool) {
	for _, agent := range agents {
		if item, ok := executorItem(check, agent.ID); ok && item.BinaryFound && item.Auth == doctor.AuthUnauthenticated {
			addToolDirToPath(item.BinaryPath)
			return agent, true
		}
	}
	return hostdeps.Agent{}, false
}

func executorItem(check doctor.ExecutorsCheck, id string) (doctor.ExecutorCheck, bool) {
	for _, item := range check.Items {
		if item.ID == id {
			return item, true
		}
	}
	return doctor.ExecutorCheck{}, false
}

func addToolDirToPath(binary string) {
	if strings.Contains(binary, string(os.PathSeparator)) {
		hostdeps.AddToPath(filepath.Dir(binary))
	}
}

// prepareToolPath puts this executable's directory and the installers'
// usual directories on PATH, so tools installed there are found by the
// daemon, its tmux server and a boot unit that copies PATH.
func prepareToolPath() {
	if executable, err := os.Executable(); err == nil && !strings.Contains(executable, ".app/Contents/") {
		addToolDirToPath(executable)
	}
	home, _ := os.UserHomeDir()
	for _, dir := range hostdeps.ExtraPathDirs(home) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			hostdeps.AddToPath(dir)
		}
	}
}

// quickDepsCheck runs on every start that skips the first-run conversation:
// tmux plus whether any agent CLI is installed (no sign-in probes). It offers
// installs in a terminal and prints the commands otherwise.
func quickDepsCheck(stderr io.Writer) {
	interactive := stdioIsTerminal() && !daemonSupervised() && os.Getenv("CI") == ""
	d := newDepsPrompt(bufio.NewReader(os.Stdin), stderr, interactive)
	if d.tmux().Status != doctor.StatusOK {
		d.ensureTmux()
	}
	installed := doctor.CheckExecutors(doctor.Options{
		LookPath: hostdeps.LookPath,
		RunCommand: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("not probed at start")
		},
	})
	if readyAgent(installed) == "" {
		d.ensureAgent()
	}
}

// offerBoot asks to keep Mewla running in the background with the existing
// `mewla boot install`, right after the connect choice.
func offerBoot(d *depsPrompt, stateDir string, choice startupChoice) {
	switch bootGOOS {
	case "linux":
		if _, err := (execBootRunner{}).run("systemctl", "--user", "show-environment"); err != nil {
			fmt.Fprintln(d.w, "\nThis system has no systemd user services, so Mewla runs while this terminal is open.")
			return
		}
	case "darwin":
	default:
		return
	}
	fmt.Fprintln(d.w)
	if !d.ask("Start Mewla automatically when this computer starts?") {
		fmt.Fprintln(d.w, "Mewla runs while this terminal is open. Later: mewla boot install")
		return
	}
	args := []string{}
	if stateDir != "" {
		args = append(args, "-state-dir", stateDir)
	}
	if choice == choiceLAN {
		args = append(args, "-lan")
	}
	config, err := parseBootConfig("mewla boot install", args)
	var report bytes.Buffer
	if err == nil {
		if bootGOOS == "darwin" {
			err = bootLaunchdInstall(config, execBootRunner{}, &report)
		} else {
			err = bootInstall(config, execBootRunner{}, &report)
		}
	}
	switch {
	case err == nil:
		fmt.Fprintln(d.w, "Mewla now runs in the background and starts with this computer. Turn it off: mewla boot uninstall")
	case strings.Contains(err.Error(), "boot linger required"):
		fmt.Fprintf(d.w, "Mewla now runs in the background and starts when you log in.\nTo start it at boot before anyone logs in: sudo loginctl enable-linger %s\n", currentUserName())
	default:
		fmt.Fprintf(d.w, "Could not start Mewla in the background: %v\nMewla runs in this terminal instead. Later: mewla boot install\n", err)
	}
}

// runDepsCommand is `mewla deps`: check tmux and an agent CLI, offering to
// install what is missing. The installer runs `mewla deps -only tmux`.
func runDepsCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla deps", flag.ContinueOnError)
	fs.SetOutput(stderr)
	only := ""
	fs.StringVar(&only, "only", "", "check one dependency: tmux or agent")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla deps [-only tmux|agent]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Check tmux and an agent CLI. In a terminal, offer this computer's official")
		fmt.Fprintln(stderr, "install command and run it after you agree; otherwise print it.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || (only != "" && only != "tmux" && only != "agent") {
		return fmt.Errorf("usage: mewla deps [-only tmux|agent]")
	}
	prepareToolPath()
	d := newDepsPrompt(bufio.NewReader(os.Stdin), stderr, stdioIsTerminal())
	ok := true
	if only != "agent" {
		ok = d.ensureTmux() && ok
	}
	if only != "tmux" {
		ok = d.ensureAgent() && ok
	}
	if !ok {
		return errDepsMissing
	}
	return nil
}

var errDepsMissing = errors.New("mewla deps: something is still missing")
