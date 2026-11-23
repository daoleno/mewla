package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/statedir"
)

func TestGCStatusInsideASessionListsOnlyItsOwnTrash(t *testing.T) {
	root := t.TempDir()
	statedir.SetRoot(root)
	t.Cleanup(func() { statedir.SetRoot("") })
	unit := "mewla-worker-abc-unit-a.scope"
	scratch := filepath.Join(root, "t", "aaaaaa")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, ".mewla-worker-unit"), []byte(unit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{unit, "mewla-worker-abc-unit-b.scope"} {
		dir := filepath.Join(root, "t", "x"+owner[len(owner)-7:len(owner)-6])
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := gc.Trash(root, gc.KindScratch, dir, owner, "Session closed", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MEWLA_WORKER_RESOURCE_UNIT", unit)
	t.Setenv("MEWLA_WORKER_RESOURCE_OWNER", "abc")
	t.Setenv("TMPDIR", scratch)

	var out bytes.Buffer
	if err := runGCStatus(nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"This Session", scratch, "When it ends", "Trash: 1,"} {
		if !strings.Contains(text, want) {
			t.Fatalf("status lacks %q:\n%s", want, text)
		}
	}
	out.Reset()
	if err := runGCStatus([]string{"--all"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Trash: 2,") {
		t.Fatalf("--all status:\n%s", out.String())
	}
}

func TestGCRestorePutsAnItemBack(t *testing.T) {
	root := t.TempDir()
	statedir.SetRoot(root)
	t.Cleanup(func() { statedir.SetRoot("") })
	t.Setenv("MEWLA_WORKER_RESOURCE_UNIT", "")
	dir := filepath.Join(root, "t", "bbbbbb")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	item, err := gc.Trash(root, gc.KindScratch, dir, "u", "Session gone from the session host", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runGCRestore([]string{item.ID}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "Restored "+dir {
		t.Fatalf("restore printed %q", out.String())
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
