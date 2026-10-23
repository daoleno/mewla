package brain

import (
	"os"
	"path/filepath"
	"testing"
)

func readSeed(t *testing.T, store *Store, relativePath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(store.WorkspacePath(), filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeSeed(t *testing.T, store *Store, relativePath, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(store.WorkspacePath(), filepath.FromSlash(relativePath)), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// withSeedContent ships a new release of one playbook seed for the test.
func withSeedContent(t *testing.T, name, content string) {
	t.Helper()
	for index := range seedPlaybooks {
		if seedPlaybooks[index].name == name {
			previous := seedPlaybooks[index].initial
			seedPlaybooks[index].initial = content
			t.Cleanup(func() { seedPlaybooks[index].initial = previous })
			return
		}
	}
	t.Fatalf("no seed playbook %q", name)
}

func TestFirstRunSeedsEveryFileAndARerunChangesNothing(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range workspaceSeeds() {
		if got := readSeed(t, store, seed.path); got != seed.content {
			t.Fatalf("%s was not seeded:\n%s", seed.path, got)
		}
	}
	manifest, err := os.ReadFile(store.seedManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.seedManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(store.seedManifestPath())
	afterInfo, _ := os.Stat(store.seedManifestPath())
	if string(again) != string(manifest) || !afterInfo.ModTime().Equal(info.ModTime()) {
		t.Fatalf("a rerun rewrote the manifest:\n%s\n%s", manifest, again)
	}
}

func TestANewSeedReplacesAnUntouchedCopyAndKeepsEditedOnes(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSeed(t, store, "playbooks/wayfind.md", "my own wayfind\n")
	withSeedContent(t, "delegate-brief.md", "---\ndescription: v2\n---\n\n# Delegate Brief v2\n")
	withSeedContent(t, "wayfind.md", "---\ndescription: v2\n---\n\n# Wayfind v2\n")

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := readSeed(t, store, "playbooks/delegate-brief.md"); got != "---\ndescription: v2\n---\n\n# Delegate Brief v2\n" {
		t.Fatalf("untouched seed was not upgraded:\n%s", got)
	}
	if got := readSeed(t, store, "playbooks/wayfind.md"); got != "my own wayfind\n" {
		t.Fatalf("edited seed was overwritten:\n%s", got)
	}
	// Edited stays edited on every later release too.
	withSeedContent(t, "wayfind.md", "v3\n")
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := readSeed(t, store, "playbooks/wayfind.md"); got != "my own wayfind\n" {
		t.Fatalf("edited seed was overwritten by a later release:\n%s", got)
	}
}

func TestExistingFilesWithoutAManifestAreTheUsersUnlessTheyMatchTheSeed(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.seedManifestPath()); err != nil {
		t.Fatal(err)
	}
	writeSeed(t, store, "playbooks/slice-work.md", "an older slice-work seed or my edit\n")
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := readSeed(t, store, "playbooks/slice-work.md"); got != "an older slice-work seed or my edit\n" {
		t.Fatalf("a file nobody can prove untouched was replaced:\n%s", got)
	}
	// The files that already matched the seed are tracked again.
	withSeedContent(t, "wayfind.md", "v2\n")
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := readSeed(t, store, "playbooks/wayfind.md"); got != "v2\n" {
		t.Fatalf("a seed copy was not tracked after the manifest vanished:\n%s", got)
	}
}

func TestBlankAndMissingSeedFilesAreReseededAndSymlinksAreLeftAlone(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	writeSeed(t, store, "current.md", " \n")
	if err := os.Remove(filepath.Join(store.WorkspacePath(), "playbooks", "wayfind.md")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "elsewhere.md")
	if err := os.WriteFile(target, []byte("linked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(store.WorkspacePath(), "playbooks", "slice-work.md")
	if err := os.Remove(linked); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := readSeed(t, store, "current.md"); got != defaultCurrentContext {
		t.Fatalf("blank current.md was not reseeded: %q", got)
	}
	if got := readSeed(t, store, "playbooks/wayfind.md"); got != defaultWayfindPlaybook {
		t.Fatalf("missing playbook was not reseeded: %q", got)
	}
	if got, _ := os.ReadFile(target); string(got) != "linked\n" {
		t.Fatalf("symlinked playbook target was written: %q", got)
	}
}

func TestAnUnreadableManifestStopsUpgradesButNotSeeding(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.seedManifestPath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	withSeedContent(t, "wayfind.md", "v2\n")
	if _, err := NewStore(root); err != nil {
		t.Fatalf("a broken manifest must not stop the store: %v", err)
	}
	if got := readSeed(t, store, "playbooks/wayfind.md"); got != defaultWayfindPlaybook {
		t.Fatalf("seed changed without a manifest to prove it untouched: %q", got)
	}
}
