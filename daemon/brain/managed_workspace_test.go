package brain

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewStoreCreatesExactlyOneCanonicalManagedBlock(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, spec := range store.managedMarkdownSpecs() {
		raw := mustReadFile(t, spec.path)
		want := append(append([]byte{}, canonicalManagedBlock(spec)...), '\n')
		if !bytes.Equal(raw, want) {
			t.Fatalf("%s bytes differ from the canonical block:\n%s", spec.relativePath, raw)
		}
		if strings.Count(string(raw), managedStartMarker(spec.managedID)) != 1 ||
			strings.Count(string(raw), managedEndMarker(spec.managedID)) != 1 {
			t.Fatalf("%s does not contain exactly one marker pair:\n%s", spec.relativePath, raw)
		}
		assertFileMode(t, spec.path, 0o600)
	}
	if got := mustReadFile(t, store.profileNotesPath()); string(got) != defaultProfileNotes {
		t.Fatalf("profile.md = %q, want %q", got, defaultProfileNotes)
	}
	for _, retired := range []string{"soul.md", "playbooks/README.md", "playbooks/brain-flows.md"} {
		if _, err := os.Stat(filepath.Join(store.WorkspacePath(), retired)); !os.IsNotExist(err) {
			t.Fatalf("fresh workspace created retired %s: %v", retired, err)
		}
	}
}

