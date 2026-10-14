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
	ConfigVersion  = 1
	configFileName = "remote.json"
	statusFileName = "remote-status.json"
	certFileName   = "remote-cert.pem"
	acmeKeyFile    = "remote-acme-account.pem"
)

// DefaultServiceURL is the hosted service a plain `mewla login` uses. Release
// builds set it with -ldflags "-X github.com/daoleno/mewla/daemon/remote.DefaultServiceURL=…";
// MEWLA_REMOTE_SERVICE_URL overrides it.
var DefaultServiceURL = ""

// ServiceURL returns the service a new login should use, or "" when this
// build has none.
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
// this computer is signed in.
type Config struct {
	Version    int    `json:"version"`
	ServiceURL string `json:"service_url"`
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

// LoadConfig returns the saved sign-in, or ok=false when signed out.
func LoadConfig(stateDir string) (Config, bool, error) {
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
	if config.Version != ConfigVersion {
		return Config{}, false, fmt.Errorf("unsupported remote access config version %d", config.Version)
	}
	if err := config.validate(); err != nil {
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

// RemoveSignIn deletes everything this computer keeps for remote access.
func RemoveSignIn(stateDir string) error {
	var first error
	for _, name := range []string{configFileName, statusFileName, certFileName, acmeKeyFile} {
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
