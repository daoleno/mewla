package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

// depsPrompt checks the one thing Brain cannot work without, a signed-in agent
// CLI, and offers the official command when it is missing.
// It never blocks a start: a "no", a failed install or a closed input prints
// the command and moves on.
type depsPrompt struct {
	in          *bufio.Reader
	w           io.Writer
	interactive bool
	platform    hostdeps.Platform
	// run executes one shown command attached to the terminal.
	run func(command string) error
	// agents re-checks the machine; tests replace it.
	agents func() doctor.ExecutorsCheck
	// brainHost names the agent Brain already uses; chosen is true when one
	// is set by mewla brain use, an earlier first run, a Brain that already
	// ran (id may then be empty) or MEWLA_BRAIN_HOST_EXECUTOR. setBrainHost
	// saves a choice where mewla brain use does. Tests replace both.
	brainHost    func() (id string, chosen bool)
	setBrainHost func(id string) error
}

func newDepsPrompt(in *bufio.Reader, w io.Writer, interactive bool) *depsPrompt {
	options := doctor.Options{LookPath: hostdeps.LookPath}
	return &depsPrompt{
		in:          in,
		w:           w,
		interactive: interactive,
		platform:    hostdeps.Detect(),
		run:         runShownCommand,
		agents:      func() doctor.ExecutorsCheck { return doctor.CheckExecutors(options) },
		brainHost:   currentBrainHost,
		setBrainHost: func(id string) error {
			root, err := brain.DefaultRoot()
			if err != nil {
				return err
			}
			return brain.SaveHostExecutorID(root, id)
		},
	}
}

// currentBrainHost reads the agent Brain uses from the environment override
// or the Brain host binding mewla brain use writes.
func currentBrainHost() (string, bool) {
	if id := strings.TrimSpace(os.Getenv("MEWLA_BRAIN_HOST_EXECUTOR")); id != "" {
		return id, true
	}
	root, err := brain.DefaultRoot()
	if err != nil {
		return "", false
	}
	host, err := brain.LoadHostSession(root)
	if err != nil {
		// Unreadable is not "never chosen": leave it alone.
		return "", true
	}
	return host.ExecutorID, host.ExecutorID != "" || host.ID != ""
}

