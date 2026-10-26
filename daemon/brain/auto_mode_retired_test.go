package brain

import (
	"strings"
	"testing"
)

const legacyPermissionBlockPlaybook = "---\ndescription: Answer a delegated Worker's blocked action: approve what its brief authorizes, escalate the rest.\n---\n\n# Permission Block\n\nA Worker's session.needs_input whose details say \"event\":\"permission_blocked\" names the action Auto mode refused.\n"

func TestAutoModeGuidanceIsGoneFromTheShippedTemplatesAndReachesExistingHomes(t *testing.T) {
	for name, text := range map[string]string{
		"delegation policy":      productDelegationPolicy,
		"permission-block seed":  defaultPermissionBlockPlaybook,
		"workspace instructions": productWorkspaceInstructions,
	} {
		for _, stale := range []string{"Auto mode", "auto mode", "classifier", "refused action"} {
			if strings.Contains(text, stale) {
				t.Fatalf("%s still mentions %q", name, stale)
			}
		}
	}

	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	// A home written by the previous release: the old managed delegation block
	// and the old playbook, recorded in the seed manifest as untouched.
	var delegation managedMarkdownSpec
	for _, spec := range store.managedMarkdownSpecs() {
		if spec.managedID == delegationManagedID {
			delegation = spec
		}
	}
	oldBlock := managedStartMarker(delegationManagedID) + "\n- A Worker whose action Auto mode refused raises session.needs_input.\n" + managedEndMarker(delegationManagedID)
	writeSeed(t, store, delegation.relativePath, "my own notes\n\n"+oldBlock+"\n")
	writeSeed(t, store, "playbooks/permission-block.md", legacyPermissionBlockPlaybook)
	manifest := store.readSeedManifest()
	manifest.Files["playbooks/permission-block.md"] = sha256Hex([]byte(legacyPermissionBlockPlaybook))
	if err := writeJSONFile(store.seedManifestPath(), manifest); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	policy := readSeed(t, store, delegation.relativePath)
	if strings.Contains(policy, "Auto mode") || !strings.HasPrefix(policy, "my own notes\n\n") || !strings.Contains(policy, "permission_blocked") {
		t.Fatalf("managed delegation block was not reconciled around the user's text:\n%s", policy)
	}
	if got := readSeed(t, store, "playbooks/permission-block.md"); got != defaultPermissionBlockPlaybook {
		t.Fatalf("untouched playbook was not upgraded:\n%s", got)
	}
}
