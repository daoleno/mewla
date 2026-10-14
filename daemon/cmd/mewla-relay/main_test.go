package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunRejectsNonPositiveAndOverflowingCapacityFlagsBeforeAllocation(
	t *testing.T,
) {
	t.Setenv(
		"MEWLA_LINK_CONNECTOR_TOKEN",
		"abcdef0123456789abcdef0123456789",
	)
	flags := []string{
		"max-routes",
		"max-clients",
		"max-clients-per-route",
		"max-client-handshakes",
		"max-connector-handshakes",
		"max-admissions",
		"max-admissions-per-route",
		"max-nonces",
		"max-pending-streams",
		"max-route-bindings",
		"max-client-handshakes-per-ip",
		"max-clients-per-ip",
		"max-connector-handshakes-per-ip",
	}
	for _, name := range flags {
		for _, value := range []int{0, -1, math.MaxInt} {
			t.Run(name+"/"+strconv.Itoa(value), func(t *testing.T) {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("run panicked: %v", recovered)
					}
				}()
				err := run([]string{
					"-" + name + "=" + strconv.Itoa(value),
				})
				if err == nil ||
					!strings.Contains(err.Error(), name) {
					t.Fatalf("invalid flag error=%v", err)
				}
			})
		}
	}
}

func TestRunRejectsNonPositiveDurationFlags(t *testing.T) {
	t.Setenv(
		"MEWLA_LINK_CONNECTOR_TOKEN",
		"abcdef0123456789abcdef0123456789",
	)
	for _, name := range []string{
		"handshake-timeout",
		"attach-timeout",
		"idle-timeout",
		"sweep-interval",
	} {
		for _, value := range []string{"0s", "-1s"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				err := run([]string{"-" + name + "=" + value})
				if err == nil ||
					!strings.Contains(err.Error(), name) {
					t.Fatalf("invalid duration error=%v", err)
				}
			})
		}
	}
}

func TestRunAcceptsGrantKeysWithoutConnectorToken(t *testing.T) {
	t.Setenv("MEWLA_LINK_CONNECTOR_TOKEN", "")
	if err := run([]string{}); err == nil ||
		!strings.Contains(err.Error(), "grant-public-key-file") {
		t.Fatalf("missing credentials error=%v", err)
	}

	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	keyFile := filepath.Join(directory, "grant.pub")
	if err := os.WriteFile(
		keyFile,
		[]byte("# account service\n"+hex.EncodeToString(publicKey)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	keys, err := loadGrantKeys(keyFile)
	if err != nil || len(keys) != 1 || !keys[0].Equal(publicKey) {
		t.Fatalf("grant keys=%v err=%v", keys, err)
	}
	// Credentials and relay config pass; startup stops at the TLS identity.
	err = run([]string{
		"-grant-public-key-file=" + keyFile,
		"-sni-route=accounts.example.test=127.0.0.1:9443",
	})
	if err == nil || !strings.Contains(err.Error(), "-tls-cert") {
		t.Fatalf("hosted-only startup error=%v", err)
	}

	badFile := filepath.Join(directory, "bad.pub")
	if err := os.WriteFile(badFile, []byte("not-hex\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-grant-public-key-file=" + badFile}); err == nil ||
		!strings.Contains(err.Error(), "line 1") {
		t.Fatalf("invalid grant key file error=%v", err)
	}
	if err := run([]string{
		"-grant-public-key-file=" + keyFile,
		"-sni-route=missing-address",
	}); err == nil {
		t.Fatal("malformed -sni-route was accepted")
	}
}
