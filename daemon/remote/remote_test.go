package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/linkproto"
)

func testConfig(serviceURL string) Config {
	return Config{
		ServiceURL: serviceURL,
		Machine: Machine{
			MachineID: "mach_1", AccountLogin: "octo", Hostname: "studio-3f9a2c.mewla.test",
			Label: "studio-3f9a2c", RouteID: strings.Repeat("ab", 16),
			Grant: "old", GrantExpiresAtMS: time.Now().Add(10 * time.Minute).UnixMilli(),
			Relays: []Relay{{Name: "r1", ControlAddress: "127.0.0.1:1", ControlServerName: "relay.test", ClientDomain: "mewla.test", ClientPort: 19443}},
		},
	}
}

func TestConfigRoundTripAndURL(t *testing.T) {
	dir := t.TempDir()
	if _, ok, err := LoadConfig(dir); ok || err != nil {
		t.Fatalf("signed-out load ok=%v err=%v", ok, err)
	}
	config := testConfig("https://svc.test")
	if err := SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadConfig(dir)
	if !ok || err != nil || loaded.Machine.Label != "studio-3f9a2c" {
		t.Fatalf("load = %#v ok=%v err=%v", loaded, ok, err)
	}
	if got := loaded.URL(); got != "https://studio-3f9a2c.mewla.test:19443" {
		t.Fatalf("URL = %s", got)
	}
	if err := RemoveSignIn(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadConfig(dir); ok {
		t.Fatal("sign-in survived RemoveSignIn")
	}
}

// The service verifies exactly this payload with auth.VerifyLinkSignature.
func TestClientSignsRequestsWithTheDaemonKey(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "MewlaMachine v1:"), ":")
		if len(parts) != 4 || parts[0] != manager.PublicKeyHex() {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		digest := sha256.Sum256(body)
		payload := strings.Join([]string{"mewla-account-v1", r.Method, r.URL.Path, parts[1], parts[2], hex.EncodeToString(digest[:])}, "\n")
		seen = auth.VerifyLinkSignature(parts[0], []byte(payload), parts[3])
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PollLogin(context.Background(), "login_1"); err != ErrLoginPending {
		t.Fatalf("poll err = %v", err)
	}
	if !seen {
		t.Fatal("service could not verify the request signature")
	}
}

func signedGrant(t *testing.T, issued, expires time.Time) string {
	t.Helper()
	_, service, _ := ed25519.GenerateKey(rand.Reader)
	daemonKey, _, _ := ed25519.GenerateKey(rand.Reader)
	token, err := linkproto.SignRouteGrant(service, linkproto.RouteGrant{
		AccountID: "acct_1", MachineID: "mach_1", RouteID: strings.Repeat("ab", 16), Label: "studio-3f9a2c",
		DaemonPublicKey: hex.EncodeToString(daemonKey), IssuedAtMS: issued.UnixMilli(), ExpiresAtMS: expires.UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestGrantSourceRefreshesPastHalfLifeAndSaves(t *testing.T) {
	manager, _ := auth.NewManager(t.TempDir())
	dir := t.TempDir()
	now := time.Now()
	fresh := testConfig("").Machine
	fresh.Grant = signedGrant(t, now, now.Add(time.Hour))
	fresh.GrantExpiresAtMS = now.Add(time.Hour).UnixMilli()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(fresh)
	}))
	defer server.Close()
	config := testConfig(server.URL)
	config.Machine.Grant = signedGrant(t, now.Add(-50*time.Minute), now.Add(10*time.Minute))
	if err := SaveConfig(dir, config); err != nil {
		t.Fatal(err)
	}
	client, _ := NewClient(server.URL, "", manager)
	source := &grantSource{stateDir: dir, client: client, config: config}
	for range 2 {
		grant, err := source.Grant(context.Background())
		if err != nil || grant != fresh.Grant {
			t.Fatalf("grant = %q, %v", grant, err)
		}
	}
	if calls != 1 {
		t.Fatalf("service calls = %d, want 1", calls)
	}
	saved, _, _ := LoadConfig(dir)
	if saved.Machine.Grant != fresh.Grant {
		t.Fatal("refreshed grant was not saved")
	}
}

func TestSignedOutMachineStopsGrants(t *testing.T) {
	manager, _ := auth.NewManager(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "", manager)
	config := testConfig(server.URL)
	config.Machine.GrantExpiresAtMS = time.Now().UnixMilli()
	source := &grantSource{stateDir: t.TempDir(), client: client, config: config}
	if _, err := source.Grant(context.Background()); err != ErrSignedOut {
		t.Fatalf("err = %v, want ErrSignedOut", err)
	}
}

func TestServerTLSConfigServesWebPKICertOnlyForTheHostname(t *testing.T) {
	identity, err := link.LoadOrCreateTransportIdentity(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	hosted := identity.Certificate
	hosted.Certificate = [][]byte{[]byte("hosted")}
	certs := &CertManager{cert: &hosted}
	config := ServerTLSConfig(identity.Certificate, "studio-3f9a2c.mewla.test", certs)
	for name, want := range map[string]string{
		"studio-3f9a2c.mewla.test":  "hosted",
		"STUDIO-3F9A2C.mewla.test.": "hosted",
		"other-111111.mewla.test":   "self",
		"":                          "self",
	} {
		got, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
		if err != nil {
			t.Fatal(err)
		}
		if kind := map[bool]string{true: "hosted", false: "self"}[string(got.Certificate[0]) == "hosted"]; kind != want {
			t.Fatalf("%q served %s, want %s", name, kind, want)
		}
	}
}

func TestUserTextNeverSaysLink(t *testing.T) {
	for _, message := range []string{
		"no Link relay is available: local: dial tcp: connection refused",
		"Link relay local disconnected: EOF",
		"request Mewla Link pairing admission from remote access: boom (make sure mewla is running and Link is connected)",
	} {
		if got := UserText(message); strings.Contains(got, "Link") {
			t.Fatalf("%q -> %q", message, got)
		}
	}
}