// setRunningBrainHost moves the Brain of the Mewla already running to id,
// exactly as mewla brain use does.
func setRunningBrainHost(stateDir, id string) error {
	resp, err := callControl(cliConfig{stateDir: stateDir}, control.Request{Type: "brain_set_executor", ExecutorID: id})
	if err != nil {
		return err
	}
	return controlResponseError(resp)
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

// style is how this prompt's writer renders emphasis.
func (d *depsPrompt) style() termStyle { return styleForWriter(d.w) }

// ask reads a Y/n answer; Enter means yes, closed input means no.
func (d *depsPrompt) ask(question string) bool {
	if !d.interactive {
		return false
	}
	for {
		fmt.Fprintf(d.w, "%s %s ", d.style().bold(question), d.style().dim("[Y/n]:"))
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
		fmt.Fprintf(d.w, "\n%s %s\n", d.style().dim("$"), d.style().cmd(step))
		if err := d.run(step); err != nil {
			fmt.Fprintf(d.w, "\nThat command failed (%v).\n", err)
			return false
		}
	}
	fmt.Fprintln(d.w)
	return true
}

// agentNames are the display names of the executors first run reports.
var agentNames = map[string]string{
	"claude": "Claude Code", "codex": "Codex", "agent": "Cursor Agent",
	"grok": "Grok", "pi": "Pi", "opencode": "OpenCode",
}

func agentName(id string) string {
	if name, ok := agentNames[id]; ok {
		return name
	}
	return id
}

// checkAll shows the agent CLI as a status row, then offers one when none is
// ready.
func (d *depsPrompt) checkAll() (agentReady bool, err error) {
	style := d.style()
	agents := d.agents()
	agentReady = readyAgent(agents) != ""
	agentValue := "none installed"
	var choices []doctor.ExecutorCheck
	switch {
	case agentReady:
		ready := readyAgents(agents)
		host, chosen := d.brainHost()
		switch {
		case chosen && host != "":
			agentValue = agentName(host) + style.dim(" · Brain uses it")
		case chosen:
			agentValue = agentList(ready) + style.dim(" · Brain keeps its agent")
		case len(ready) > 1:
			agentValue = agentList(ready) + style.dim(" · ready")
			choices = ready
		default:
			agentValue = agentName(agents.RecommendedHost) + style.dim(" · Brain uses it")
		}
	case anyInstalledAgent(agents) != "":
		agentValue = agentName(anyInstalledAgent(agents)) + " · not signed in"
	}
	fmt.Fprintln(d.w)
	fmt.Fprintln(d.w, style.bold("This computer"))
	fmt.Fprintln(d.w, style.check(agentReady, "Agent CLI", agentValue))
	for _, item := range agents.Items {
		if outdatedAgent(item) {
			fmt.Fprintf(d.w, "  %s\n", item.Summary)
		}
	}
	if len(choices) > 1 {
		if err := d.chooseBrainHost(choices); err != nil {
			return agentReady, err
		}
	}
	if !agentReady {
		fmt.Fprintln(d.w)
		agentReady = d.ensureAgent()
	}
	return agentReady, nil
}

// chooseBrainHost asks once which ready agent Brain runs on, Claude Code
// first, and saves the answer as Brain's host. Without a terminal it takes
// the first and says how to change it. Ctrl-C stops first run with nothing
// saved, so it is asked again.
func (d *depsPrompt) chooseBrainHost(ready []doctor.ExecutorCheck) error {
	style := d.style()
	fmt.Fprintln(d.w)
	pick := 0
	if d.interactive {
		fmt.Fprintln(d.w, style.bold("Which agent should Brain use?"))
		fmt.Fprintln(d.w)
		options := make([]menuOption, 0, len(ready))
		for _, item := range ready {
			note := "sign-in not checked"
			if item.Auth == doctor.AuthAuthenticated {
				note = "signed in"
			}
			options = append(options, menuOption{label: agentName(item.ID), note: note})
		}
		switch pick = chooseOption(d.in, d.w, options); pick {
		case menuCancelled:
			return errStartupCancelled
		case menuClosed:
			fmt.Fprintf(d.w, "Brain's agent is not chosen yet. Choose any time: %s\n", style.cmd(brainUseHint(readyIDs(ready))))
			return nil
		}
	}
	chosen := ready[pick]
	if err := d.setBrainHost(chosen.ID); err != nil {
		fmt.Fprintf(d.w, "Could not save Brain's agent (%v). Choose later: %s\n", err, style.cmd("mewla brain use "+chosen.ID))
		return nil
	}
	others := slices.DeleteFunc(readyIDs(ready), func(id string) bool { return id == chosen.ID })
	verb := "uses"
	if !d.interactive {
		verb = "uses the first ready agent,"
	}
	fmt.Fprintf(d.w, "✓ Brain %s %s. Change it any time: %s\n", verb, agentName(chosen.ID), style.cmd(brainUseHint(others)))
	return nil
}

// brainUseHint is the mewla brain use command that moves Brain to one of ids.
func brainUseHint(ids []string) string {
	if len(ids) == 1 {
		return "mewla brain use " + ids[0]
	}
	return "mewla brain use <" + strings.Join(ids, "|") + ">"
}

// readyAgents lists usable executors in hostdeps' AgentOrder, the order the
// install menu uses.
func readyAgents(check doctor.ExecutorsCheck) []doctor.ExecutorCheck {
	ready := []doctor.ExecutorCheck{}
	for _, item := range check.Items {
		if item.Usable {
			ready = append(ready, item)
		}
	}
	slices.SortStableFunc(ready, func(a, b doctor.ExecutorCheck) int {
		return hostdeps.AgentRank(a.ID) - hostdeps.AgentRank(b.ID)
	})
	return ready
}

func readyIDs(ready []doctor.ExecutorCheck) []string {
	ids := make([]string, 0, len(ready))
	for _, item := range ready {
		ids = append(ids, item.ID)
	}
	return ids
}

func agentList(ready []doctor.ExecutorCheck) string {
	names := make([]string, 0, len(ready))
	for _, item := range ready {
		names = append(names, agentName(item.ID))
	}
	return strings.Join(names, ", ")
}

func anyInstalledAgent(check doctor.ExecutorsCheck) string {
	for _, item := range check.Items {
		if item.BinaryFound && !outdatedAgent(item) {
			return item.ID
		}
	}
	return ""
}

// outdatedAgent reports an installed agent CLI whose version Mewla does not
// support (OpenCode 1); doctor's summary says how to upgrade.
func outdatedAgent(item doctor.ExecutorCheck) bool {
	return item.BinaryFound && item.Remediation == doctor.RemediationInstallExecutor
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
	options := make([]menuOption, 0, len(agents)+1)
	for _, agent := range agents {
		options = append(options, menuOption{label: agent.Name, note: agent.Install.Command()})
	}
	options = append(options, menuOption{label: "Skip for now"})
	choice := chooseOption(d.in, d.w, options) + 1
	if choice <= 0 || choice == len(options) {
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
		fmt.Fprintf(d.w, "✓ %s is ready. Brain will use it.\n", agent.Name)
		return true
	}
	return d.signIn(agent)
}

func (d *depsPrompt) signIn(agent hostdeps.Agent) bool {
	fmt.Fprintf(d.w, "%s is installed. Sign in to it:\n  %s\n", agent.Name, d.style().cmd(agent.Login))
	if !d.ask("Sign in now?") {
		return false
	}
	fmt.Fprintf(d.w, "\n%s %s\n", d.style().dim("$"), d.style().cmd(agent.Login))
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
	fmt.Fprintf(d.w, "\n✓ %s is ready. Brain will use it.\n", agent.Name)
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
// daemon, the Sessions it starts and a boot unit that copies PATH.
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
// whether any agent CLI is installed (no sign-in probes). It offers an install
// in a terminal and prints the commands otherwise.
func quickDepsCheck(stderr io.Writer) {
	interactive := stdioIsTerminal() && !daemonSupervised() && os.Getenv("CI") == ""
	d := newDepsPrompt(bufio.NewReader(os.Stdin), stderr, interactive)
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
	if bootServiceInstalled() {
		return
	}
	switch bootGOOS {
	case "linux":
		if _, err := (execBootRunner{}).run("systemctl", "--user", "show-environment"); err != nil {
			fmt.Fprintln(d.w, "\nNo systemd user services here, so Mewla runs while this terminal is open.")
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
	err := installBootService(stateDir, choice == choiceLAN)
	switch {
	case err == nil:
		fmt.Fprintln(d.w, "Mewla now runs in the background and starts with this computer. Turn it off: mewla boot uninstall")
	case errors.Is(err, errBootLingerRequired):
		fmt.Fprintf(d.w, "Mewla now runs in the background and starts when you log in.\nTo start it at boot before anyone logs in: sudo loginctl enable-linger %s\n", currentUserName())
	default:
		fmt.Fprintf(d.w, "Could not start Mewla in the background: %v\nMewla runs in this terminal instead. Later: mewla boot install\n", err)
	}
}

// bootServiceInstalled reports whether mewla boot install already set up a
// systemd unit or LaunchAgent, so first run does not ask again.
func bootServiceInstalled() bool {
	path, marker := "", bootManagedMarker
	var err error
	switch bootGOOS {
	case "linux":
		path, err = bootUnitPath()
	case "darwin":
		path, err = bootLaunchdPlistPath()
		marker = bootLaunchdMarker
	default:
		return false
	}
	if err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), marker)
}

// runDepsCommand is `mewla deps`: check for an agent CLI, offering to
// install one when none is ready.
func runDepsCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla deps", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla deps")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Check for an agent CLI. In a terminal, offer this computer's official")
		fmt.Fprintln(stderr, "install command and run it after you agree; otherwise print it.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: mewla deps")
	}
	prepareToolPath()
	d := newDepsPrompt(bufio.NewReader(os.Stdin), stderr, stdioIsTerminal())
	if !d.ensureAgent() {
		return errDepsMissing
	}
	return nil
}

var errDepsMissing = errors.New("mewla deps: something is still missing")
