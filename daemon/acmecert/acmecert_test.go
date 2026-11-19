package acmecert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testDomain = "turn.example.test"

var serials atomic.Int64

// pemPair is a self-signed certificate for testDomain valid in [from, until).
func pemPair(t *testing.T, from, until time.Time) (chain, key []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serials.Add(1)), Subject: pkix.Name{CommonName: testDomain},
		DNSNames: []string{testDomain}, NotBefore: from, NotAfter: until,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writePair(t *testing.T, dir, name string, from, until time.Time) (string, string) {
	t.Helper()
	chain, key := pemPair(t, from, until)
	c, k := filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
	if err := os.WriteFile(c, chain, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return c, k
}

// fakeCA issues certificates valid for life from now, failing while fail is set.
type fakeCA struct {
	t      *testing.T
	life   time.Duration
	mu     sync.Mutex
	fail   error
	issued int
	done   chan struct{}
}

func (f *fakeCA) issue(context.Context) ([]byte, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer func() {
		select {
		case f.done <- struct{}{}:
		default:
		}
	}()
	if f.fail != nil {
		return nil, nil, f.fail
	}
	f.issued++
	chain, key := pemPair(f.t, time.Now().Add(-time.Minute), time.Now().Add(f.life))
	return chain, key, nil
}

func newKeeper(t *testing.T, ca *fakeCA, cfg Config) *Keeper {
	t.Helper()
	cfg.Domain = testDomain
	if cfg.Dir == "" {
		cfg.Dir = t.TempDir()
	}
	cfg.issue = ca.issue
	cfg.Logf = t.Logf
	k, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func waitIssue(t *testing.T, ca *fakeCA) {
	t.Helper()
	select {
	case <-ca.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no renewal attempt")
	}
}

func serial(t *testing.T, k *Keeper) string {
	t.Helper()
	c, err := k.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Leaf.SerialNumber.String()
}

// A seed that is not yet due is served and nothing is renewed; the later of
// the seed and the stored certificate wins.
func TestSeedServedUntilDueAndLaterOneWins(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	seedC, seedK := writePair(t, dir, "seed", now.Add(-time.Hour), now.Add(80*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{SeedCert: seedC, SeedKey: seedK})
	if got := k.Status().NotAfter; !got.Equal(now.Add(80 * 24 * time.Hour).Truncate(time.Second)) {
		t.Fatalf("seed not served: %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go k.Run(ctx)
	select {
	case <-ca.done:
		t.Fatal("renewed a certificate 80 days from expiry")
	case <-time.After(300 * time.Millisecond):
	}
	cancel()

	// A stored certificate that expires later than the seed is served instead.
	storeDir := t.TempDir()
	c, key := writePair(t, storeDir, "x", now.Add(-time.Hour), now.Add(85*24*time.Hour))
	_ = os.Rename(c, filepath.Join(storeDir, CertFile))
	_ = os.Rename(key, filepath.Join(storeDir, KeyFile))
	k2 := newKeeper(t, ca, Config{Dir: storeDir, SeedCert: seedC, SeedKey: seedK})
	if d := k2.Status().DaysLeft(now); d < 84 || d > 85 {
		t.Fatalf("stored certificate not preferred: %.1f days", d)
	}
}

// A certificate inside RenewBefore is renewed at start; the new one is served
// and stored, and a keeper started again serves the stored one.
func TestRenewsWhenDueAndStores(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-80*24*time.Hour), now.Add(10*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{Dir: dir, SeedCert: seedC, SeedKey: seedK})
	before := serial(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)
	waitIssue(t, ca)
	waitFor(t, func() bool { return serial(t, k) != before })
	if d := k.Status().DaysLeft(time.Now()); d < 89 {
		t.Fatalf("renewed certificate has %.1f days", d)
	}
	if st := k.Status(); st.Failures != 0 || st.Renewed.IsZero() {
		t.Fatalf("status after renewal: %+v", st)
	}
	for _, name := range []string{CertFile, KeyFile} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	again := newKeeper(t, ca, Config{Dir: dir, SeedCert: seedC, SeedKey: seedK})
	if serial(t, again) != serial(t, k) {
		t.Fatal("a restarted keeper does not serve the renewed certificate")
	}
}

// A failed renewal is counted and retried after RetryAfter; success resets it.
func TestFailureIsCountedAndRetried(t *testing.T) {
	now := time.Now()
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-80*24*time.Hour), now.Add(5*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 8), fail: errors.New("DNS provider said no")}
	k := newKeeper(t, ca, Config{SeedCert: seedC, SeedKey: seedK, RetryAfter: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)
	waitIssue(t, ca)
	waitIssue(t, ca)
	st := k.Status()
	if st.Failures < 1 || st.LastError != "DNS provider said no" {
		t.Fatalf("failure not reported: %+v", st)
	}
	if d := st.DaysLeft(time.Now()); d > 5 {
		t.Fatalf("the old certificate should still be served: %.1f days", d)
	}
	ca.mu.Lock()
	ca.fail = nil
	ca.mu.Unlock()
	waitFor(t, func() bool { return k.Status().Failures == 0 && k.Status().DaysLeft(time.Now()) > 89 })
}

// RenewNow renews a certificate far from expiry.
func TestRenewNowForcesARenewal(t *testing.T) {
	now := time.Now()
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-time.Hour), now.Add(89*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{SeedCert: seedC, SeedKey: seedK})
	before := serial(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	k.RenewNow()
	waitIssue(t, ca)
	waitFor(t, func() bool { return serial(t, k) != before })
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if ca.issued != 1 {
		t.Fatalf("issued %d", ca.issued)
	}
}

// A certificate that lives less than twice RenewBefore renews at half its
// life, not at once and forever.
func TestShortLivedCertificateRenewsAtHalfLife(t *testing.T) {
	now := time.Now()
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-time.Hour), now.Add(5*24*time.Hour))
	ca := &fakeCA{t: t, life: 6 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{SeedCert: seedC, SeedKey: seedK})
	if due := k.due(); !due.After(now) {
		t.Fatalf("a 6-day certificate one hour old is due at %v", due)
	}
	seedC2, seedK2 := writePair(t, t.TempDir(), "old", now.Add(-4*24*time.Hour), now.Add(2*24*time.Hour))
	k2 := newKeeper(t, ca, Config{SeedCert: seedC2, SeedKey: seedK2})
	if due := k2.due(); due.After(now) {
		t.Fatalf("a 6-day certificate past half life is not due: %v", due)
	}
}

// A renewal reaches new TLS connections; one already open keeps working.
func TestRenewalReachesNewConnectionsWithoutClosingOpenOnes(t *testing.T) {
	now := time.Now()
	seedC, seedK := writePair(t, t.TempDir(), "seed", now.Add(-time.Hour), now.Add(89*24*time.Hour))
	ca := &fakeCA{t: t, life: 90 * 24 * time.Hour, done: make(chan struct{}, 4)}
	k := newKeeper(t, ca, Config{SeedCert: seedC, SeedKey: seedK})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: k.GetCertificate})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(c)
		}
	}()
	dial := func() (*tls.Conn, string) {
		c, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, ServerName: testDomain})
		if err != nil {
			t.Fatal(err)
		}
		return c, c.ConnectionState().PeerCertificates[0].SerialNumber.String()
	}
	open, first := dial()
	defer open.Close()
	if err := k.Renew(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh, second := dial()
	fresh.Close()
	if first == second {
		t.Fatal("a new connection got the old certificate")
	}
	if _, err := open.Write([]byte("still here")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	_ = open.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := open.Read(buf); err != nil || string(buf[:n]) != "still here" {
		t.Fatalf("the open connection after renewal: %q %v", buf[:n], err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
