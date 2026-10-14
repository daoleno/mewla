// Package account implements Mewla's hosted remote access account service:
// daemons sign in through a device-flow identity provider, receive one
// machine (hostname label + route) per daemon key, fetch short-lived route
// grants for the relay and publish ACME DNS-01 TXT records for their own
// hostname.
//
// Operators ban an account with
//
//	UPDATE accounts SET disabled = 1 WHERE id = '<account id>';
//
// The ban takes effect within the route grant TTL: the daemon can no longer
// fetch a grant and the relay drops its route when the current grant expires.
package account

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/account/dns"
	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/linkproto"
)

const (
	defaultGrantTTL       = time.Hour
	defaultMaxMachines    = 10
	defaultACMEOpsPerHour = 20
	renamesPerDay         = 3
	// loginStartsPerIPHour bounds sign-in attempts from one client address
	// (IPv4 address or IPv6 /64), counted before the signature is checked.
	loginStartsPerIPHour  = 10
	maxPendingLoginsByKey = 5
	maxMachineNameLength  = 64
	maxLabelPrefixLength  = 20
	staleLoginRetention   = time.Hour
	acmeChallengePrefix   = "_acme-challenge."
	acmeValueLength       = 43
	identityCallTimeout   = 20 * time.Second
	// txtVisibleTimeout stays under the daemon's 30-second request timeout.
	txtVisibleTimeout = 20 * time.Second
)

// Relay is one relay advertised to machines.
type Relay struct {
	Name              string
	ControlAddress    string
	ControlServerName string
	ControlCAPEM      string
}

