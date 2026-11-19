// Package acmecert keeps one host name's certificate valid by itself: it
// issues and renews it from an ACME CA by DNS-01 (the account service's DNS
// providers write the challenge), stores it in a directory, and serves the
// current one through GetCertificate, so a renewal reaches new TLS connections
// without a restart and without closing the ones already open.
package acmecert

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"golang.org/x/crypto/acme"
)

// LetsEncrypt is the production directory.
const LetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

// The files kept in Config.Dir.
const (
	AccountKeyFile = "account.pem"
	CertFile       = "cert.pem"
	KeyFile        = "key.pem"
)

// DNS writes and removes the DNS-01 challenge record (account/dns.Provider
// has these).
type DNS interface {
	SetTXT(ctx context.Context, fqdn, value string) error
	ClearTXT(ctx context.Context, fqdn, value string) error
}

type Config struct {
	// Domain is the one name on the certificate.
	Domain string
	// Dir holds the ACME account key and the issued certificate and key.
	Dir string
	// Directory is the ACME directory URL (default LetsEncrypt).
	Directory string
	// HTTPClient talks to the CA (default: the system roots).
	HTTPClient *http.Client
	DNS        DNS
	// RenewBefore is how long before expiry a renewal starts (default 30
	// days); for a certificate that lives less than twice that, at half of
	// its lifetime.
	RenewBefore time.Duration
	// CheckEvery is the longest wait between looks at the expiry (default 12 h).
	CheckEvery time.Duration
	// RetryAfter is the wait after a failed renewal (default 1 h).
	RetryAfter time.Duration
	// Propagation is the wait between publishing the challenge and asking the
	// CA to validate it (default 30 s): the CA asks the authoritative servers.
	Propagation time.Duration
	// SeedCert and SeedKey are PEM files used until the first certificate of
	// Dir exists (the one a deploy uploaded); the later-expiring one is served.
	SeedCert, SeedKey string
	Logf              func(format string, args ...any)
	// Now is the clock (tests).
	Now func() time.Time
	// issue replaces the ACME exchange (tests).
	issue func(ctx context.Context) ([]byte, []byte, error)
}

// Status is what the keeper reports for health and metrics.
type Status struct {
	Domain   string
	NotAfter time.Time // zero: no certificate
	// Failures counts renewals that failed since the last success.
	Failures  int
	LastError string
	Renewed   time.Time // the last successful renewal (zero: none yet)
}

// DaysLeft is the days until NotAfter (negative once expired).
func (s Status) DaysLeft(now time.Time) float64 {
	if s.NotAfter.IsZero() {
		return 0
	}
	return s.NotAfter.Sub(now).Hours() / 24
}

type Keeper struct {
	cfg   Config
	renew chan struct{}

	mu        sync.Mutex
	cert      *tls.Certificate
	notAfter  time.Time
	failures  int
	lastError string
	renewed   time.Time
}

// New loads the certificate of Dir (or the seed) without renewing anything;
// Run keeps it valid.
func New(cfg Config) (*Keeper, error) {
	cfg.Domain = strings.ToLower(strings.TrimSpace(cfg.Domain))
	if cfg.Domain == "" || strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("acmecert: a domain and a directory are required")
	}
	if cfg.DNS == nil && cfg.issue == nil {
		return nil, errors.New("acmecert: a DNS provider is required")
	}
	if cfg.Directory == "" {
		cfg.Directory = LetsEncrypt
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.RenewBefore <= 0 {
		cfg.RenewBefore = 30 * 24 * time.Hour
	}
	if cfg.CheckEvery <= 0 {
		cfg.CheckEvery = 12 * time.Hour
	}
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = time.Hour
	}
	if cfg.Propagation <= 0 {
		cfg.Propagation = 30 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("acmecert: %w", err)
	}
	k := &Keeper{cfg: cfg, renew: make(chan struct{}, 1)}
	stored, storedErr := loadPair(filepath.Join(cfg.Dir, CertFile), filepath.Join(cfg.Dir, KeyFile))
	var seed *tls.Certificate
	var seedErr error
	if cfg.SeedCert != "" {
		seed, seedErr = loadPair(cfg.SeedCert, cfg.SeedKey)
	}
	for _, c := range []*tls.Certificate{stored, seed} {
		if c != nil && (k.cert == nil || c.Leaf.NotAfter.After(k.notAfter)) {
			k.cert, k.notAfter = c, c.Leaf.NotAfter
		}
	}
	if storedErr != nil && !errors.Is(storedErr, os.ErrNotExist) {
		cfg.Logf("acmecert: %s: stored certificate unreadable (renewing): %v", cfg.Domain, storedErr)
	}
	if seedErr != nil {
		cfg.Logf("acmecert: %s: seed certificate unreadable: %v", cfg.Domain, seedErr)
	}
	return k, nil
}

// GetCertificate serves the current certificate (tls.Config.GetCertificate).
func (k *Keeper) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.cert == nil {
		return nil, fmt.Errorf("no certificate for %s yet", k.cfg.Domain)
	}
	return k.cert, nil
}

func (k *Keeper) Status() Status {
	k.mu.Lock()
	defer k.mu.Unlock()
	return Status{Domain: k.cfg.Domain, NotAfter: k.notAfter, Failures: k.failures, LastError: k.lastError, Renewed: k.renewed}
}

// RenewNow asks Run to renew at once, whatever the expiry.
func (k *Keeper) RenewNow() {
	select {
	case k.renew <- struct{}{}:
	default:
	}
}

// due is when the current certificate should be renewed.
func (k *Keeper) due() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.cert == nil {
		return time.Time{}
	}
	before := k.cfg.RenewBefore
	if life := k.notAfter.Sub(k.cert.Leaf.NotBefore); life > 0 && life < 2*before {
		before = life / 2
	}
	return k.notAfter.Add(-before)
}

