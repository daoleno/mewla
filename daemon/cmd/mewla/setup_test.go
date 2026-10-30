package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
)

func TestSetupHelp(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"setup", "--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	out := stderr.String()
	for _, want := range []string{"Usage: mewla setup", "--non-interactive", "--host"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestMainHelpListsSetup(t *testing.T) {
	var stderr bytes.Buffer
	err := run([]string{"--help"}, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr.String(), "setup") {
		t.Fatalf("main help missing setup:\n%s", stderr.String())
	}
}

func TestSetupHasNoPermissionProfile(t *testing.T) {
	var stderr bytes.Buffer
	for _, flagName := range []string{"--profile", "--yes"} {
		stderr.Reset()
		if err := run([]string{"setup", flagName}, &stderr); err == nil {
			t.Fatalf("setup accepted removed flag %s", flagName)
		}
	}
}
