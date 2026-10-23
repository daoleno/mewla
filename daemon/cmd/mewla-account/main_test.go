package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrantKeyCreatedOnceWithPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "grant-key.pem")
	first, err := loadOrCreateGrantKey(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	second, err := loadOrCreateGrantKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) || len(second) != ed25519.PrivateKeySize {
		t.Fatal("reloaded key differs")
	}
}

func TestRelayFlags(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	caPEM := selfSignedPEM(t)
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	var relays relayFlags
	if err := relays.Set("name=eu,control=relay.example.com:8443"); err != nil {
		t.Fatal(err)
	}
	if err := relays.Set("name=us, control=10.0.0.1:8443, server-name=relay-us.example.com, ca-file=" + caFile); err != nil {
		t.Fatal(err)
	}
	if relays[0].serverName != "relay.example.com" || relays[1].serverName != "relay-us.example.com" {
		t.Fatalf("relays = %+v", relays)
	}
	loaded, err := relays.load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0].ControlCAPEM != "" || loaded[1].ControlCAPEM != string(caPEM) ||
		loaded[1].ControlAddress != "10.0.0.1:8443" || loaded[1].Name != "us" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := os.WriteFile(caFile, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := relays.load(); err == nil {
		t.Fatal("invalid ca-file accepted")
	}
	for _, bad := range []string{"control=a:1", "name=x", "name=x,control=nohost", "name=x,control=a:1,bogus=1"} {
		var parsed relayFlags
		if err := parsed.Set(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestGoogleExchangeNeedsEveryValueOrNone(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	key := "MEWLA_ACCOUNT_GOOGLE_RECEIPT_KEY"
	good := map[string]string{
		"MEWLA_ACCOUNT_GOOGLE_CLIENT_ID": "id", "MEWLA_ACCOUNT_GOOGLE_CLIENT_SECRET": "secret",
		key: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}
	if handler, err := googleExchange("https://connect.example", true, env(nil)); handler != nil || err != nil {
		t.Fatalf("no configuration: %v %v", handler, err)
	}
	if handler, err := googleExchange("https://connect.example", true, env(good)); handler == nil || err != nil {
		t.Fatalf("full configuration: %v %v", handler, err)
	}
	for name, values := range map[string]map[string]string{
		"only an id":     {"MEWLA_ACCOUNT_GOOGLE_CLIENT_ID": "id"},
		"no receipt key": {"MEWLA_ACCOUNT_GOOGLE_CLIENT_ID": "id", "MEWLA_ACCOUNT_GOOGLE_CLIENT_SECRET": "secret"},
		"short key":      {"MEWLA_ACCOUNT_GOOGLE_CLIENT_ID": "id", "MEWLA_ACCOUNT_GOOGLE_CLIENT_SECRET": "secret", key: "AAAA"},
		"bad key":        {"MEWLA_ACCOUNT_GOOGLE_CLIENT_ID": "id", "MEWLA_ACCOUNT_GOOGLE_CLIENT_SECRET": "secret", key: "not base64!"},
	} {
		if handler, err := googleExchange("https://connect.example", true, env(values)); handler != nil || err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := googleExchange("https://connect.example", false, env(good)); err == nil {
		t.Fatal("served without the public HTTPS listener")
	}
	if _, err := googleExchange("http://connect.example", true, env(good)); err == nil {
		t.Fatal("served from a non-HTTPS origin")
	}
}
