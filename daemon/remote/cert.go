package remote

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

const renewBefore = 30 * 24 * time.Hour

// CertManager keeps a WebPKI certificate for this computer's hostname. The
// certificate reuses the Link transport key, so its SPKI equals the pin
// phones already trust; the private key never leaves this computer.
type CertManager struct {
	stateDir string
	key      crypto.Signer
	client   *Client
	config   Config

	mu   sync.RWMutex
	cert *tls.Certificate
}

func NewCertManager(stateDir string, key crypto.Signer, client *Client, config Config) *CertManager {
	manager := &CertManager{stateDir: stateDir, key: key, client: client, config: config}
	if cert, err := manager.load(); err == nil {
		manager.cert = cert
	}
	return manager
}

// Certificate returns the current certificate, or nil before the first issue.
func (manager *CertManager) Certificate() *tls.Certificate {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.cert
}

// NotAfter reports when the current certificate expires.
func (manager *CertManager) NotAfter() time.Time {
	if cert := manager.Certificate(); cert != nil && cert.Leaf != nil {
		return cert.Leaf.NotAfter
	}
	return time.Time{}
}

// Ensure issues a certificate when none is valid for the hostname or it
// expires within 30 days.
func (manager *CertManager) Ensure(ctx context.Context) error {
	if cert := manager.Certificate(); cert != nil && cert.Leaf != nil &&
		time.Until(cert.Leaf.NotAfter) > renewBefore &&
		cert.Leaf.VerifyHostname(manager.config.Machine.Hostname) == nil {
		return nil
	}
	cert, err := manager.issue(ctx)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	manager.cert = cert
	manager.mu.Unlock()
	return nil
}

func (manager *CertManager) issue(ctx context.Context) (*tls.Certificate, error) {
	hostname := manager.config.Machine.Hostname
	directory := strings.TrimSpace(manager.config.Machine.ACME.DirectoryURL)
	if directory == "" {
		return nil, errors.New("remote access service gave no certificate authority")
	}
	accountKey, err := manager.accountKey()
	if err != nil {
		return nil, err
	}
	httpClient := http.DefaultClient
	if caPEM := strings.TrimSpace(manager.config.Machine.ACME.CAPEM); caPEM != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, errors.New("certificate authority CA contains no certificates")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		httpClient = &http.Client{Transport: transport}
	}
	client := &acme.Client{Key: accountKey, DirectoryURL: directory, HTTPClient: httpClient}
	if _, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil &&
		!errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("register with certificate authority: %w", err)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(hostname))
	if err != nil {
		return nil, fmt.Errorf("order certificate: %w", err)
	}
	orderURL := order.URI
	for _, authzURL := range order.AuthzURLs {
		if err := manager.authorize(ctx, client, authzURL); err != nil {
			return nil, err
		}
	}
	if order, err = client.WaitOrder(ctx, orderURL); err != nil {
		return nil, fmt.Errorf("wait for certificate order: %w", err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: hostname},
		DNSNames: []string{hostname},
	}, manager.key)
	if err != nil {
		return nil, fmt.Errorf("create certificate request: %w", err)
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		// A CA may answer finalize without a Location header, leaving the
		// library nothing to poll; poll the order URL known from the start.
		ready, waitErr := client.WaitOrder(ctx, orderURL)
		if waitErr != nil || ready.Status != acme.StatusValid || ready.CertURL == "" {
			return nil, fmt.Errorf("finalize certificate: %w", err)
		}
		if chain, err = client.FetchCert(ctx, ready.CertURL, true); err != nil {
			return nil, fmt.Errorf("download certificate: %w", err)
		}
	}
	var encoded []byte
	for _, der := range chain {
		encoded = append(encoded, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	if err := atomicfile.Write(filepath.Join(manager.stateDir, certFileName), encoded, 0o600); err != nil {
		return nil, err
	}
	return manager.parse(encoded)
}

func (manager *CertManager) authorize(ctx context.Context, client *acme.Client, authzURL string) error {
	authz, err := client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("read certificate authorization: %w", err)
	}
	if authz.Status == acme.StatusValid {
		return nil
	}
	var challenge *acme.Challenge
	for _, candidate := range authz.Challenges {
		if candidate.Type == "dns-01" {
			challenge = candidate
			break
		}
	}
	if challenge == nil {
		return errors.New("certificate authority offered no DNS challenge")
	}
	value, err := client.DNS01ChallengeRecord(challenge.Token)
	if err != nil {
		return err
	}
	fqdn := "_acme-challenge." + manager.config.Machine.Hostname
	if err := manager.client.SetACMEChallenge(ctx, fqdn, value); err != nil {
		return fmt.Errorf("publish certificate challenge: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = manager.client.ClearACMEChallenge(cleanup, fqdn, value)
	}()
	if _, err := client.Accept(ctx, challenge); err != nil {
		return fmt.Errorf("answer certificate challenge: %w", err)
	}
	if _, err := client.WaitAuthorization(ctx, authz.URI); err != nil {
		return fmt.Errorf("certificate authority rejected the challenge: %w", err)
	}
	return nil
}

func (manager *CertManager) load() (*tls.Certificate, error) {
	raw, err := os.ReadFile(filepath.Join(manager.stateDir, certFileName))
	if err != nil {
		return nil, err
	}
	return manager.parse(raw)
}

func (manager *CertManager) parse(chainPEM []byte) (*tls.Certificate, error) {
	cert := &tls.Certificate{PrivateKey: manager.key}
	for {
		var block *pem.Block
		block, chainPEM = pem.Decode(chainPEM)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert.Certificate = append(cert.Certificate, block.Bytes)
		}
	}
	if len(cert.Certificate) == 0 {
		return nil, errors.New("certificate file is empty")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	public, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	signer, signerOK := manager.key.Public().(*ecdsa.PublicKey)
	if !ok || !signerOK || !public.Equal(signer) {
		return nil, errors.New("certificate does not match this computer's key")
	}
	cert.Leaf = leaf
	return cert, nil
}

func (manager *CertManager) accountKey() (crypto.Signer, error) {
	path := filepath.Join(manager.stateDir, acmeKeyFile)
	if raw, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, errors.New("certificate account key is unreadable")
		}
		return x509.ParseECPrivateKey(block.Bytes)
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

// ServerTLSConfig serves the WebPKI certificate to clients asking for the
// hosted hostname and the self-signed transport certificate to everyone else
// (pinned phones and self-hosted relays). Both carry the same key.
func ServerTLSConfig(fallback tls.Certificate, hostname string, certs *CertManager) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if certs != nil && strings.EqualFold(strings.TrimSuffix(hello.ServerName, "."), hostname) {
				if cert := certs.Certificate(); cert != nil {
					return cert, nil
				}
			}
			return &fallback, nil
		},
	}
}
