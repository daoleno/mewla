package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
)

func fixtureTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// The owned-native pairing mint must produce the exact production v2 shape:
// daemon-signed binding, SPKI pin of the served cert, emulator-visible LAN
// admission URL, expiry and relay-free route. The client verifies the daemon
// signature and tunnels to the candidate with the pin; no CA is involved.
func TestMintFixtureLinkShape(t *testing.T) {
	dir := t.TempDir()
	manager, err := auth.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert := fixtureTestCert(t)
	got, err := mintFixtureLink(manager, cert, "https://10.0.2.2:19877/some/path?q=1#frag")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil || parsed.Scheme != "zen" || parsed.Query().Get("v") != "2" {
		t.Fatalf("link envelope wrong: %q", got)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parsed.Query().Get("p"))
	if err != nil {
		t.Fatal(err)
	}
	var payload link.PairingPayload
	if json.Unmarshal(raw, &payload) != nil {
		t.Fatal("link payload not JSON")
	}
	if err := link.ValidatePairingPayload(payload, time.Now()); err != nil {
		t.Fatalf("minted payload invalid: %v", err)
	}
	if len(payload.Candidates) != 1 || payload.Candidates[0].AdmissionURL != "https://10.0.2.2:19877" {
		t.Fatalf("admission URL not normalized to numerical LAN: %+v", payload.Candidates)
	}
	if payload.DaemonID != manager.DaemonID() || payload.DaemonPublicKey != manager.PublicKeyHex() {
		t.Fatal("payload identity mismatch")
	}
	if _, err := manager.EnrollDevice(
		"bogus-token", payload.DaemonID, payload.DaemonPublicKey,
		"owned-test-phone", "Owned Test Phone", strings.Repeat("ab", 32),
	); err == nil {
		t.Fatal("bogus token accepted")
	}
	if _, err := mintFixtureLink(manager, cert, "http://10.0.2.2:19877"); err == nil {
		t.Fatal("cleartext base accepted")
	}
	if _, err := mintFixtureLink(manager, nil, "https://10.0.2.2:19877"); err == nil {
		t.Fatal("missing cert accepted")
	}
}

// mintFixtureLink builds a test-only Link v2 pairing link for the owned
// native route: daemon-signed binding, SPKI pin of the served TLS cert, and
// the emulator-visible admission URL. No relay is involved at any step.
func mintFixtureLink(m *auth.Manager, servedCert *x509.Certificate, baseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base.Scheme != "https" || base.Host == "" || servedCert == nil {
		return "", errors.New("invalid fixture link base")
	}
	base.RawQuery, base.Fragment, base.Path = "", "", ""
	token, err := m.IssuePairingToken(10 * time.Minute)
	if err != nil {
		return "", err
	}
	var route [16]byte
	if _, err := rand.Read(route[:]); err != nil {
		return "", err
	}
	pin := sha256.Sum256(servedCert.RawSubjectPublicKeyInfo)
	payload := link.PairingPayload{
		Version:         link.PairingVersion,
		DaemonID:        m.DaemonID(),
		DaemonPublicKey: m.PublicKeyHex(),
		EnrollmentToken: strings.ToLower(strings.TrimSpace(token.Value)),
		RouteID:         hex.EncodeToString(route[:]),
		TransportPin:    hex.EncodeToString(pin[:]),
		Candidates: []link.PairingCandidate{
			{Name: "Owned VM", AdmissionURL: base.String(), StableURL: base.String()},
		},
		ExpiresAtMS: time.Now().Add(10 * time.Minute).UnixMilli(),
	}
	payload.Signature = m.CreateLinkPairingSignature(link.PairingBindingPayload(payload))
	if err := link.ValidatePairingPayload(payload, time.Now()); err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("v", fmt.Sprintf("%d", link.PairingVersion))
	values.Set("p", base64.RawURLEncoding.EncodeToString(raw))
	return "zen://settings?" + values.Encode(), nil
}
