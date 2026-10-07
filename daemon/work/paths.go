package work

import (
	"os"
	"path/filepath"

	"github.com/daoleno/mewla/daemon/statedir"
)

// DefaultRoot returns ~/.mewla/work.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "work"), nil
}

// DefaultWorktreeRoot returns the durable, Mewla-managed location agents should
// use when concurrent write isolation genuinely requires a git worktree.
// It intentionally lives under the user's home directory rather than /tmp,
// which is commonly memory-backed and may be cleared across restarts.
func DefaultWorktreeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "worktrees"), nil
}

// DefaultExecutorsPath returns ~/.mewla/executors.toml.
func DefaultExecutorsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "executors.toml"), nil
}

// DefaultModelProfilesPath returns ~/.mewla/model-profiles.toml.
func DefaultModelProfilesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "model-profiles.toml"), nil
}

// DefaultProviderDiscoveryPath returns ~/.mewla/provider-discovery.json.
// Secret-free TTL/LKG model id cache; live ids never authorize capabilities.
func DefaultProviderDiscoveryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "provider-discovery.json"), nil
}

// DefaultProviderCredentialsPath returns ~/.mewla/provider-credentials.json.
func DefaultProviderCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "provider-credentials.json"), nil
}

// DefaultRouteBindingsPath returns ~/.mewla/route-bindings.json.
// Stage 2B Session lifecycle owns Save/Load against this path; Stage 2A only
// defines the codec and RouteTable.Restore contract.
func DefaultRouteBindingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "route-bindings.json"), nil
}

// DefaultRouteListenerPath returns ~/.mewla/route-listener.json.
// Persists the loopback listen address so daemon restart rebinds the same port
// and surviving CLI Sessions keep working.
func DefaultRouteListenerPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(statedir.Default(home), "route-listener.json"), nil
}

// EnsureDir creates dir with mode 0o700 if it does not already exist.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}
