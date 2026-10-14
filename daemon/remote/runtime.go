package remote

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/addressbook"
	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/linkproto"
)

const (
	configPollInterval = 3 * time.Second
	certCheckInterval  = 12 * time.Hour
	certRetryInterval  = 5 * time.Minute
)

// Status is remote-status.json, written by the running daemon for
// `mewla doctor` and `mewla address list`.
type Status struct {
	Phase        string    `json:"phase"`
	URL          string    `json:"url"`
	Relay        string    `json:"relay,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	CertError    string    `json:"cert_error,omitempty"`
	CertNotAfter time.Time `json:"cert_not_after,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func LoadStatus(stateDir string) (Status, bool) {
	raw, err := os.ReadFile(filepath.Join(stateDir, statusFileName))
	if err != nil {
		return Status{}, false
	}
	var status Status
	if json.Unmarshal(raw, &status) != nil {
		return Status{}, false
	}
	return status, true
}

// Runtime follows remote.json: while it exists this computer stays connected
// to its relay with a fresh grant and certificate; when it disappears
// (mewla logout) the connection stops.
type Runtime struct {
	StateDir    string
	Auth        *auth.Manager
	Identity    *link.TransportIdentity
	Handler     http.Handler
	AddressBook *addressbook.Store
	Logf        func(format string, args ...any)
}

