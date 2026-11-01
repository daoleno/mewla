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

	report, err := doctor.Run(doctor.Options{
		StateDir: stateDir,
		Addr:     addr,
	})
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
// running Mewla does not, with the step that fixes it. A sign-in or path that
// only exists in the shell's environment is invisible to the daemon.
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
		if !ok || !item.BinaryFound {
			continue
		}
		switch {
		case !theirs.BinaryFound:
			warnings = append(warnings, fmt.Sprintf("%s is on your PATH here, but not on the running Mewla's. Install %s somewhere standard such as /usr/local/bin, or stop Mewla and start it again from this terminal (run mewla).", item.Name, item.Name))
		case item.VerifiedAuthenticated && !theirs.VerifiedAuthenticated:
			warnings = append(warnings, fmt.Sprintf("%s is signed in here, but the running Mewla cannot see that sign-in: it comes from your shell's environment, which Mewla does not get when it starts at login. Sign in once with %s's own login so it is kept for every program, or stop Mewla and start it again from this terminal (run mewla).", item.Name, item.Name))
		}
	}
	return warnings
}

func printDoctorUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: mewla doctor [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Diagnose whether this machine can run Mewla (tmux, state dir, listen port, executors).")
	fmt.Fprintln(w, "Never installs packages or prints credentials. Exit status is nonzero when not ready.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  mewla doctor")
	fmt.Fprintln(w, "  mewla doctor --json")
	fmt.Fprintln(w, "  mewla doctor --state-dir /tmp/mewla-state --addr 127.0.0.1:9876")
}
