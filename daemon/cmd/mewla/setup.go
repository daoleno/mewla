package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/mewla/daemon/setup"
)

func runSetupCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla setup", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		asJSON         bool
		nonInteractive bool
		host           string
		profile        string
		yes            bool
		stateDir       string
		addr           string
	)
	fs.BoolVar(&asJSON, "json", false, "print machine-readable JSON result")
	fs.BoolVar(&nonInteractive, "non-interactive", false, "run without prompts (requires --host, --profile)")
	fs.StringVar(&host, "host", "", "host executor id")
	fs.StringVar(&profile, "profile", "safe", "permission profile: safe or autonomous")
	fs.BoolVar(&yes, "yes", false, "confirm Autonomous profile (required with --profile=autonomous in non-interactive mode)")
	fs.StringVar(&stateDir, "state-dir", "", "state directory for daemon identity")
	fs.StringVar(&addr, "addr", "127.0.0.1:9876", "daemon listen address for readiness checks")
	fs.Usage = func() {
		printSetupUsage(stderr)
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	normalized, err := normalizeSetupProfileFlag(profile)
	if err != nil {
		return err
	}

	humanOut := io.Writer(os.Stdout)
	if asJSON {
		humanOut = io.Discard
	}

	result, runErr := setup.Run(setup.Options{
		NonInteractive: nonInteractive,
		Host:           host,
		Profile:        normalized,
		Yes:            yes,
		StateDir:       stateDir,
		Addr:           addr,
		Stdin:          os.Stdin,
		Stdout:         humanOut,
		Stderr:         stderr,
	})

	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if encErr := encoder.Encode(result); encErr != nil {
			return encErr
		}
	} else if runErr != nil && !errors.Is(runErr, setup.ErrBlocked) && !errors.Is(runErr, setup.ErrNoExecutor) && !errors.Is(runErr, setup.ErrConsentRequired) {
		// Blocked/no-executor/consent already printed actionable output from setup.Run.
		// Unexpected errors get a clean message (no stack).
		fmt.Fprintf(stderr, "mewla setup: %v\n", runErr)
	}

	if runErr == nil {
		return nil
	}
	switch {
	case errors.Is(runErr, setup.ErrBlocked),
		errors.Is(runErr, setup.ErrNoExecutor),
		errors.Is(runErr, setup.ErrConsentRequired),
		errors.Is(runErr, setup.ErrInvalidArgs),
		errors.Is(runErr, setup.ErrIncomplete):
		return runErr
	default:
		return runErr
	}
}

func normalizeSetupProfileFlag(value string) (setup.Profile, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "safe", "manual":
		return setup.ProfileSafe, nil
	case "autonomous", "auto":
		return setup.ProfileAutonomous, nil
	default:
		return "", fmt.Errorf("%w: unknown --profile %q", setup.ErrInvalidArgs, value)
	}
}

func printSetupUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: mewla setup [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Guided first-run setup on top of mewla doctor.")
	fmt.Fprintln(w, "Never installs packages, runs sudo, logs into providers, or weakens permissions without consent.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  mewla setup")
	fmt.Fprintln(w, "  mewla setup --non-interactive --host codex --profile safe")
	fmt.Fprintln(w, "  mewla setup --non-interactive --host codex --profile autonomous --yes")
}