func TestCleanHomeShipsAutonomousPolicyAndRepairPreservesPrivateOverlays(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	worklog := filepath.Join(workspace, "worklog")
	if err := os.MkdirAll(worklog, 0o700); err != nil {
		t.Fatal(err)
	}
	private := map[string]string{
		filepath.Join(workspace, "soul.md"):       "# Private Soul\n\nuser-soul-sentinel\n",
		filepath.Join(workspace, "profile.md"):    "# Private Profile\n\nuser-profile-sentinel\n",
		filepath.Join(workspace, "memory.md"):     "# Private Memory\n\nuser-memory-sentinel\n",
		filepath.Join(workspace, "current.md"):    "# Private Current\n\nuser-current-sentinel\n",
		filepath.Join(worklog, "private-task.md"): "# Private Work\n\nuser-worklog-sentinel\n",
		filepath.Join(workspace, "AGENTS.md"):     "# User Brain Rules\n\nuser-agents-sentinel\n",
	}
	for path, body := range private {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range private {
		got := string(mustReadFile(t, path))
		if path == filepath.Join(workspace, "AGENTS.md") {
			if !strings.Contains(got, body) || !strings.Contains(got, productWorkspaceInstructions) {
				t.Fatalf("managed repair did not preserve user AGENTS.md and append shipped policy:\n%s", got)
			}
			continue
		}
		if got != body {
			t.Fatalf("private overlay %s changed:\n%s", path, got)
		}
	}

	for _, contract := range []string{
		"Brain owns conversation",
		"visible Mewla Worker",
		"Check each result against its acceptance criteria",
		"with no separate resolve",
		"Brain decides decomposition",
		"User instructions override skill guidelines",
	} {
		if !strings.Contains(productWorkspaceInstructions, contract) && !strings.Contains(productDelegationPolicy, contract) {
			t.Fatalf("shipped templates missing autonomous contract %q", contract)
		}
	}
	for _, privateFact := range []string{
		"user-soul-sentinel", "user-profile-sentinel", "user-memory-sentinel", "user-current-sentinel", "user-worklog-sentinel",
		"52466569-0fbc-4623-8fba-d754591a2f83", "49dc23f4-8f89-44b7-aa5b-e740173aad68", "OpenList",
	} {
		for name, template := range map[string]string{
			"AGENTS.md": productWorkspaceInstructions, "delegation.md": productDelegationPolicy,
			"engine.md": productEnginePolicy, "handoff.md": productHandoffPolicy,
		} {
			if strings.Contains(template, privateFact) {
				t.Fatalf("shipped %s contains private fact %q", name, privateFact)
			}
		}
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	for path, body := range private {
		if path == filepath.Join(workspace, "AGENTS.md") {
			continue
		}
		if got := string(mustReadFile(t, path)); got != body {
			t.Fatalf("second repair changed private overlay %s:\n%s", path, got)
		}
	}
	_ = store
}

func TestNewStorePreservesUnmarkedDocumentAndAppendsCanonicalBlock(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	custom := []byte("# Existing Workspace Notes\n\nKeep every old and user-authored byte.  \n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	spec := store.managedMarkdownSpecs()[0]
	want := appendManagedBlock(custom, canonicalManagedBlock(spec))
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("unmarked AGENTS.md was not preserved\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestNewStoreUpgradesMarkedBlockInPlaceAndSecondRunIsExact(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	prefix := []byte("# User Before\n\n")
	suffix := []byte("\n\n# User After\n\nKeep this exact suffix.  \n")
	oldBlock := []byte(managedStartMarker(brainWorkersManagedID) + "\n# Old Product Block\n" + managedEndMarker(brainWorkersManagedID))
	input := bytes.Join([][]byte{prefix, oldBlock, suffix}, nil)
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	spec := store.managedMarkdownSpecs()[0]
	want := bytes.Join([][]byte{prefix, canonicalManagedBlock(spec), suffix}, nil)
	first := mustReadFile(t, path)
	if !bytes.Equal(first, want) {
		t.Fatalf("marked upgrade changed outer bytes\ngot:\n%s\nwant:\n%s", first, want)
	}
	assertFileMode(t, path, 0o600)

	fixed := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, first) {
		t.Fatal("second NewStore changed canonical AGENTS.md bytes")
	}
	assertFileMtime(t, path, fixed)
}

func TestNewStoreConsolidatesMarkedBlocksAndPreservesAllExteriorBytes(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	oldBlock := []byte(managedStartMarker(brainWorkersManagedID) + "\nold\n" + managedEndMarker(brainWorkersManagedID))
	prefix := []byte("USER BEFORE\n")
	between := []byte("\nUSER BETWEEN BLOCKS\n")
	suffix := []byte("\nUSER AFTER\n")
	input := bytes.Join([][]byte{prefix, oldBlock, between, oldBlock, suffix}, nil)
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Join([][]byte{prefix, canonicalManagedBlock(store.managedMarkdownSpecs()[0]), between, suffix}, nil)
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("consolidation changed exterior bytes\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestManagedMarkerValidationRejectsCorruptionAndForeignIDs(t *testing.T) {
	tests := map[string]string{
		"missing end": managedStartMarker(brainWorkersManagedID) + "\nbody\n",
		"stray end":   managedEndMarker(brainWorkersManagedID) + "\n",
		"nested": managedStartMarker(brainWorkersManagedID) + "\n" +
			managedStartMarker(brainWorkersManagedID) + "\n" +
			managedEndMarker(brainWorkersManagedID) + "\n",
		"malformed line":  managedStartMarker(brainWorkersManagedID) + " trailing text\n",
		"foreign id":      managedStartMarker(handoffManagedID) + "\nforeign\n" + managedEndMarker(handoffManagedID) + "\n",
		"embedded prefix": "User prose mentions " + managedMarkerPrefix + "agents:start --> inline.\n",
	}
	for name, malformed := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "AGENTS.md")
			original := []byte(malformed)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(root); err == nil {
				t.Fatal("NewStore accepted an invalid managed marker")
			}
			if got := mustReadFile(t, path); !bytes.Equal(got, original) {
				t.Fatalf("invalid document changed:\n%s", got)
			}
		})
	}
}

