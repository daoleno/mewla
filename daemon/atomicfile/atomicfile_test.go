package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteReplacesWithModeIndependentOfUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "new" {
		t.Fatalf("content = %q, %v", raw, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}
	assertNoTemporaries(t, filepath.Dir(path))
}

func TestWriteRequiresAnExistingParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	if err := Write(path, []byte("x"), 0o600); err == nil {
		t.Fatal("write into a missing directory succeeded")
	}
}

func TestFailureBeforeRenameKeepsOldContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected")
	for _, phase := range []string{"before_write", "after_write", "before_sync", "after_sync", "before_rename"} {
		err := WriteOptions(path, []byte("new"), 0o600, Options{Hook: func(name string) error {
			if name == phase {
				return injected
			}
			return nil
		}})
		if !errors.Is(err, injected) || errors.Is(err, ErrDirSync) {
			t.Fatalf("%s: err = %v", phase, err)
		}
		if raw, _ := os.ReadFile(path); string(raw) != "old" {
			t.Fatalf("%s: content = %q", phase, raw)
		}
		assertNoTemporaries(t, filepath.Dir(path))
	}
}

func TestFailureAfterRenameIsDirSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	injected := errors.New("injected")
	for _, phase := range []string{"after_rename", "before_dirsync", "after_dirsync"} {
		err := WriteOptions(path, []byte(phase), 0o600, Options{Hook: func(name string) error {
			if name == phase {
				return injected
			}
			return nil
		}})
		if !errors.Is(err, ErrDirSync) || !errors.Is(err, injected) {
			t.Fatalf("%s: err = %v", phase, err)
		}
		if raw, _ := os.ReadFile(path); string(raw) != phase {
			t.Fatalf("%s: content = %q", phase, raw)
		}
	}
	err := WriteOptions(path, []byte("sync"), 0o600, Options{SyncDir: func(string) error { return injected }})
	if !errors.Is(err, ErrDirSync) || !errors.Is(err, injected) {
		t.Fatalf("SyncDir: err = %v", err)
	}
	assertNoTemporaries(t, filepath.Dir(path))
}

func TestWriteStreamBuffersAndAborts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.jsonl")
	if err := WriteStream(path, 0o600, func(w io.Writer) error {
		for range 3 {
			if _, err := io.WriteString(w, "line\n"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != strings.Repeat("line\n", 3) {
		t.Fatalf("content = %q", raw)
	}
	injected := errors.New("injected")
	if err := WriteStream(path, 0o600, func(io.Writer) error { return injected }); !errors.Is(err, injected) {
		t.Fatalf("err = %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != strings.Repeat("line\n", 3) {
		t.Fatalf("aborted stream changed content to %q", raw)
	}
	assertNoTemporaries(t, filepath.Dir(path))
}

func assertNoTemporaries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}
