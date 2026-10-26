// Package remote connects this computer to the hosted remote access service:
// account login, the relay route grant, the hostname certificate, and the
// relay connector that carries phones and browsers to the daemon.
package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daoleno/mewla/daemon/atomicfile"
)

const (
	ConfigVersion  = 2
	configFileName = "remote.json"
	statusFileName = "remote-status.json"
	certFileName   = "remote-cert.pem"
	acmeKeyFile    = "remote-acme-account.pem"
)

// DefaultServiceURL is the hosted service a plain `mewla login` uses, the same
// for release and dev builds. MEWLA_REMOTE_SERVICE_URL overrides it (isolated
// tests, -dev-fake-login setups).
const DefaultServiceURL = "https://connect.mewla.ai"

// ServiceURL returns the service a new login should use.
func ServiceURL() string {
	if value := strings.TrimSpace(os.Getenv("MEWLA_REMOTE_SERVICE_URL")); value != "" {
		return strings.TrimSuffix(value, "/")
	}
	return strings.TrimSuffix(strings.TrimSpace(DefaultServiceURL), "/")
}

type Relay struct {
	Name              string `json:"name"`
	ControlAddress    string `json:"control_address"`
	ControlServerName string `json:"control_server_name"`
	ControlCAPEM      string `json:"control_ca_pem,omitempty"`
	ClientDomain      string `json:"client_domain"`
	ClientPort        int    `json:"client_port,omitempty"`
}

type ACME struct {
	DirectoryURL string `json:"directory_url"`
	CAPEM        string `json:"ca_pem,omitempty"`
}

// Machine is the service's MachineInfo for this computer.
type Machine struct {
	MachineID        string  `json:"machine_id"`
	AccountID        string  `json:"account_id"`
	AccountLogin     string  `json:"account_login"`
	Provider         string  `json:"provider"`
	Hostname         string  `json:"hostname"`
	Label            string  `json:"label"`
	RouteID          string  `json:"route_id"`
	Grant            string  `json:"grant"`
	GrantExpiresAtMS int64   `json:"grant_expires_at_ms"`
	Relays           []Relay `json:"relays"`
	ACME             ACME    `json:"acme"`
}

// Config is remote.json in the daemon state directory. It exists only while
// this computer is signed in to a Mewla account, and holds that sign-in. Friends
// needs only the sign-in; remote access (the relay connection, the certificate
// and the public address) runs only while RemoteAccess is true, which only
// `mewla login` turns on.
type Config struct {
	Version    int    `json:"version"`
	ServiceURL string `json:"service_url"`
	// RemoteAccess is whether this computer is reachable through the relay.
	RemoteAccess bool `json:"remote_access"`
	// ServiceCAPEM is an extra root for the service's HTTPS, used by local
	// development deployments only.
	ServiceCAPEM string  `json:"service_ca_pem,omitempty"`
	Machine      Machine `json:"machine"`
}

// URL is the address phones and browsers use for this computer.
func (config Config) URL() string {
	host := config.Machine.Hostname
	if len(config.Machine.Relays) > 0 {
		if port := config.Machine.Relays[0].ClientPort; port > 0 && port != 443 {
			host = net.JoinHostPort(host, strconv.Itoa(port))
		}
	}
	return "https://" + host
}

func ConfigPath(stateDir string) string {
	return filepath.Join(stateDir, configFileName)
}

// LoadAccount returns the saved Mewla account sign-in, whether or not remote
// access is on, or ok=false when this computer is signed out. Friends and
// everything else that only needs the account use it.
func LoadAccount(stateDir string) (Config, bool, error) {
	raw, err := os.ReadFile(ConfigPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, false, fmt.Errorf("decode remote access config: %w", err)
	}
	switch config.Version {
	case ConfigVersion:
	case 1:
		// Version 1 had no Friends-only sign-in: a saved sign-in was `mewla
		// login`, which turned remote access on. The next save writes version 2.
		config.RemoteAccess = true
	default:
		return Config{}, false, fmt.Errorf("unsupported remote access config version %d", config.Version)
	}
	if err := config.validate(); err != nil {
		return Config{}, false, err
	}
	return config, true, nil
}

// LoadRemote returns the sign-in only while remote access is on. The relay
// connection, the public address and the pairing links use it.
func LoadRemote(stateDir string) (Config, bool, error) {
	config, ok, err := LoadAccount(stateDir)
	if err != nil || !ok || !config.RemoteAccess {
		return Config{}, false, err
	}
	return config, true, nil
}

func SaveConfig(stateDir string, config Config) error {
	config.Version = ConfigVersion
	if err := config.validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(ConfigPath(stateDir), raw, 0o600)
}

// StopRemote turns remote access off and keeps the account sign-in. It forgets
// the hostname certificate, which belongs to the address that goes away.
func StopRemote(stateDir string) error {
	config, ok, err := LoadAccount(stateDir)
	if err != nil || !ok {
		return err
	}
	config.RemoteAccess = false
	if err := SaveConfig(stateDir, config); err != nil {
		return err
	}
	return removeFiles(stateDir, statusFileName, certFileName, acmeKeyFile)
}

// RemoveSignIn deletes everything this computer keeps for its Mewla account
// and remote access.
func RemoveSignIn(stateDir string) error {
	return removeFiles(stateDir, configFileName, statusFileName, certFileName, acmeKeyFile)
}

func removeFiles(stateDir string, names ...string) error {
	var first error
	for _, name := range names {
		if err := os.Remove(filepath.Join(stateDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

func (config Config) validate() error {
	machine := config.Machine
	switch {
	case strings.TrimSpace(config.ServiceURL) == "":
		return errors.New("remote access config has no service")
	case machine.Hostname == "" || machine.Label == "" || machine.RouteID == "":
		return errors.New("remote access config has no machine address")
	case len(machine.Relays) == 0:
		return errors.New("remote access config has no relay")
	}
	return nil
}