func TestManagedWorkspaceLateFailureNeverWritesEarlierDocument(t *testing.T) {
	t.Run("construction", func(t *testing.T) {
		root, agentsPath, handoffPath, original, fixed := lateFailureFixture(t)
		if _, err := NewStore(root); err == nil {
			t.Fatal("NewStore accepted a foreign handoff marker")
		}
		assertBytesAndMtime(t, agentsPath, original, fixed)
		if !bytes.Contains(mustReadFile(t, handoffPath), []byte(managedStartMarker(brainWorkersManagedID))) {
			t.Fatal("foreign handoff marker unexpectedly changed")
		}
	})

	t.Run("housekeeping", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		agentsPath := store.workspaceInstructionsPath()
		original := staleManagedWorkers()
		if err := os.WriteFile(agentsPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		foreign := []byte(managedStartMarker(brainWorkersManagedID) + "\nforeign\n" + managedEndMarker(brainWorkersManagedID) + "\n")
		if err := os.WriteFile(store.policyPath("handoff.md"), foreign, 0o600); err != nil {
			t.Fatal(err)
		}
		fixed := time.Date(2002, 3, 4, 5, 6, 7, 0, time.UTC)
		if err := os.Chtimes(agentsPath, fixed, fixed); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(store, nil, nil).Housekeeping(); err == nil {
			t.Fatal("Housekeeping accepted a foreign handoff marker")
		}
		assertBytesAndMtime(t, agentsPath, original, fixed)
	})
}

func TestNewStorePreservesEveryExistingNonEmptyProfile(t *testing.T) {
	profiles := map[string][]byte{
		"minimal heading":  []byte("# Brain Profile\n\n"),
		"whitespace":       []byte(" \n\t\n"),
		"old looking text": []byte("# Brain Profile\n\n## Voice\n\nKnown working preferences: keep this.  \n"),
	}
	for name, profile := range profiles {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "profile.md")
			if err := os.WriteFile(path, profile, 0o600); err != nil {
				t.Fatal(err)
			}
			fixed := time.Date(2003, 4, 5, 6, 7, 8, 0, time.UTC)
			if err := os.Chtimes(path, fixed, fixed); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(root); err != nil {
				t.Fatal(err)
			}
			assertBytesAndMtime(t, path, profile, fixed)
		})
	}
}

func TestNewStoreInitializesExactlyEmptyProfile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "profile.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, path); string(got) != defaultProfileNotes {
		t.Fatalf("empty profile = %q, want %q", got, defaultProfileNotes)
	}
}

func TestNewStorePreservesEveryExistingNonEmptySoul(t *testing.T) {
	souls := map[string][]byte{
		"minimal heading":  []byte("# Brain Soul\n\n"),
		"whitespace":       []byte(" \n\t\n"),
		"old looking text": []byte("# Brain Soul\n\nKnown principles: keep this.  \n"),
	}
	for name, soul := range souls {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "soul.md")
			if err := os.WriteFile(path, soul, 0o600); err != nil {
				t.Fatal(err)
			}
			fixed := time.Date(2005, 5, 6, 7, 8, 9, 0, time.UTC)
			if err := os.Chtimes(path, fixed, fixed); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(root); err != nil {
				t.Fatal(err)
			}
			assertBytesAndMtime(t, path, soul, fixed)
		})
	}
}

func TestHousekeepingReportsOnlySortedChangedPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.workspaceInstructionsPath(), []byte("custom AGENTS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.currentPath()); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)

	first, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AGENTS.md", "current.md"}
	if !equalStrings(first.ChangedPaths, want) {
		t.Fatalf("first changed paths = %v, want %v", first.ChangedPaths, want)
	}
	second, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ChangedPaths) != 0 {
		t.Fatalf("second changed paths = %v, want none", second.ChangedPaths)
	}
}

