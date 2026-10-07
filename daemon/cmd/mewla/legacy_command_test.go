package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureMewlaCommandLinksBesideLegacyBinary(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "zen")
	if err := os.WriteFile(legacy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ensureMewlaCommand(legacy)
	ensureMewlaCommand(legacy)
	if target, err := os.Readlink(filepath.Join(dir, "mewla")); err != nil || target != "zen" {
		t.Fatalf("mewla -> %q (%v), want zen", target, err)
	}
}

func TestEnsureMewlaCommandLeavesOtherNamesAlone(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "mewla")
	if err := os.WriteFile(existing, []byte("real"), 0o755); err != nil {
		t.Fatal(err)
	}
	ensureMewlaCommand(filepath.Join(dir, "zen"))
	if data, err := os.ReadFile(existing); err != nil || string(data) != "real" {
		t.Fatalf("existing mewla replaced: %q %v", data, err)
	}

	other := t.TempDir()
	for _, name := range []string{"mewla", "zen-dev", "mewla-dev"} {
		ensureMewlaCommand(filepath.Join(other, name))
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Fatalf("unexpected entries for non-legacy executables: %v", entries)
	}
}
