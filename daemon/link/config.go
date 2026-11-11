package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ConfigVersion = 1
	// DefaultConfigFilename is the operator config in the state directory.
	DefaultConfigFilename = "relay.json"
	// LegacyConfigFilename is its name before Mewla Relay was called that.
	LegacyConfigFilename = "link.json"
)

// LegacyConfigMove says what MoveLegacyConfig found.
type LegacyConfigMove int

const (
	// LegacyConfigAbsent: there is no legacy file; nothing to do.
	LegacyConfigAbsent LegacyConfigMove = iota
	// LegacyConfigMoved: the legacy file was renamed to the default name.
	LegacyConfigMoved
	// LegacyConfigIgnored: both files exist; the default one is used and the
	// legacy one is left for the operator to remove.
	LegacyConfigIgnored
)

// MoveLegacyConfig renames <stateDir>/link.json to <stateDir>/relay.json when
// only the legacy file exists. It is a one-time move, not a second read path:
// LoadConnectorConfig only ever reads the default name. The rename keeps the
// file's mode (0600).
func MoveLegacyConfig(stateDir string) (LegacyConfigMove, error) {
	dir := strings.TrimSpace(stateDir)
	if dir == "" {
		return LegacyConfigAbsent, nil
	}
	legacy := filepath.Join(dir, LegacyConfigFilename)
	current := filepath.Join(dir, DefaultConfigFilename)
	if _, err := os.Lstat(legacy); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LegacyConfigAbsent, nil
		}
		return LegacyConfigAbsent, err
	}
	if _, err := os.Lstat(current); err == nil {
		return LegacyConfigIgnored, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return LegacyConfigAbsent, err
	}
	if err := os.Rename(legacy, current); err != nil {
		return LegacyConfigAbsent, fmt.Errorf("move %s to %s: %w", legacy, current, err)
	}
	return LegacyConfigMoved, nil
}

type fileConfig struct {
	Version           int         `json:"version"`
	ConnectorToken    string      `json:"connector_token,omitempty"`
	ConnectorTokenEnv string      `json:"connector_token_env,omitempty"`
	Relays            []fileRelay `json:"relays"`
	MaxStreams        int         `json:"max_streams,omitempty"`
}

type fileRelay struct {
	Name              string `json:"name"`
	ControlAddress    string `json:"control_address"`
	ControlServerName string `json:"control_server_name"`
	ControlCAFile     string `json:"control_ca_file,omitempty"`
	ClientDomain      string `json:"client_domain"`
	ClientPort        int    `json:"client_port,omitempty"`
}

func DefaultConfigPath(stateDir string) string {
	return filepath.Join(strings.TrimSpace(stateDir), DefaultConfigFilename)
}

func LoadConnectorConfig(path string) (ConnectorConfig, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return ConnectorConfig{}, errors.New("Mewla Relay config path is required")
	}
	raw, err := os.ReadFile(normalizedPath)
	if err != nil {
		return ConnectorConfig{}, err
	}
	var stored fileConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return ConnectorConfig{}, fmt.Errorf("decode Mewla Relay config: %w", err)
	}
	if stored.Version != ConfigVersion {
		return ConnectorConfig{}, fmt.Errorf(
			"unsupported Mewla Relay config version %d (expected %d)",
			stored.Version,
			ConfigVersion,
		)
	}
	token := strings.TrimSpace(stored.ConnectorToken)
	if envName := strings.TrimSpace(stored.ConnectorTokenEnv); envName != "" {
		if token != "" {
			return ConnectorConfig{}, errors.New(
				"Mewla Relay config must use connector_token or connector_token_env, not both",
			)
		}
		token = strings.TrimSpace(os.Getenv(envName))
		if token == "" {
			return ConnectorConfig{}, fmt.Errorf(
				"Mewla Relay connector token environment variable %s is empty",
				envName,
			)
		}
	}
	baseDir := filepath.Dir(normalizedPath)
	candidates := make([]RelayCandidate, 0, len(stored.Relays))
	for _, relay := range stored.Relays {
		caFile := strings.TrimSpace(relay.ControlCAFile)
		if caFile != "" && !filepath.IsAbs(caFile) {
			caFile = filepath.Join(baseDir, caFile)
		}
		candidates = append(candidates, RelayCandidate{
			Name:              relay.Name,
			ControlAddress:    relay.ControlAddress,
			ControlServerName: relay.ControlServerName,
			ControlCAFile:     caFile,
			ClientDomain:      relay.ClientDomain,
			ClientPort:        relay.ClientPort,
		})
	}
	return normalizeConnectorConfig(ConnectorConfig{
		ConnectorToken: token,
		Candidates:     candidates,
		MaxStreams:     stored.MaxStreams,
	})
}

func RelayDomains(config ConnectorConfig) []string {
	domains := make([]string, 0, len(config.Candidates))
	for _, candidate := range config.Candidates {
		if domain := strings.TrimSpace(candidate.ClientDomain); domain != "" {
			domains = append(domains, domain)
		}
	}
	return domains
}