func TestRetiredShippedDefaultsAreDeletedAndEditedCopiesReported(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace := store.WorkspacePath()
	fixtures := map[string]string{
		"soul.md":                  "testdata/retired/soul.md",
		"playbooks/README.md":      "testdata/retired/playbooks-README.md",
		"playbooks/brain-flows.md": "testdata/retired/brain-flows.md",
		"current.md":               "testdata/retired/current.md",
		"worklog/README.md":        "testdata/retired/worklog-README.md",
	}
	for target, fixture := range fixtures {
		if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(target)), mustReadFile(t, fixture), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(store, nil, nil)
	report, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{"soul.md", "playbooks/README.md", "playbooks/brain-flows.md"} {
		if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(retired))); !os.IsNotExist(err) {
			t.Fatalf("shipped default %s was not deleted: %v", retired, err)
		}
	}
	if got := string(mustReadFile(t, store.currentPath())); got != defaultCurrentContext {
		t.Fatalf("legacy current.md seed not upgraded:\n%s", got)
	}
	if got := string(mustReadFile(t, store.worklogReadmePath())); got != defaultWorklogReadme {
		t.Fatalf("legacy worklog README not upgraded:\n%s", got)
	}
	want := []string{"current.md", "playbooks/README.md", "playbooks/brain-flows.md", "soul.md", "worklog/README.md"}
	if !equalStrings(report.ChangedPaths, want) {
		t.Fatalf("changed paths = %v, want %v", report.ChangedPaths, want)
	}

	private := []byte("# Brain Soul\n\nPRIVATE_SOUL_SENTINEL\n")
	soulPath := filepath.Join(workspace, "soul.md")
	if err := os.WriteFile(soulPath, private, 0o600); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2006, 7, 8, 9, 10, 11, 0, time.UTC)
	if err := os.Chtimes(soulPath, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, "downloads"), 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	assertBytesAndMtime(t, soulPath, private, fixed)
	if len(second.ChangedPaths) != 0 || !equalStrings(second.UnmanagedPaths, []string{"downloads/", "soul.md"}) {
		t.Fatalf("second report changed=%v unmanaged=%v", second.ChangedPaths, second.UnmanagedPaths)
	}
}

func TestContextReportsNoteBudgetsWithoutInliningPrivateNotes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	large := "# Current\n\nPRIVATE_CURRENT_SENTINEL\n" + strings.Repeat("finished item\n", 2000)
	if err := os.WriteFile(store.currentPath(), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	context, err := service.Context()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE_CURRENT_SENTINEL") || strings.Contains(string(raw), "Brain Memory") {
		t.Fatalf("context inlined private notes:\n%s", raw)
	}
	if len(raw) > 4096 {
		t.Fatalf("idle context is %d bytes", len(raw))
	}
	var current WorkspaceNote
	for _, note := range context.Notes {
		if note.Path == "current.md" {
			current = note
		}
	}
	if current.Bytes != int64(len(large)) || !current.OverBudget {
		t.Fatalf("current note = %+v", current)
	}
	report, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(report.RecommendedNextSteps, "\n"), "Compact current.md") {
		t.Fatalf("over-budget current.md not flagged: %v", report.RecommendedNextSteps)
	}
}

func lateFailureFixture(t *testing.T) (root, agentsPath, handoffPath string, workers []byte, fixed time.Time) {
	t.Helper()
	root = t.TempDir()
	policies := filepath.Join(root, "workspace", "policies")
	if err := os.MkdirAll(policies, 0o700); err != nil {
		t.Fatal(err)
	}
	agentsPath = filepath.Join(root, "workspace", "AGENTS.md")
	handoffPath = filepath.Join(policies, "handoff.md")
	workers = staleManagedWorkers()
	if err := os.WriteFile(agentsPath, workers, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(managedStartMarker(brainWorkersManagedID) + "\nforeign\n" + managedEndMarker(brainWorkersManagedID) + "\n")
	if err := os.WriteFile(handoffPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixed = time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(agentsPath, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	return root, agentsPath, handoffPath, workers, fixed
}

func staleManagedWorkers() []byte {
	return []byte(managedStartMarker(brainWorkersManagedID) + "\nold product bytes\n" + managedEndMarker(brainWorkersManagedID) + "\n")
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func assertBytesAndMtime(t *testing.T, path string, want []byte, mtime time.Time) {
	t.Helper()
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("%s bytes changed\ngot:\n%s\nwant:\n%s", path, got, want)
	}
	assertFileMtime(t, path, mtime)
}

func assertFileMtime(t *testing.T, path string, want time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(want) {
		t.Fatalf("%s mtime = %s, want %s", path, info.ModTime(), want)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