// Run renews when due (and on RenewNow) until ctx ends. A failure is logged,
// counted in Status and retried after RetryAfter.
func (k *Keeper) Run(ctx context.Context) {
	forced := false
	for {
		now := k.cfg.Now()
		if due := k.due(); forced || !now.Before(due) {
			forced = false
			if err := k.Renew(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				if !k.wait(ctx, k.cfg.RetryAfter, &forced) {
					return
				}
				continue
			}
		}
		st := k.Status()
		wait := k.cfg.CheckEvery
		if until := k.due().Sub(k.cfg.Now()); until < wait {
			wait = max(until, time.Second)
		}
		k.cfg.Logf("acmecert: %s valid until %s (%.0f days); next look in %s",
			k.cfg.Domain, st.NotAfter.UTC().Format(time.RFC3339), st.DaysLeft(k.cfg.Now()), wait.Round(time.Minute))
		if !k.wait(ctx, wait, &forced) {
			return
		}
	}
}

func (k *Keeper) wait(ctx context.Context, d time.Duration, forced *bool) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-k.renew:
		*forced = true
	case <-t.C:
	}
	return true
}

// Renew issues a new certificate now, stores it and serves it.
func (k *Keeper) Renew(ctx context.Context) error {
	k.cfg.Logf("acmecert: %s: renewing from %s", k.cfg.Domain, k.cfg.Directory)
	issue := k.cfg.issue
	if issue == nil {
		issue = k.issueACME
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	chainPEM, keyPEM, err := issue(ctx)
	var cert tls.Certificate
	if err == nil {
		cert, err = tls.X509KeyPair(chainPEM, keyPEM)
	}
	if err == nil {
		err = atomicfile.Write(filepath.Join(k.cfg.Dir, KeyFile), keyPEM, 0o600)
	}
	if err == nil {
		err = atomicfile.Write(filepath.Join(k.cfg.Dir, CertFile), chainPEM, 0o600)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err != nil {
		k.failures++
		k.lastError = err.Error()
		k.cfg.Logf("acmecert: %s: renewal failed (%d in a row): %v", k.cfg.Domain, k.failures, err)
		return err
	}
	k.cert, k.notAfter = &cert, cert.Leaf.NotAfter
	k.failures, k.lastError, k.renewed = 0, "", k.cfg.Now()
	k.cfg.Logf("acmecert: %s: renewed, serial %s, valid until %s", k.cfg.Domain, cert.Leaf.SerialNumber.Text(16), k.notAfter.UTC().Format(time.RFC3339))
	return nil
}

func (k *Keeper) issueACME(ctx context.Context) ([]byte, []byte, error) {
	accountKey, err := k.accountKey()
	if err != nil {
		return nil, nil, err
	}
	client := &acme.Client{Key: accountKey, DirectoryURL: k.cfg.Directory, HTTPClient: k.cfg.HTTPClient}
	if _, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, nil, fmt.Errorf("register with the CA: %w", err)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(k.cfg.Domain))
	if err != nil {
		return nil, nil, fmt.Errorf("order: %w", err)
	}
	for _, authzURL := range order.AuthzURLs {
		if err := k.authorize(ctx, client, authzURL); err != nil {
			return nil, nil, err
		}
	}
	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return nil, nil, fmt.Errorf("wait for the order: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: k.cfg.Domain}, DNSNames: []string{k.cfg.Domain},
	}, key)
	if err != nil {
		return nil, nil, err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		// A CA may answer finalize without a Location header; poll the order.
		ready, waitErr := client.WaitOrder(ctx, order.URI)
		if waitErr != nil || ready.Status != acme.StatusValid || ready.CertURL == "" {
			return nil, nil, fmt.Errorf("finalize: %w", err)
		}
		if chain, err = client.FetchCert(ctx, ready.CertURL, true); err != nil {
			return nil, nil, fmt.Errorf("download the certificate: %w", err)
		}
	}
	var chainPEM []byte
	for _, der := range chain {
		chainPEM = append(chainPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return chainPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

func (k *Keeper) authorize(ctx context.Context, client *acme.Client, authzURL string) error {
	authz, err := client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("read the authorization: %w", err)
	}
	if authz.Status == acme.StatusValid {
		return nil
	}
	var challenge *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "dns-01" {
			challenge = c
		}
	}
	if challenge == nil {
		return errors.New("the CA offered no DNS challenge")
	}
	value, err := client.DNS01ChallengeRecord(challenge.Token)
	if err != nil {
		return err
	}
	fqdn := "_acme-challenge." + k.cfg.Domain
	if err := k.cfg.DNS.SetTXT(ctx, fqdn, value); err != nil {
		return fmt.Errorf("publish the challenge: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := k.cfg.DNS.ClearTXT(cleanup, fqdn, value); err != nil {
			k.cfg.Logf("acmecert: %s: removing the challenge record: %v", k.cfg.Domain, err)
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(k.cfg.Propagation):
	}
	if _, err := client.Accept(ctx, challenge); err != nil {
		return fmt.Errorf("answer the challenge: %w", err)
	}
	if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
		return fmt.Errorf("the CA rejected the challenge: %w", err)
	}
	return nil
}

func (k *Keeper) accountKey() (crypto.Signer, error) {
	path := filepath.Join(k.cfg.Dir, AccountKeyFile)
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, errors.New("ACME account key: no PEM block")
		}
		return x509.ParseECPrivateKey(block.Bytes)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := atomicfile.Write(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func loadPair(certPath, keyPath string) (*tls.Certificate, error) {
	if _, err := os.Stat(certPath); err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}
