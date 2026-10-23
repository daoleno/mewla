package brain

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// Seeded workspace files are written once, and an older seed that nobody has
// touched since follows a new release. The state directory remembers the
// digest of what Mewla last wrote to each file; a file whose digest still
// matches is untouched and may be replaced, anything else belongs to the user.
// There is no table of past seed digests to grow.
const seedManifestName = "workspace-seeds.json"

type workspaceSeed struct {
	path    string // relative to the workspace, slash separated
	content string
}

func workspaceSeeds() []workspaceSeed {
	seeds := []workspaceSeed{
		{"current.md", defaultCurrentContext},
		{routingGuideName, defaultRoutingGuide},
		{"worklog/README.md", defaultWorklogReadme},
	}
	for _, playbook := range seedPlaybooks {
		seeds = append(seeds, workspaceSeed{playbooksDirName + "/" + playbook.name, playbook.initial})
	}
	return seeds
}

type seedManifest struct {
	Files map[string]string `json:"files"`
}

func (s *Store) seedManifestPath() string {
	return filepath.Join(s.statePath(), seedManifestName)
}

func (s *Store) readSeedManifest() seedManifest {
	manifest := seedManifest{Files: map[string]string{}}
	raw, exists, err := readOptionalFile(s.seedManifestPath())
	if err != nil || !exists {
		if err != nil {
			log.Printf("brain: read %s: %v; seeded files will not be upgraded this run", seedManifestName, err)
		}
		return manifest
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Files == nil {
		log.Printf("brain: %s is unreadable (%v); seeded files will not be upgraded this run", seedManifestName, err)
		manifest.Files = map[string]string{}
	}
	return manifest
}

// seedWorkspaceFiles creates every missing or blank seeded file and upgrades
// the ones still holding an earlier seed byte for byte.
func (s *Store) seedWorkspaceFiles() error {
	manifest := s.readSeedManifest()
	before := maps.Clone(manifest.Files)
	for _, seed := range workspaceSeeds() {
		path := filepath.Join(s.WorkspacePath(), filepath.FromSlash(seed.path))
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		raw, exists, err := readOptionalFile(path)
		if err != nil {
			return fmt.Errorf("read Brain workspace seed %s: %w", seed.path, err)
		}
		seedDigest := sha256Hex([]byte(seed.content))
		switch {
		case !exists || strings.TrimSpace(string(raw)) == "":
		case sha256Hex(raw) == seedDigest:
			manifest.Files[seed.path] = seedDigest
			continue
		case manifest.Files[seed.path] == sha256Hex(raw):
		default:
			continue
		}
		if err := writeAtomic(path, []byte(seed.content), 0o600); err != nil {
			return err
		}
		manifest.Files[seed.path] = seedDigest
	}
	if maps.Equal(manifest.Files, before) {
		return nil
	}
	return writeJSONFile(s.seedManifestPath(), manifest)
}

func sha256Hex(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
