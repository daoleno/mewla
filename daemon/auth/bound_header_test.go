package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The bytes a v2 device header signs, line for line with the app
// (client/core/deviceAuthContract.test.ts "a bound header signs the same bytes").
func TestBoundSignaturePayloadIsLineForLine(t *testing.T) {
	got := string(BuildBoundSignaturePayload(" mewla-connect ", strings.Repeat("AA", 32), " phone ", "1700000000000", strings.Repeat("BB", 16), "host:desk.example"))
	want := "mewla-device-v2\nmewla-connect\n" + strings.Repeat("aa", 32) + "\nphone\n1700000000000\n" + strings.Repeat("bb", 16) + "\nhost:desk.example"
	if got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

func TestBindingGrammar(t *testing.T) {
	good := []string{"host:desk.example", "host:192.168.1.20", "host:fd7a:115c::1", "host:quiet-otter-3f9a.m.mewla.ai", "pin:" + hexOf(32)}
	bad := []string{"", "host:", "host:Desk.example", "host:desk.example:443", "host:[::1]", "host:desk\nexample", "host:desk\x00", "host:desk\u0085", "host:desk example",
		"pin:" + hexOf(31), "pin:" + hexOf(32) + "\n", "pin:ABCD", "desk.example", "url:wss://desk.example"}
	for _, binding := range good {
		if !ValidBinding(binding) {
			t.Errorf("refused %q", binding)
		}
	}
	for _, binding := range bad {
		if ValidBinding(binding) {
			t.Errorf("accepted %q", binding)
		}
	}
}

func hexOf(bytes int) string {
	value := make([]byte, bytes)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func boundHeader(privateKey ed25519.PrivateKey, daemonID, deviceID, purpose, binding string) string {
	timestamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	nonce := hexOf(16)
	signature := ed25519.Sign(privateKey, BuildBoundSignaturePayload(purpose, daemonID, deviceID, timestamp, nonce, binding))
	return AuthorizationHeaderPrefix + "v2:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonce + ":" + base64.RawURLEncoding.EncodeToString([]byte(binding)) + ":" + hex.EncodeToString(signature)
}

// Security review 11-23, PR B item 6: a v2 header is accepted only where the
// server says its binding is the request's; v1 (older apps) still works.
func TestBoundHeaderIsAcceptedOnlyForItsBinding(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "phone", "Phone", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	only := func(want string) BindingCheck { return func(binding string) bool { return binding == want } }

	if _, err := manager.VerifyAuthorization(boundHeader(private, manager.DaemonID(), "phone", "mewla-connect", "host:desk.example"), "mewla-connect", time.Minute, only("host:desk.example")); err != nil {
		t.Fatalf("its own host: %v", err)
	}
	forwarded := boundHeader(private, manager.DaemonID(), "phone", "mewla-connect", "host:evil.example")
	if _, err := manager.VerifyAuthorization(forwarded, "mewla-connect", time.Minute, only("host:desk.example")); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("a header made for another host: %v", err)
	}
	if reason, _ := RefusalReason(func() error {
		_, err := manager.VerifyAuthorization(forwarded, "mewla-connect", time.Minute, only("host:desk.example"))
		return err
	}()); reason != "wrong-host" {
		t.Fatalf("reason = %q", reason)
	}
	// No check (a signature that is not a request's) takes no v2 header.
	if _, err := manager.VerifyAuthorization(boundHeader(private, manager.DaemonID(), "phone", "mewla-connect", "host:desk.example"), "mewla-connect", time.Minute, nil); err == nil {
		t.Fatal("a v2 header passed with no binding check")
	}
	// The binding is signed: rewriting it breaks the signature.
	header := boundHeader(private, manager.DaemonID(), "phone", "mewla-connect", "host:evil.example")
	parsed, _ := parseDeviceHeader(header)
	rewritten := AuthorizationHeaderPrefix + "v2:phone:" + manager.DaemonID() + ":" + parsed.timestamp + ":" + parsed.nonceHex + ":" + base64.RawURLEncoding.EncodeToString([]byte("host:desk.example")) + ":" + parsed.signatureHex
	if _, err := manager.VerifyAuthorization(rewritten, "mewla-connect", time.Minute, only("host:desk.example")); err == nil {
		t.Fatal("a rewritten binding verified")
	}
	// A v1 signature does not read as v2.
	v1 := buildTestAuthorizationHeader(t, private, manager.DaemonID(), "phone", "mewla-connect")
	v1parsed, _ := parseDeviceHeader(v1)
	asV2 := AuthorizationHeaderPrefix + "v2:phone:" + manager.DaemonID() + ":" + v1parsed.timestamp + ":" + v1parsed.nonceHex + ":" + base64.RawURLEncoding.EncodeToString([]byte("host:desk.example")) + ":" + v1parsed.signatureHex
	if _, err := manager.VerifyAuthorization(asV2, "mewla-connect", time.Minute, only("host:desk.example")); err == nil {
		t.Fatal("a v1 signature verified as v2")
	}
	if _, err := manager.VerifyAuthorization(v1, "mewla-connect", time.Minute, only("host:desk.example")); err != nil {
		t.Fatalf("an older app's v1 header: %v", err)
	}
}
