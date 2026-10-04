package brain

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Retired product files. Only byte-identical shipped defaults are removed;
// an edited copy belongs to the user and gc reports it as unmanaged.
var retiredWorkspaceDefaults = map[string][]string{
	"soul.md": {
		"232492deb92a5e5186ee1905e0921b714300b141949d169e1b8f678d293154a8",
		"c2b866be13290e45eed27eab647d60465aa03334ca4ec2ea6a1ea4f1a89414cc",
	},
	"playbooks/README.md": {"e3dedafb4c59b3fa8c6fa7a2a00814d0e8ba89922c49cf69f3ae64970da305ab"},
	// align folded into the AGENTS.md Role section.
	"playbooks/align.md": {
		"6fd71ab61bcc85cf89124f3402d9f2dd9dd5ed007e0302b4ffa78f61b2badf8e",
		"50613fbc28dddbba12088f1b9a9a6d28127012b3e85c368d5051a34c7fc1bfb6",
	},
}

// brain-flows.md shipped as a default seed plus a managed role block. It is
// retired when the text outside that block is still the trimmed seed.
const (
	brainFlowsManagedID       = "playbook-brain-flows-routing"
	retiredBrainFlowsPath     = "playbooks/brain-flows.md"
	retiredBrainFlowsSeedHash = "29d929596134f13c622593e5a2ce96614d370bb5313b0c090a0c07f6805ae9f8"
)

// Earlier seeds repeated policy or invited long records. Untouched copies are
// upgraded to the compact seed.
var legacySeedUpgrades = []struct {
	path    string
	digests []string
	seed    string
}{
	{"current.md", []string{"04fbbb41cebb49b27e97766eaf34b34bfa90201f6ee43c25fd5bef58d8a831ee"}, defaultCurrentContext},
	{"worklog/README.md", []string{"31a2b263b364e1543ea9999c74443f74e1e81195878122504c3b5debd9609a49"}, defaultWorklogReadme},
}

func (s *Store) retireLegacyWorkspaceDefaults() error {
	for relativePath, digests := range retiredWorkspaceDefaults {
		path := filepath.Join(s.WorkspacePath(), filepath.FromSlash(relativePath))
		raw, exists, err := readOptionalFile(path)
		if err != nil {
			return fmt.Errorf("read retired Brain workspace %s: %w", relativePath, err)
		}
		if exists && slices.Contains(digests, sha256Hex(raw)) {
			if err := removeIfExists(path); err != nil {
				return err
			}
		}
	}
	if err := s.retireBrainFlowsPlaybook(); err != nil {
		return err
	}
	for _, upgrade := range legacySeedUpgrades {
		path := filepath.Join(s.WorkspacePath(), filepath.FromSlash(upgrade.path))
		raw, exists, err := readOptionalFile(path)
		if err != nil {
			return err
		}
		if exists && slices.Contains(upgrade.digests, sha256Hex(raw)) {
			if err := writeAtomic(path, []byte(upgrade.seed), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) retireBrainFlowsPlaybook() error {
	path := filepath.Join(s.WorkspacePath(), filepath.FromSlash(retiredBrainFlowsPath))
	raw, exists, err := readOptionalFile(path)
	if err != nil || !exists {
		return err
	}
	spec := managedMarkdownSpec{relativePath: retiredBrainFlowsPath, managedID: brainFlowsManagedID}
	spans, marked, err := managedBlockSpans(raw, spec)
	if err != nil {
		// A corrupt marker means someone edited it; leave it for the user.
		return nil
	}
	outside := raw
	if marked {
		outside = nil
		cursor := 0
		for _, span := range spans {
			outside = append(outside, raw[cursor:span.start]...)
			cursor = span.end
		}
		outside = append(outside, raw[cursor:]...)
	}
	if sha256Hex([]byte(strings.TrimSpace(string(outside)))) == retiredBrainFlowsSeedHash {
		return removeIfExists(path)
	}
	if !marked {
		return nil
	}
	// Keep the user's routing notes but drop the retired product block, which
	// would otherwise freeze a stale copy of the role contract.
	return writeAtomic(path, append(bytes.TrimRight(outside, "\n"), '\n'), 0o600)
}

// productWorkspaceRoot lists the top-level entries Zen owns in the Brain
// workspace. Anything else visible at the root is user or tool clutter.
var productWorkspaceRoot = []string{"AGENTS.md", "current.md", "memory.md", "profile.md", routingGuideName, "policies", "playbooks", "worklog"}

func (s *Store) unmanagedWorkspaceEntries() ([]string, error) {
	entries, err := os.ReadDir(s.WorkspacePath())
	if err != nil {
		return nil, err
	}
	unmanaged := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || slices.Contains(productWorkspaceRoot, name) {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		unmanaged = append(unmanaged, name)
	}
	sort.Strings(unmanaged)
	return unmanaged, nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func sha256Hex(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// Private notes are read on demand. Budgets keep the always-loaded handoff
// small; memory and profile hold durable facts, not history.
var workspaceNoteBudgets = []struct {
	path   string
	budget int64
}{
	{"current.md", 16 << 10},
	{"memory.md", 32 << 10},
	{"profile.md", 8 << 10},
	{routingGuideName, 4 << 10},
}

func (s *Store) WorkspaceNotes() ([]WorkspaceNote, error) {
	notes := make([]WorkspaceNote, 0, len(workspaceNoteBudgets))
	for _, spec := range workspaceNoteBudgets {
		var size int64
		info, err := os.Stat(filepath.Join(s.WorkspacePath(), spec.path))
		switch {
		case err == nil:
			size = info.Size()
		case !errors.Is(err, os.ErrNotExist):
			return nil, err
		}
		notes = append(notes, WorkspaceNote{Path: spec.path, Bytes: size, BudgetBytes: spec.budget, OverBudget: size > spec.budget})
	}
	return notes, nil
}
