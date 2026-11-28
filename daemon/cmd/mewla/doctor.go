package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

func runDoctorCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := false
	stateDir := ""
	addr := ""
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON")
	fs.StringVar(&stateDir, "state-dir", "", "state directory for daemon identity and readiness checks")
	fs.StringVar(&addr, "addr", "127.0.0.1:9876", "daemon listen address to probe")
	fs.Usage = func() {
		printDoctorUsage(stderr)
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	report, err := doctor.Run(doctorOptions(stateDir, addr))
	if err != nil {
		return err
	}

	report.Warnings = append(report.Warnings, daemonViewWarnings(report.Executors, daemonExecutors(resolveCLIStateDir(stateDir)))...)

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		if err := doctor.WriteHuman(os.Stdout, report); err != nil {
			return err
		}
	}
	if !report.Ready {
		return doctor.ErrNotReady
	}
	return nil
}

// doctorOptions finds agents the way the running Mewla does: on PATH, then in
// the directories the native installers use (~/.local/bin), so an agent the
// daemon runs is not reported missing here just because the shell's PATH
// lacks it.
func doctorOptions(stateDir, addr string) doctor.Options {
	return doctor.Options{StateDir: stateDir, Addr: addr, LookPath: hostdeps.LookPath}
}

// daemonExecutors is the agents as the running daemon sees them, or nil when
// none is running. This command looks from the terminal; Brain's host is
// launched by the daemon, which a login service starts without the shell's
// environment.
func daemonExecutors(stateDir string) *doctor.ExecutorsCheck {
	resp, err := callControl(cliConfig{stateDir: stateDir}, control.Request{Type: "executor_check"})
	if err != nil || !resp.OK || resp.Context == nil {
		return nil
	}
	raw, err := json.Marshal(resp.Context)
	if err != nil {
		return nil
	}
	var check doctor.ExecutorsCheck
	if json.Unmarshal(raw, &check) != nil {
		return nil
	}
	return &check
}

// daemonViewWarnings names each agent this terminal finds ready that the
// running Mewla does not, with the step that fixes it. A path that only
// exists in the shell's environment is invisible to the daemon.
func daemonViewWarnings(here doctor.ExecutorsCheck, daemon *doctor.ExecutorsCheck) []string {
	if daemon == nil {
		return nil
	}
	seen := map[string]doctor.ExecutorCheck{}
	for _, item := range daemon.Items {
		seen[item.ID] = item
	}
	var warnings []string
	for _, item := range here.Items {
		theirs, ok := seen[item.ID]
		if !ok {
			continue
		}
		// The other way round: Brain runs on what the running Mewla sees, so a
		// terminal that finds the agent missing (a different PATH) must not
		// leave "NOT READY" as the last word on a Brain that works.
		if theirs.BinaryFound && !item.BinaryFound {
			warnings = append(warnings, fmt.Sprintf("%s is on the running Mewla's PATH, so Brain can use it, but not on this terminal's. The agent checks and host above come from this terminal, not from the running Mewla.", item.Name))
			continue
		}
		if item.BinaryFound && !theirs.BinaryFound {
			warnings = append(warnings, fmt.Sprintf("%s is on your PATH here, but not on the running Mewla's. Install %s somewhere standard such as /usr/local/bin, or stop Mewla and start it again from this terminal (run mewla).", item.Name, item.Name))
		}
	}
	return warnings
}

func printDoctorUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: mewla doctor [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Diagnose whether this machine can run Mewla (state dir, listen port, executors, agents).")
	fmt.Fprintln(w, "Never installs packages or prints credentials. Exit status is nonzero when not ready.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  mewla doctor")
	fmt.Fprintln(w, "  mewla doctor --json")
	fmt.Fprintln(w, "  mewla doctor --state-dir /tmp/mewla-state --addr 127.0.0.1:9876")
}
