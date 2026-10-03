package brain

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreEnsuresSeedPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	for _, name := range seedPlaybookFilenames() {
		path := store.playbookPath(name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read playbook %s: %v", name, err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			t.Fatalf("playbook %s is empty", name)
		}
	}

	align, err := os.ReadFile(store.playbookPath("align.md"))
	if err != nil {
		t.Fatalf("read align playbook: %v", err)
	}
	for _, marker := range []string{
		"check discoverable facts", "Continue authorized preparation", "independent material decisions together",
		"recommended default", "remaining unknowns have safe defaults", "completion is observable",
		"actual permission boundary",
	} {
		if !strings.Contains(string(align), marker) {
			t.Fatalf("align playbook missing %q:\n%s", marker, align)
		}
	}
	for _, obsolete := range []string{"one question at a time", "Grill before you delegate", "Do not stack questions"} {
		if strings.Contains(string(align), obsolete) {
			t.Fatalf("align playbook retained obsolete %q:\n%s", obsolete, align)
		}
	}

	delegateBrief, err := os.ReadFile(store.playbookPath("delegate-brief.md"))
	if err != nil {
		t.Fatalf("read delegate-brief playbook: %v", err)
	}
	for _, marker := range []string{
		"outcome, cwd, necessary context, acceptance criteria", "verification and expected report",
	} {
		if !strings.Contains(string(delegateBrief), marker) {
			t.Fatalf("delegate-brief playbook missing %q:\n%s", marker, delegateBrief)
		}
	}
}

func TestNewStorePreservesCustomNonProductPlaybookContent(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	playbooks := filepath.Join(workspace, "playbooks")
	if err := os.MkdirAll(playbooks, 0o700); err != nil {
		t.Fatalf("create playbooks dir: %v", err)
	}
	customAlign := "---\ndescription: Custom align playbook.\n---\n\n# Custom Align\n\nKeep my custom note.\n"
	if err := os.WriteFile(filepath.Join(playbooks, "align.md"), []byte(customAlign), 0o600); err != nil {
		t.Fatalf("write custom align playbook: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(playbooks, "align.md"))
	if err != nil {
		t.Fatalf("read align playbook: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "Keep my custom note.") {
		t.Fatalf("custom align playbook was overwritten:\n%s", content)
	}
	if !strings.Contains(content, "Custom align playbook.") {
		t.Fatalf("custom align frontmatter was lost:\n%s", content)
	}
}

func TestRetiringEditedBrainFlowsKeepsUserNotesAndDropsProductBlock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "workspace", "playbooks", "brain-flows.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	user := "---\ndescription: Existing Brain flows.\n---\n\n# Brain Flows\n\nUser routing note must survive.\n"
	stale := user + "\n" + managedStartMarker(brainFlowsManagedID) + "\nold product contract\n" + managedEndMarker(brainFlowsManagedID) + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != user {
		t.Fatalf("edited brain-flows = %q, err=%v; want user notes only", got, err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); !bytes.Equal(again, got) {
		t.Fatalf("second reconciliation changed bytes:\n%s", again)
	}
}

func TestPlaybookCatalogListsSeedPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatalf("PlaybookCatalog() error = %v", err)
	}
	if len(catalog.Playbooks) != 4 {
		t.Fatalf("catalog playbooks = %d, want 4: %#v", len(catalog.Playbooks), catalog.Playbooks)
	}

	byName := map[string]PlaybookEntry{}
	for _, entry := range catalog.Playbooks {
		byName[entry.Name] = entry
	}
	for _, name := range []string{"align", "delegate-brief", "slice-work", "wayfind"} {
		entry, ok := byName[name]
		if !ok {
			t.Fatalf("catalog missing playbook %q: %#v", name, catalog.Playbooks)
		}
		if entry.Path != "playbooks/"+name+".md" {
			t.Fatalf("playbook %q path = %q, want playbooks/%s.md", name, entry.Path, name)
		}
		if strings.TrimSpace(entry.Description) == "" {
			t.Fatalf("playbook %q missing description", name)
		}
	}
	if !strings.Contains(byName["align"].Description, "consequential missing decisions") {
		t.Fatalf("align description = %q", byName["align"].Description)
	}
	if !strings.Contains(byName["wayfind"].Description, "next executable concern") {
		t.Fatalf("wayfind description = %q", byName["wayfind"].Description)
	}
}

func TestParsePlaybookDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "frontmatter description",
			content: `---
description: Resolve the current decision frontier.
---

# Align
`,
			want: "Resolve the current decision frontier.",
		},
		{
			name: "blockquote fallback",
			content: `# Title

> Short summary line.
`,
			want: "Short summary line.",
		},
		{
			name:    "empty",
			content: "",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePlaybookDescription(tc.content); got != tc.want {
				t.Fatalf("parsePlaybookDescription() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHousekeepingCreatesMissingPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := os.RemoveAll(store.playbooksPath()); err != nil {
		t.Fatalf("remove playbooks dir: %v", err)
	}

	service := NewService(store, nil, nil)
	report, err := service.Housekeeping()
	if err != nil {
		t.Fatalf("Housekeeping() error = %v", err)
	}
	if len(report.ChangedPaths) == 0 {
		t.Fatalf("expected repaired workspace report: %+v", report)
	}
	for _, path := range seedPlaybookPaths() {
		if !containsString(report.ChangedPaths, path) {
			t.Fatalf("changed paths %v missing %q", report.ChangedPaths, path)
		}
	}
	if len(report.PlaybookPaths) != 4 {
		t.Fatalf("playbook paths = %#v", report.PlaybookPaths)
	}
	if _, err := os.Stat(store.playbookPath("delegate-brief.md")); err != nil {
		t.Fatalf("delegate-brief playbook not backfilled: %v", err)
	}
}
