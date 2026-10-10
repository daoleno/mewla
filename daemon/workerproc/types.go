package workerproc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

var resourceIDRE = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

type Lease struct {
	Observed   []ProcessIdentity `json:"observed,omitempty"`
	Version    int               `json:"version"`
	ResourceID string            `json:"resource_id"`
	BootID     string            `json:"boot_id"`
	StartedAt  time.Time         `json:"started_at"`
}

func LeasePath(dir, resourceID string) (string, error) {
	dir = strings.TrimSpace(dir)
	resourceID = strings.TrimSpace(resourceID)
	if dir == "" {
		return "", fmt.Errorf("lease directory is required")
	}
	if !resourceIDRE.MatchString(resourceID) || strings.Contains(resourceID, "..") {
		return "", fmt.Errorf("invalid resource id %q", resourceID)
	}
	return filepath.Join(dir, resourceID+".json"), nil
}

func ReadLease(path string) (Lease, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Lease{}, err
	}
	var lease Lease
	if err := json.Unmarshal(raw, &lease); err != nil {
		return Lease{}, fmt.Errorf("decode lease: %w", err)
	}
	if lease.Version != 1 || !resourceIDRE.MatchString(lease.ResourceID) {
		return Lease{}, fmt.Errorf("invalid lease metadata")
	}
	return lease, nil
}

func ListLeases(dir string) ([]Lease, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	leases := make([]Lease, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		lease, err := ReadLease(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func writeLease(path string, lease Lease) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(raw, '\n'), 0o600)
}

// WriteOwnershipLease persists only identity, never limits. Old version-1
// supervisor leases remain readable; obsolete budget fields are ignored.
func WriteOwnershipLease(path, resourceID string) error {
	return writeLease(path, Lease{Version: 1, ResourceID: resourceID, BootID: bootID(), StartedAt: time.Now().UTC()})
}
