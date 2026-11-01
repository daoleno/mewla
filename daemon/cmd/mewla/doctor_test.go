package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/mewla/daemon/doctor"
	"github.com/daoleno/mewla/daemon/hostdeps"
)

func TestDoctorHelp(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"doctor", "--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want ErrHelp", err)
	}
	out := stderr.String()
	for _, want := range []string{"Usage: mewla doctor", "--json", "Diagnose whether this machine can run Mewla"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestMainHelpListsDoctor(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "doctor") {
		t.Fatalf("main help missing doctor:\n%s", stderr.String())
	}
}

func TestDoctorJSONSchemaSmoke(t *testing.T) {
	// Library-level smoke through the same Report shape the CLI encodes.
	report := doctor.Report{
		Ready: false,
		Checks: []doctor.NamedCheck{
			{ID: "tmux", Status: doctor.StatusFail, Remediation: doctor.RemediationInstallTmux, Summary: "tmux not found on PATH"},
		},
		Remediations: []doctor.Remediation{doctor.RemediationInstallTmux},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"ready":false`) {
		t.Fatalf("unexpected JSON: %s", raw)
	}
}

func TestDaemonViewWarningsNameWhatOnlyTheShellSees(t *testing.T) {
	ready := doctor.ExecutorCheck{ID: "claude", Name: "Claude Code", BinaryFound: true, VerifiedAuthenticated: true}
	here := doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{ready}}

	if got := daemonViewWarnings(here, nil); len(got) != 0 {
		t.Fatalf("no daemon running must warn nothing, got %v", got)
	}

	same := &doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{ready}}
	if got := daemonViewWarnings(here, same); len(got) != 0 {
		t.Fatalf("matching views must warn nothing, got %v", got)
	}

	// Signed in through the shell's environment: the daemon, started at login
	// without it, sees the same binary signed out.
	signedOut := ready
	signedOut.VerifiedAuthenticated = false
	got := daemonViewWarnings(here, &doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{signedOut}})
	if len(got) != 1 || !strings.Contains(got[0], "Claude Code is signed in here") || !strings.Contains(got[0], "run mewla") {
		t.Fatalf("want one sign-in warning with its fix, got %v", got)
	}

	// A binary on the shell's PATH only.
	missing := doctor.ExecutorCheck{ID: "claude", Name: "Claude Code"}
	got = daemonViewWarnings(here, &doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{missing}})
	if len(got) != 1 || !strings.Contains(got[0], "not on the running Mewla's") {
		t.Fatalf("want one PATH warning, got %v", got)
	}

	// The daemon does better than the terminal (the terminal's PATH or sign-in
	// environment differs): nothing to fix, but the terminal's host line is not
	// Brain's, and the output says so.
	for name, terminal := range map[string]doctor.ExecutorCheck{"signed out here": signedOut, "missing here": missing} {
		got := daemonViewWarnings(doctor.ExecutorsCheck{Items: []doctor.ExecutorCheck{terminal}}, same)
		if len(got) != 1 || !strings.Contains(got[0], "signed in for the running Mewla") || !strings.Contains(got[0], "Brain can use it") {
			t.Fatalf("%s: want one warning that Brain uses the daemon's view, got %v", name, got)
		}
	}
}

// The terminal looks for agents where the daemon does, so ~/.local/bin (where
// the native installers put claude) is not "missing" just because the shell's
// PATH lacks it.
func TestDoctorFindsAgentsWhereTheDaemonDoes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	restore := hostdeps.SetSystemPathDirs()
	t.Cleanup(restore)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := doctorOptions("", "").LookPath("claude")
	if err != nil || found != filepath.Join(bin, "claude") {
		t.Fatalf("LookPath = %q, %v; want the native installer's ~/.local/bin/claude", found, err)
	}
}
