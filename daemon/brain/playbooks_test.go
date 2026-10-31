package brain

import (
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

	// Align was folded into the AGENTS.md Role section.
	if _, err := os.Stat(store.playbookPath("align.md")); !os.IsNotExist(err) {
		t.Fatalf("retired align playbook was seeded: %v", err)
	}
	for _, marker := range []string{"neither facts nor a small test can settle it", "ask every open decision at once", "recommended default", "keep doing independent preparation"} {
		if !strings.Contains(productWorkspaceInstructions, marker) {
			t.Fatalf("AGENTS.md missing alignment rule %q", marker)
		}
	}

	delegateBrief, err := os.ReadFile(store.playbookPath("delegate-brief.md"))
	if err != nil {
		t.Fatalf("read delegate-brief playbook: %v", err)
	}
	for _, marker := range []string{
		"Turn the chosen method into concrete work", "Design proof around what the user does",
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

func TestPlaybookCatalogListsSeedPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatalf("PlaybookCatalog() error = %v", err)
	}
	if len(catalog.Playbooks) != 5 {
		t.Fatalf("catalog playbooks = %d, want 5: %#v", len(catalog.Playbooks), catalog.Playbooks)
	}

	byName := map[string]PlaybookEntry{}
	for _, entry := range catalog.Playbooks {
		byName[entry.Name] = entry
	}
	for _, name := range []string{"delegate-brief", "slice-work", "wayfind", "friends"} {
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
	if !strings.Contains(byName["wayfind"].Description, "check reuse before building") {
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
	if len(report.PlaybookPaths) != 5 {
		t.Fatalf("playbook paths = %#v", report.PlaybookPaths)
	}
	if _, err := os.Stat(store.playbookPath("delegate-brief.md")); err != nil {
		t.Fatalf("delegate-brief playbook not backfilled: %v", err)
	}
}

// Friends is off by default: its playbook must say so first, and must never
// give the Brain a way to read other people's posts.
func TestFriendsPlaybookIsGatedAndWriteOnly(t *testing.T) {
	content := defaultFriendsPlaybook
	gate := strings.Index(content, "mewla friends status --json")
	if gate < 0 || gate > strings.Index(content, "## When") {
		t.Fatal("the playbook does not gate itself on Friends being on before anything else")
	}
	for _, want := range []string{"mewla friends draft", "It posts nothing", "-media", "I made", "my AI agents made", "untrusted quoted material", "cannot read, like"} {
		if !strings.Contains(content, want) {
			t.Errorf("playbook lacks %q", want)
		}
	}
	if len(content) > 4300 {
		t.Errorf("playbook is %d bytes; keep it lazy-loadable", len(content))
	}
}

// A feature that is off is not announced: its playbook is neither written nor
// listed, an untouched copy goes, and turning the feature on brings it back.
func TestGatedPlaybookFollowsItsFeature(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PlaybookCatalog(); err != nil {
		t.Fatal(err)
	}
	path := store.playbookPath("friends.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ungated playbook not seeded: %v", err)
	}

	on := false
	store.GateSeed("playbooks/friends.md", func() bool { return on })
	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog.Playbooks {
		if entry.Name == "friends" {
			t.Fatalf("friends listed while off: %#v", catalog.Playbooks)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("untouched friends playbook stayed on disk while off: %v", err)
	}
	if containsString(store.shownPlaybookPaths(), "playbooks/friends.md") || len(store.shownPlaybookPaths()) != 4 {
		t.Fatalf("shown paths = %v", store.shownPlaybookPaths())
	}

	on = true
	catalog, err = store.PlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range catalog.Playbooks {
		found = found || entry.Name == "friends"
	}
	if !found {
		t.Fatalf("friends missing while on: %#v", catalog.Playbooks)
	}

	// A playbook the owner edited is never deleted, only left out of the list.
	if err := os.WriteFile(path, []byte("---\ndescription: mine\n---\nmine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	on = false
	catalog, err = store.PlaybookCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range catalog.Playbooks {
		if entry.Name == "friends" {
			t.Fatalf("edited friends playbook listed while off")
		}
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "mine") {
		t.Fatalf("edited playbook lost: %v", err)
	}
}
