package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveTelegramStateDeletesTokenBindingAndBackups(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "brain", "brain.db")
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "telegram", "token"))
	write(filepath.Join(root, "telegram", "state.db"))
	write(filepath.Join(root, "legacy-telegram-20260101T000000Z", "telegram", "state.json"))
	write(keep)
	var out bytes.Buffer
	removeTelegramState(root, &out)
	for _, gone := range []string{"telegram", "legacy-telegram-20260101T000000Z"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s still there: %v", gone, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated state was touched: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("said something: %q", out.String())
	}
	removeTelegramState(root, &out)
	if out.Len() != 0 {
		t.Fatalf("second run said something: %q", out.String())
	}
}
