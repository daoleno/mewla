package work

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeWorkItem(t *testing.T, path, id string) {
	t.Helper()

	content := `---
id: ` + id + `
created: 2026-04-21T00:00:00Z
---
# Item ` + id + `

Body.
`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestStore_Scan(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "mewla", "a.md"), "A")
	writeWorkItem(t, filepath.Join(root, "mewla", "b.md"), "B")
	writeWorkItem(t, filepath.Join(root, "inbox", "c.md"), "C")

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	all := store.List()
	if len(all) != 3 {
		t.Fatalf("len = %d, want 3", len(all))
	}
}

func TestStore_ScanSkipsNonWorkMarkdownSilently(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "mewla", "a.md"), "A")
	plain := filepath.Join(root, "calendar", "hn-2026-10-01", "briefing.md")
	writeFile(t, plain, "# Briefing\n\nPlain deliverable written by a job.\n")

	out := captureStderr(t, func() {
		store, err := NewStore(root)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		defer store.Close()

		all := store.List()
		if len(all) != 1 || all[0].ID != "A" {
			t.Fatalf("items = %#v, want only A", all)
		}
		if _, ok := store.GetByIDFromPath(plain); ok {
			t.Fatal("plain markdown must not be indexed")
		}
	})
	if out != "" {
		t.Fatalf("stderr = %q, want no output", out)
	}
}

func TestStore_ScanWarnsOnMalformedFrontmatter(t *testing.T) {
	root := t.TempDir()
	// Starts with the frontmatter delimiter but the YAML cannot be decoded.
	badType := filepath.Join(root, "mewla", "bad-type.md")
	writeFile(t, badType, "---\nid: [1, 2]\n---\n# Broken\n")
	// Starts with the frontmatter delimiter but never closes it.
	unclosed := filepath.Join(root, "mewla", "unclosed.md")
	writeFile(t, unclosed, "---\nid: X\n# Broken\n")

	out := captureStderr(t, func() {
		store, err := NewStore(root)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		defer store.Close()
		if all := store.List(); len(all) != 0 {
			t.Fatalf("items = %#v, want none", all)
		}
	})
	for _, path := range []string{badType, unclosed} {
		if !strings.Contains(out, "work: skip "+path) {
			t.Fatalf("stderr = %q, want skip warning for %s", out, path)
		}
	}
}

func TestStore_ScanKeepsValidItem(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "mewla", "a.md"), "A")

	out := captureStderr(t, func() {
		store, err := NewStore(root)
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		defer store.Close()
		if _, ok := store.GetByID("A"); !ok {
			t.Fatal("valid item A not indexed")
		}
	})
	if out != "" {
		t.Fatalf("stderr = %q, want no output", out)
	}
}

func TestStore_ReloadPathSkipsNonWorkMarkdown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mewla", "deliverable.md")
	writeFile(t, path, "# Deliverable\n")

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.reloadPath(path); !errors.Is(err, ErrMissingFrontmatter) {
		t.Fatalf("reloadPath err = %v, want ErrMissingFrontmatter", err)
	}
	if _, ok := store.GetByIDFromPath(path); ok {
		t.Fatal("non-work markdown must not be indexed by reloadPath")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	_ = r.Close()
	return string(data)
}

func TestStore_GetByID(t *testing.T) {
	root := t.TempDir()
	writeWorkItem(t, filepath.Join(root, "mewla", "a.md"), "A")

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	iss, ok := store.GetByID("A")
	if !ok {
		t.Fatal("work item A not found")
	}
	if iss.Project != "mewla" {
		t.Fatalf("project = %q", iss.Project)
	}
}

func TestStore_WriteAndRead(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mewla"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	iss := &Item{
		Path: filepath.Join(root, "mewla", "new.md"),
		Body: "# New\n\nBody.\n",
		Frontmatter: Frontmatter{
			ID:      "NEW",
			Created: time.Now().UTC(),
		},
	}
	written, err := store.Write(iss, time.Time{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.Title != "New" {
		t.Fatalf("title = %q", written.Title)
	}

	got, ok := store.GetByID("NEW")
	if !ok {
		t.Fatal("work item NEW not found")
	}
	if got.Title != "New" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestStore_WatchNotifiesOnChange(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mewla"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.StartWatcher(); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	_, ch := store.Subscribe()

	writeWorkItem(t, filepath.Join(root, "mewla", "live.md"), "LIVE")

	select {
	case ev := <-ch:
		if ev.Type != EventChanged {
			t.Fatalf("type = %q", ev.Type)
		}
		if ev.Item == nil || ev.Item.ID != "LIVE" {
			t.Fatalf("item = %#v", ev.Item)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for change event")
	}
}

func TestStore_WatchDebouncesMultipleWrites(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mewla"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.StartWatcher(); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	_, ch := store.Subscribe()

	path := filepath.Join(root, "mewla", "hot.md")
	writeWorkItem(t, path, "HOT")
	for range 3 {
		time.Sleep(50 * time.Millisecond)
		writeWorkItem(t, path, "HOT")
	}

	count := 0
	timeout := time.After(600 * time.Millisecond)
loop:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break loop
		}
	}

	if count == 0 {
		t.Fatal("expected at least one event")
	}
	if count > 2 {
		t.Fatalf("count = %d, want <= 2", count)
	}
}