func (runtime *Runtime) Run(ctx context.Context) error {
	var current string
	var stop context.CancelFunc
	var done chan struct{}
	stopSession := func() {
		if stop != nil {
			stop()
			<-done
			stop = nil
		}
	}
	defer stopSession()
	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()
	for {
		config, ok, err := LoadConfig(runtime.StateDir)
		key := ""
		if err != nil {
			runtime.logf("Remote access is off: %v", err)
		} else if ok {
			key = sessionKey(config)
		}
		if key != current {
			stopSession()
			current = key
			if key == "" {
				_ = os.Remove(filepath.Join(runtime.StateDir, statusFileName))
			} else {
				sessionCtx, cancel := context.WithCancel(ctx)
				stop, done = cancel, make(chan struct{})
				go func(config Config) {
					defer close(done)
					runtime.runSession(sessionCtx, config)
				}(config)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sessionKey changes when the sign-in changes in a way that needs a new
// connection; grant refreshes rewrite remote.json but keep the key.
func sessionKey(config Config) string {
	raw, _ := json.Marshal(struct {
		Service string
		Machine string
		Host    string
		Relays  []Relay
		ACME    ACME
	}{config.ServiceURL, config.Machine.MachineID, config.Machine.Hostname, config.Machine.Relays, config.Machine.ACME})
	return string(raw)
}

func (runtime *Runtime) runSession(ctx context.Context, config Config) {
	status := &statusWriter{stateDir: runtime.StateDir, status: Status{Phase: "connecting", URL: config.URL()}}
	status.write()
	client, err := NewClient(config.ServiceURL, config.ServiceCAPEM, runtime.Auth)
	if err != nil {
		status.update(func(s *Status) { s.Phase, s.LastError = "offline", err.Error() })
		return
	}
	if runtime.AddressBook != nil {
		if _, err := runtime.AddressBook.Add(config.URL(), addressbook.SourceRemote); err != nil {
			runtime.logf("Remote access: could not record %s: %v", config.URL(), err)
		}
	}
	grants := &grantSource{stateDir: runtime.StateDir, client: client, config: config}
	key, ok := runtime.Identity.Certificate.PrivateKey.(crypto.Signer)
	if !ok {
		status.update(func(s *Status) { s.Phase, s.LastError = "offline", "transport key cannot sign certificates" })
		return
	}
	certs := NewCertManager(runtime.StateDir, key, client, config)
	status.update(func(s *Status) { s.CertNotAfter = certs.NotAfter() })

	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			err := certs.Ensure(ctx)
			status.update(func(s *Status) {
				s.CertNotAfter = certs.NotAfter()
				s.CertError = ""
				if err != nil && ctx.Err() == nil {
					s.CertError = err.Error()
				}
			})
			next := certCheckInterval
			if err != nil {
				if ctx.Err() == nil {
					runtime.logf("Remote access certificate for %s: %v", config.Machine.Hostname, err)
				}
				next = certRetryInterval
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(next):
			}
		}
	}()
	defer wait.Wait()

	connectorConfig, err := connectorConfigFor(config, grants.Grant)
	if err == nil {
		connectorConfig.TLSConfig = ServerTLSConfig(runtime.Identity.Certificate, config.Machine.Hostname, certs)
		connectorConfig.StateObserver = func(state link.ConnectorState) {
			state.LastError = UserText(state.LastError)
			status.update(func(s *Status) {
				s.Phase, s.Relay, s.LastError = state.Phase, state.Relay, state.LastError
			})
			switch {
			case state.Phase == "connected":
				runtime.logf("Remote access connected: %s", config.URL())
			case state.Phase == "offline" && state.LastError != "":
				runtime.logf("Remote access offline: %s", state.LastError)
			}
		}
		var connector *link.Connector
		connector, err = link.NewConnector(connectorConfig, runtime.Auth, runtime.Identity, runtime.Handler)
		if err == nil {
			err = connector.Run(ctx)
		}
	}
	if err != nil && ctx.Err() == nil {
		status.update(func(s *Status) { s.Phase, s.LastError = "offline", UserText(err.Error()) })
		runtime.logf("Remote access stopped: %v", err)
		<-ctx.Done()
	}
}

// ConnectorConfig builds the relay connector configuration for a sign-in.
// mewla pair uses it to request admissions from the same relays.
func ConnectorConfig(stateDir string, config Config, authManager *auth.Manager) (link.ConnectorConfig, error) {
	client, err := NewClient(config.ServiceURL, config.ServiceCAPEM, authManager)
	if err != nil {
		return link.ConnectorConfig{}, err
	}
	grants := &grantSource{stateDir: stateDir, client: client, config: config}
	return connectorConfigFor(config, grants.Grant)
}

func connectorConfigFor(config Config, grant link.GrantSource) (link.ConnectorConfig, error) {
	candidates := make([]link.RelayCandidate, 0, len(config.Machine.Relays))
	for _, relay := range config.Machine.Relays {
		candidate := link.RelayCandidate{
			Name:              relay.Name,
			ControlAddress:    relay.ControlAddress,
			ControlServerName: relay.ControlServerName,
			ClientDomain:      relay.ClientDomain,
			ClientPort:        relay.ClientPort,
		}
		if relay.ControlCAPEM != "" {
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM([]byte(relay.ControlCAPEM)) {
				return link.ConnectorConfig{}, fmt.Errorf("relay %s CA contains no certificates", relay.Name)
			}
			candidate.ControlRootCAs = roots
		}
		candidates = append(candidates, candidate)
	}
	return link.ConnectorConfig{
		Grant:      grant,
		HostLabel:  config.Machine.Label,
		Candidates: candidates,
	}, nil
}

// grantSource hands the connector a grant with at least half its lifetime
// left, refreshing it from the service and saving it to remote.json.
type grantSource struct {
	stateDir string
	client   *Client
	mu       sync.Mutex
	config   Config
}

func (source *grantSource) Grant(ctx context.Context) (string, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	machine := source.config.Machine
	// Hand out the saved grant while more than half its lifetime remains,
	// and fall back to it during a service outage while it is still valid.
	remaining := time.Duration(0)
	if issued, expires, ok := linkproto.RouteGrantTimes(machine.Grant); ok {
		remaining = time.Until(expires)
		if remaining > expires.Sub(issued)/2 {
			return machine.Grant, nil
		}
	}
	refreshed, err := source.client.Machine(ctx)
	if errors.Is(err, ErrSignedOut) {
		return "", err
	}
	if err != nil {
		if remaining > time.Minute {
			return machine.Grant, nil
		}
		return "", err
	}
	source.config.Machine = refreshed
	if current, ok, loadErr := LoadConfig(source.stateDir); loadErr == nil && ok &&
		current.Machine.MachineID == refreshed.MachineID {
		current.Machine = refreshed
		_ = SaveConfig(source.stateDir, current)
	}
	return refreshed.Grant, nil
}

type statusWriter struct {
	stateDir string
	mu       sync.Mutex
	status   Status
}

func (writer *statusWriter) update(change func(*Status)) {
	writer.mu.Lock()
	change(&writer.status)
	writer.mu.Unlock()
	writer.write()
}

func (writer *statusWriter) write() {
	writer.mu.Lock()
	writer.status.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(writer.status, "", "  ")
	writer.mu.Unlock()
	if err == nil {
		_ = atomicfile.Write(filepath.Join(writer.stateDir, statusFileName), raw, 0o600)
	}
}

func (runtime *Runtime) logf(format string, args ...any) {
	if runtime.Logf != nil {
		runtime.Logf(format, args...)
	}
}

// remote access is the product name; the transport underneath is Link.
var userTextReplacer = strings.NewReplacer(
	"Mewla Link pairing admission", "a remote access pairing code",
	"Mewla Link", "remote access",
	"Link relay", "relay",
	"Link TLS", "TLS",
	"Link is connected", "remote access is connected",
)

// UserText rewrites connector errors in remote access terms.
func UserText(message string) string {
	return userTextReplacer.Replace(message)
}