// Config configures a Service.
type Config struct {
	Store    *Store
	Identity IdentityProvider
	DNS      dns.Provider
	GrantKey ed25519.PrivateKey
	GrantTTL time.Duration
	// ClientDomain is the parent domain of machine hostnames.
	ClientDomain string
	ClientPort   int
	Relays       []Relay
	// ACMEDirectoryURL and ACMECAPEM are advertised to daemons.
	ACMEDirectoryURL string
	ACMECAPEM        string
	// MaxMachinesPerAccount defaults to 10.
	MaxMachinesPerAccount int
	// ACMEOpsPerHour limits TXT set and clear calls per machine; default 20 each.
	ACMEOpsPerHour int
	// WaitTXT, when set, runs after a TXT record is set and returns once the
	// zone's nameservers serve it, so the daemon does not ask the CA to
	// validate too early. It is bounded by txtVisibleTimeout.
	WaitTXT func(ctx context.Context, fqdn, value string) error
	// DeviceHandler, when set, is served at /device (the fake provider).
	DeviceHandler http.Handler
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

// Service serves the account HTTP API.
type Service struct {
	config Config
	nonces nonceCache
	acme   rateLimiter
	// renames limits address changes per account: every new label costs a
	// certificate from the domain's shared weekly issuance budget.
	renames rateLimiter
	// loginStarts limits sign-in attempts per client address.
	loginStarts rateLimiter
}

// MachineInfo is the daemon's view of its machine.
type MachineInfo struct {
	MachineID        string      `json:"machine_id"`
	AccountID        string      `json:"account_id"`
	AccountLogin     string      `json:"account_login"`
	Provider         string      `json:"provider"`
	Hostname         string      `json:"hostname"`
	Label            string      `json:"label"`
	RouteID          string      `json:"route_id"`
	Grant            string      `json:"grant"`
	GrantExpiresAtMS int64       `json:"grant_expires_at_ms"`
	Relays           []RelayInfo `json:"relays"`
	ACME             ACMEInfo    `json:"acme"`
}

// RelayInfo is one relay entry of MachineInfo.
type RelayInfo struct {
	Name              string `json:"name"`
	ControlAddress    string `json:"control_address"`
	ControlServerName string `json:"control_server_name"`
	ControlCAPEM      string `json:"control_ca_pem"`
	ClientDomain      string `json:"client_domain"`
	ClientPort        int    `json:"client_port"`
}

// ACMEInfo tells the daemon which ACME directory to use.
type ACMEInfo struct {
	DirectoryURL string `json:"directory_url"`
	CAPEM        string `json:"ca_pem"`
}

// LoginStartRequest is the body of POST /v1/login/start.
type LoginStartRequest struct {
	MachineName string `json:"machine_name"`
	RouteID     string `json:"route_id"`
}

// LoginStartResponse is the answer to POST /v1/login/start.
type LoginStartResponse struct {
	LoginID                 string `json:"login_id"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	IntervalSeconds         int64  `json:"interval_seconds"`
	ExpiresAtMS             int64  `json:"expires_at_ms"`
}

// LoginPollRequest is the body of POST /v1/login/poll.
type LoginPollRequest struct {
	LoginID string `json:"login_id"`
}

// LoginPollResponse is the answer to POST /v1/login/poll.
type LoginPollResponse struct {
	Status  string       `json:"status"`
	Machine *MachineInfo `json:"machine,omitempty"`
}

// ACMEChallengeRequest is the body of PUT/DELETE /v1/machine/acme-challenge.
type ACMEChallengeRequest struct {
	FQDN  string `json:"fqdn"`
	Value string `json:"value"`
}

// New validates config and returns a Service.
func New(config Config) (*Service, error) {
	if config.Store == nil {
		return nil, errors.New("account store is required")
	}
	if config.Identity == nil {
		return nil, errors.New("identity provider is required")
	}
	if config.DNS == nil {
		return nil, errors.New("DNS provider is required")
	}
	if len(config.GrantKey) != ed25519.PrivateKeySize {
		return nil, errors.New("grant key is required")
	}
	config.ClientDomain = strings.Trim(strings.ToLower(strings.TrimSpace(config.ClientDomain)), ".")
	if config.ClientDomain == "" {
		return nil, errors.New("client domain is required")
	}
	if config.ClientPort == 0 {
		config.ClientPort = 443
	}
	if config.ClientPort < 1 || config.ClientPort > 65535 {
		return nil, errors.New("client port is invalid")
	}
	if len(config.Relays) == 0 {
		return nil, errors.New("at least one relay is required")
	}
	if config.GrantTTL <= 0 {
		config.GrantTTL = defaultGrantTTL
	}
	if config.MaxMachinesPerAccount <= 0 {
		config.MaxMachinesPerAccount = defaultMaxMachines
	}
	if config.ACMEOpsPerHour <= 0 {
		config.ACMEOpsPerHour = defaultACMEOpsPerHour
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logf == nil {
		config.Logf = log.Printf
	}
	service := &Service{config: config}
	service.acme = rateLimiter{limit: config.ACMEOpsPerHour, window: time.Hour}
	service.renames = rateLimiter{limit: renamesPerDay, window: 24 * time.Hour}
	service.loginStarts = rateLimiter{limit: loginStartsPerIPHour, window: time.Hour}
	return service, nil
}

// PublicKey returns the grant verification key for relays.
func (s *Service) PublicKey() ed25519.PublicKey {
	return s.config.GrantKey.Public().(ed25519.PublicKey)
}

// Handler returns the HTTP API.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("POST /v1/login/start", s.limitLoginStarts(s.requireSignature(s.handleLoginStart)))
	mux.HandleFunc("POST /v1/login/poll", s.requireSignature(s.handleLoginPoll))
	mux.HandleFunc("GET /v1/machine", s.requireSignature(s.handleGetMachine))
	mux.HandleFunc("DELETE /v1/machine", s.requireSignature(s.handleDeleteMachine))
	mux.HandleFunc("PUT /v1/machine/name", s.requireSignature(s.handleRenameMachine))
	mux.HandleFunc("PUT /v1/machine/acme-challenge", s.requireSignature(s.handleACMEChallenge))
	mux.HandleFunc("DELETE /v1/machine/acme-challenge", s.requireSignature(s.handleACMEChallenge))
	if s.config.DeviceHandler != nil {
		mux.Handle("/device", s.config.DeviceHandler)
	}
	return mux
}

// limitLoginStarts counts every attempt from a client address, signed or
// not. Behind the relay, the address comes from its PROXY header.
func (s *Service) limitLoginStarts(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.loginStarts.allow("ip:"+clientaddr.Key(clientaddr.IP(r)), s.now()) {
			writeError(w, http.StatusTooManyRequests, "too many sign-ins from this address; try again later")
			return
		}
		next(w, r)
	}
}

func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	var request LoginStartRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	routeID := strings.ToLower(strings.TrimSpace(request.RouteID))
	if !linkproto.IsOpaqueID(routeID, 16) {
		writeError(w, http.StatusBadRequest, "invalid route_id")
		return
	}
	name, ok := cleanMachineName(request.MachineName)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid machine_name")
		return
	}
	key := daemonKey(r)
	owner, err := s.config.Store.routeOwner(r.Context(), routeID)
	if err != nil {
		s.internalError(w, "route lookup", err)
		return
	}
	if owner != "" && owner != key {
		writeError(w, http.StatusConflict, "route is bound to another machine")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), identityCallTimeout)
	defer cancel()
	device, err := s.config.Identity.StartDevice(ctx)
	if err != nil {
		s.config.Logf("account: login start failed: provider=%s error=%v", s.config.Identity.Name(), err)
		writeError(w, http.StatusBadGateway, "identity provider unavailable")
		return
	}
	loginID, err := randomHex(16)
	if err != nil {
		s.internalError(w, "login id", err)
		return
	}
	now := s.now()
	interval := device.Interval
	if interval < time.Second {
		interval = time.Second
	}
	created, err := s.config.Store.createLogin(r.Context(), loginRecord{
		ID:              loginID,
		DaemonPublicKey: key,
		MachineName:     name,
		RouteID:         routeID,
		Provider:        s.config.Identity.Name(),
		ProviderHandle:  device.Handle,
		IntervalMS:      interval.Milliseconds(),
		NextPollAtMS:    now.Add(interval).UnixMilli(),
		ExpiresAtMS:     device.ExpiresAt.UnixMilli(),
	}, now.UnixMilli(), maxPendingLoginsByKey)
	if err != nil {
		s.internalError(w, "create login", err)
		return
	}
	if !created {
		writeError(w, http.StatusTooManyRequests, "too many pending logins")
		return
	}
	s.config.Logf(
		"account: login started: login=%s provider=%s key=%s route=%s",
		loginID,
		s.config.Identity.Name(),
		shortKey(key),
		routeID,
	)
	writeJSON(w, http.StatusOK, LoginStartResponse{
		LoginID:                 loginID,
		UserCode:                device.UserCode,
		VerificationURI:         device.VerificationURI,
		VerificationURIComplete: device.VerificationURIComplete,
		IntervalSeconds:         int64((interval + time.Second - 1) / time.Second),
		ExpiresAtMS:             device.ExpiresAt.UnixMilli(),
	})
}

func (s *Service) handleLoginPoll(w http.ResponseWriter, r *http.Request) {
	var request LoginPollRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	loginID := strings.ToLower(strings.TrimSpace(request.LoginID))
	if !linkproto.IsOpaqueID(loginID, 16) {
		writeError(w, http.StatusBadRequest, "invalid login_id")
		return
	}
	key := daemonKey(r)
	login, err := s.config.Store.getLogin(r.Context(), loginID)
	if errors.Is(err, errNotFound) || (err == nil && login.DaemonPublicKey != key) {
		writeError(w, http.StatusNotFound, "unknown login")
		return
	}
	if err != nil {
		s.internalError(w, "load login", err)
		return
	}
	now := s.now()
	if login.Status == loginPending && now.UnixMilli() >= login.ExpiresAtMS {
		_ = s.config.Store.finishLogin(r.Context(), login.ID, loginExpired)
		login.Status = loginExpired
	}
	if login.Status == loginPending {
		claimed, err := s.config.Store.claimPoll(r.Context(), login, now.UnixMilli())
		if err != nil {
			s.internalError(w, "claim poll", err)
			return
		}
		if !claimed {
			writeJSON(w, http.StatusAccepted, LoginPollResponse{Status: loginPending})
			return
		}
		status, info, done := s.pollIdentity(w, r, login)
		if done {
			return
		}
		login.Status = status
		if status == loginComplete {
			writeJSON(w, http.StatusOK, LoginPollResponse{Status: loginComplete, Machine: &info})
			return
		}
	}
	s.writeLoginOutcome(w, r, login)
}

// pollIdentity asks the identity provider once. It returns the resulting
// login status, the machine on completion, or done=true after writing a
// response itself.
func (s *Service) pollIdentity(
	w http.ResponseWriter,
	r *http.Request,
	login loginRecord,
) (string, MachineInfo, bool) {
	if login.Provider != s.config.Identity.Name() {
		_ = s.config.Store.finishLogin(r.Context(), login.ID, loginExpired)
		return loginExpired, MachineInfo{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), identityCallTimeout)
	defer cancel()
	result, err := s.config.Identity.Poll(ctx, login.ProviderHandle)
	if err != nil {
		s.config.Logf("account: login poll failed: login=%s provider=%s error=%v", login.ID, login.Provider, err)
		writeJSON(w, http.StatusAccepted, LoginPollResponse{Status: loginPending})
		return "", MachineInfo{}, true
	}
	switch result.Status {
	case PollPending:
		if result.SlowDown {
			current := time.Duration(login.IntervalMS) * time.Millisecond
			interval := result.Interval
			if interval <= current {
				interval = current + 5*time.Second
			}
			_ = s.config.Store.slowDown(
				r.Context(),
				login.ID,
				interval.Milliseconds(),
				s.now().Add(interval).UnixMilli(),
			)
		}
		writeJSON(w, http.StatusAccepted, LoginPollResponse{Status: loginPending})
		return "", MachineInfo{}, true
	case PollDenied:
		_ = s.config.Store.finishLogin(r.Context(), login.ID, loginDenied)
		s.config.Logf("account: login denied: login=%s provider=%s", login.ID, login.Provider)
		return loginDenied, MachineInfo{}, false
	case PollExpired:
		_ = s.config.Store.finishLogin(r.Context(), login.ID, loginExpired)
		s.config.Logf("account: login expired: login=%s provider=%s", login.ID, login.Provider)
		return loginExpired, MachineInfo{}, false
	case PollComplete:
	default:
		writeJSON(w, http.StatusAccepted, LoginPollResponse{Status: loginPending})
		return "", MachineInfo{}, true
	}
	if strings.TrimSpace(result.Subject.ID) == "" {
		s.config.Logf("account: login poll failed: login=%s provider=%s error=empty subject", login.ID, login.Provider)
		_ = s.config.Store.finishLogin(r.Context(), login.ID, loginDenied)
		return loginDenied, MachineInfo{}, false
	}
	now := s.now()
	machine, account, created, err := s.config.Store.completeLogin(r.Context(), completeLoginParams{
		Login:       login,
		Provider:    login.Provider,
		Subject:     result.Subject,
		MaxMachines: s.config.MaxMachinesPerAccount,
		NowMS:       now.UnixMilli(),
		NewID:       func() (string, error) { return randomHex(16) },
		NewLabel:    newLabel,
	})
	switch {
	case errors.Is(err, errDisabled):
		s.config.Logf("account: login refused: login=%s reason=disabled", login.ID)
		return loginDenied, MachineInfo{}, false
	case errors.Is(err, errQuota):
		s.config.Logf("account: login refused: login=%s reason=quota", login.ID)
		return loginQuota, MachineInfo{}, false
	case errors.Is(err, errRouteConflict):
		s.config.Logf("account: login refused: login=%s reason=route-conflict", login.ID)
		return loginConflict, MachineInfo{}, false
	case err != nil:
		s.internalError(w, "complete login", err)
		return "", MachineInfo{}, true
	}
	if created {
		s.config.Logf(
			"account: machine created: machine=%s account=%s label=%s route=%s",
			machine.ID,
			account.ID,
			machine.Label,
			machine.RouteID,
		)
	}
	s.config.Logf(
		"account: login complete: login=%s account=%s login_name=%s machine=%s label=%s",
		login.ID,
		account.ID,
		account.Login,
		machine.ID,
		machine.Label,
	)
	info, err := s.machineInfo(machine, account)
	if err != nil {
		s.internalError(w, "sign grant", err)
		return "", MachineInfo{}, true
	}
	return loginComplete, info, false
}

func (s *Service) writeLoginOutcome(w http.ResponseWriter, r *http.Request, login loginRecord) {
	switch login.Status {
	case loginComplete:
		machine, account, err := s.config.Store.machineForKey(r.Context(), login.DaemonPublicKey)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "no machine")
			return
		}
		if err != nil {
			s.internalError(w, "load machine", err)
			return
		}
		if account.Disabled {
			writeError(w, http.StatusForbidden, "denied")
			return
		}
		info, err := s.machineInfo(machine, account)
		if err != nil {
			s.internalError(w, "sign grant", err)
			return
		}
		writeJSON(w, http.StatusOK, LoginPollResponse{Status: loginComplete, Machine: &info})
	case loginDenied:
		writeError(w, http.StatusForbidden, "denied")
	case loginQuota:
		writeError(w, http.StatusForbidden, "quota")
	case loginConflict:
		writeError(w, http.StatusConflict, "route is bound to another machine")
	case loginExpired:
		writeError(w, http.StatusGone, "expired")
	default:
		writeJSON(w, http.StatusAccepted, LoginPollResponse{Status: loginPending})
	}
}

func (s *Service) handleGetMachine(w http.ResponseWriter, r *http.Request) {
	machine, account, ok := s.currentMachine(w, r)
	if !ok {
		return
	}
	_ = s.config.Store.touchMachine(r.Context(), machine.ID, s.now().UnixMilli())
	info, err := s.machineInfo(machine, account)
	if err != nil {
		s.internalError(w, "sign grant", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// handleRenameMachine gives the machine a new hostname label: from the
// requested name, or a fresh random one when the name is empty.
func (s *Service) handleRenameMachine(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name, ok := cleanMachineName(request.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid name")
		return
	}
	current, _, ok := s.currentMachine(w, r)
	if !ok {
		return
	}
	if !s.renames.allow(current.AccountID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many renames today; try again tomorrow")
		return
	}
	machine, err := s.config.Store.renameMachine(r.Context(), daemonKey(r), name, newLabel)
	if err != nil {
		s.internalError(w, "rename machine", err)
		return
	}
	machine, account, ok := s.currentMachine(w, r)
	if !ok {
		return
	}
	s.config.Logf("account: machine renamed: machine=%s label=%s", machine.ID, machine.Label)
	info, err := s.machineInfo(machine, account)
	if err != nil {
		s.internalError(w, "sign grant", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Service) handleDeleteMachine(w http.ResponseWriter, r *http.Request) {
	machine, err := s.config.Store.deleteMachine(r.Context(), daemonKey(r))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "no machine")
		return
	}
	if err != nil {
		s.internalError(w, "delete machine", err)
		return
	}
	s.acme.forget(machine.ID)
	s.config.Logf(
		"account: machine deleted: machine=%s account=%s label=%s",
		machine.ID,
		machine.AccountID,
		machine.Label,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleACMEChallenge(w http.ResponseWriter, r *http.Request) {
	var request ACMEChallengeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	machine, _, ok := s.currentMachine(w, r)
	if !ok {
		return
	}
	fqdn := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(request.FQDN)), ".")
	if fqdn != acmeChallengePrefix+s.hostname(machine.Label) {
		writeError(w, http.StatusForbidden, "fqdn is not this machine's challenge name")
		return
	}
	if !isACMEValue(request.Value) {
		writeError(w, http.StatusBadRequest, "invalid value")
		return
	}
	set := r.Method == http.MethodPut
	operation := "clear"
	if set {
		operation = "set"
	}
	if !s.acme.allow(machine.ID+":"+operation, s.now()) {
		writeError(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	var err error
	if set {
		err = s.config.DNS.SetTXT(r.Context(), fqdn, request.Value)
	} else {
		err = s.config.DNS.ClearTXT(r.Context(), fqdn, request.Value)
	}
	if err != nil {
		s.config.Logf("account: ACME TXT %s failed: machine=%s fqdn=%s error=%v", operation, machine.ID, fqdn, err)
		writeError(w, http.StatusBadGateway, "DNS provider failed")
		return
	}
	if set && s.config.WaitTXT != nil {
		ctx, cancel := context.WithTimeout(r.Context(), txtVisibleTimeout)
		err := s.config.WaitTXT(ctx, fqdn, request.Value)
		cancel()
		if err != nil {
			// Answer anyway: the CA may still see it, and the daemon retries.
			s.config.Logf("account: ACME TXT not visible yet: machine=%s fqdn=%s error=%v", machine.ID, fqdn, err)
		}
	}
	s.config.Logf("account: ACME TXT %s: machine=%s fqdn=%s", operation, machine.ID, fqdn)
	w.WriteHeader(http.StatusNoContent)
}

// currentMachine loads the signed request's machine, writing 404 when the key
// has none and 403 when the account is disabled.
func (s *Service) currentMachine(w http.ResponseWriter, r *http.Request) (machineRecord, accountRecord, bool) {
	machine, account, err := s.config.Store.machineForKey(r.Context(), daemonKey(r))
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "no machine")
		return machineRecord{}, accountRecord{}, false
	}
	if err != nil {
		s.internalError(w, "load machine", err)
		return machineRecord{}, accountRecord{}, false
	}
	if account.Disabled {
		writeError(w, http.StatusForbidden, "account disabled")
		return machineRecord{}, accountRecord{}, false
	}
	return machine, account, true
}

func (s *Service) machineInfo(machine machineRecord, account accountRecord) (MachineInfo, error) {
	now := s.now()
	expiresAt := now.Add(s.config.GrantTTL)
	grant, err := linkproto.SignRouteGrant(s.config.GrantKey, linkproto.RouteGrant{
		AccountID:       account.ID,
		MachineID:       machine.ID,
		RouteID:         machine.RouteID,
		Label:           machine.Label,
		DaemonPublicKey: machine.DaemonPublicKey,
		IssuedAtMS:      now.UnixMilli(),
		ExpiresAtMS:     expiresAt.UnixMilli(),
	})
	if err != nil {
		return MachineInfo{}, err
	}
	relays := make([]RelayInfo, 0, len(s.config.Relays))
	for _, relay := range s.config.Relays {
		relays = append(relays, RelayInfo{
			Name:              relay.Name,
			ControlAddress:    relay.ControlAddress,
			ControlServerName: relay.ControlServerName,
			ControlCAPEM:      relay.ControlCAPEM,
			ClientDomain:      s.config.ClientDomain,
			ClientPort:        s.config.ClientPort,
		})
	}
	return MachineInfo{
		MachineID:        machine.ID,
		AccountID:        account.ID,
		AccountLogin:     account.Login,
		Provider:         account.Provider,
		Hostname:         s.hostname(machine.Label),
		Label:            machine.Label,
		RouteID:          machine.RouteID,
		Grant:            grant,
		GrantExpiresAtMS: expiresAt.UnixMilli(),
		Relays:           relays,
		ACME: ACMEInfo{
			DirectoryURL: s.config.ACMEDirectoryURL,
			CAPEM:        s.config.ACMECAPEM,
		},
	}, nil
}

func (s *Service) hostname(label string) string {
	return label + "." + s.config.ClientDomain
}

func (s *Service) now() time.Time {
	return s.config.Now()
}

func (s *Service) internalError(w http.ResponseWriter, operation string, err error) {
	s.config.Logf("account: %s failed: %v", operation, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// cleanMachineName trims a display name. Empty means "pick a random
// label": the computer's own hostname must never reach CT logs by default.
func cleanMachineName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", true
	}
	if len(name) > maxMachineNameLength {
		return "", false
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	return name, true
}

// newLabel builds "<sanitized name, <=20 chars>-<6 random hex>" for a chosen
// name, and "<word>-<word>-<4 random hex>" when no name was chosen.
func newLabel(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return randomLabel()
	}
	var builder strings.Builder
	lastHyphen := true
	for _, character := range strings.ToLower(name) {
		if builder.Len() >= maxLabelPrefixLength {
			break
		}
		switch {
		case (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9'):
			builder.WriteRune(character)
			lastHyphen = false
		case !lastHyphen:
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	prefix := strings.Trim(builder.String(), "-")
	if prefix == "" {
		prefix = "mewla"
	}
	suffix, err := randomHex(3)
	if err != nil {
		return "", err
	}
	label := prefix + "-" + suffix
	if !linkproto.IsHostLabel(label) {
		return "", fmt.Errorf("generated label %q is invalid", label)
	}
	return label, nil
}

func isACMEValue(value string) bool {
	if len(value) != acmeValueLength {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func shortKey(key string) string {
	if len(key) > 12 {
		return key[:12]
	}
	return key
}

// rateLimiter is a per-key sliding window counter.
type rateLimiter struct {
	limit  int
	window time.Duration

	mu     sync.Mutex
	events map[string][]time.Time
}

func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.events == nil {
		l.events = make(map[string][]time.Time)
	}
	cutoff := now.Add(-l.window)
	kept := l.events[key][:0]
	for _, at := range l.events[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.limit {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, now)
	if len(l.events) > 4096 {
		for entry, times := range l.events {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(l.events, entry)
			}
		}
	}
	return true
}

func (l *rateLimiter) forget(machineID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for entry := range l.events {
		if strings.HasPrefix(entry, machineID+":") {
			delete(l.events, entry)
		}
	}
}
