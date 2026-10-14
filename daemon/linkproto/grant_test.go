package linkproto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func testGrant(t *testing.T) (RouteGrant, ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	daemonKey, _, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now()
	return RouteGrant{
		AccountID:       "acct_1",
		MachineID:       "mach_1",
		RouteID:         strings.Repeat("ab", 16),
		Label:           "studio-3f9a2c",
		DaemonPublicKey: hex.EncodeToString(daemonKey),
		IssuedAtMS:      now.UnixMilli(),
		ExpiresAtMS:     now.Add(time.Hour).UnixMilli(),
	}, private, public
}

func TestRouteGrantRoundTripAndRejections(t *testing.T) {
	grant, private, public := testGrant(t)
	token, err := SignRouteGrant(private, grant)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyRouteGrant([]ed25519.PublicKey{public}, token, time.Now())
	if err != nil || got.Label != grant.Label || got.Version != RouteGrantVersion {
		t.Fatalf("verify = %#v, %v", got, err)
	}
	otherPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := VerifyRouteGrant([]ed25519.PublicKey{otherPublic}, token, time.Now()); err == nil {
		t.Fatal("grant verified under a foreign service key")
	}
	if _, err := VerifyRouteGrant([]ed25519.PublicKey{public}, token, time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("expired grant verified")
	}
	tampered := []byte(token)
	tampered[3] ^= 1
	if _, err := VerifyRouteGrant([]ed25519.PublicKey{public}, string(tampered), time.Now()); err == nil {
		t.Fatal("tampered grant verified")
	}
}

func TestHostLabelsNeverLookLikeOpaqueIDs(t *testing.T) {
	for _, label := range []string{"studio-3f9a2c", "a-b", "mac-mini-01"} {
		if !IsHostLabel(label) {
			t.Fatalf("%q rejected", label)
		}
	}
	for _, label := range []string{strings.Repeat("ab", 16), "-ab", "ab-", "Studio-1", "a_b-c", "ab", strings.Repeat("a-", 40)} {
		if IsHostLabel(label) {
			t.Fatalf("%q accepted", label)
		}
	}
}
