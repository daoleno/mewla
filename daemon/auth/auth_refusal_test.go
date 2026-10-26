package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A refused request says why, to the owner's log, without the client's
// response changing: every refusal still matches the error it always did.
func TestVerifyAuthorizationNamesWhyARequestWasRefused(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "device-1", "phone", hex.EncodeToString(publicKey)); err != nil {
		t.Fatal(err)
	}
	_, strangerKey, _ := ed25519.GenerateKey(rand.Reader)
	sign := func(key ed25519.PrivateKey, deviceID, daemonID, purpose string, at time.Time) string {
		nonceHex, _ := randomHex(16)
		timestamp := strconv.FormatInt(at.UnixMilli(), 10)
		signature := ed25519.Sign(key, BuildSignaturePayload(purpose, daemonID, deviceID, timestamp, nonceHex))
		return AuthorizationHeaderPrefix + "v1:" + deviceID + ":" + daemonID + ":" + timestamp + ":" + nonceHex + ":" + hex.EncodeToString(signature)
	}
	daemonID := manager.DaemonID()
	now := time.Now()

	cases := []struct {
		name       string
		header     string
		purpose    string
		is         error
		reason     string
		detailHas  string
		detailNone string
	}{
		{"no header", "", "mewla-probe", ErrUnauthorized, "malformed-header", "", ""},
		{"not ours", "Bearer abc", "mewla-probe", ErrUnauthorized, "malformed-header", "", ""},
		{"another computer", sign(privateKey, "device-1", strings.Repeat("ab", 32), "mewla-probe", now), "mewla-probe", ErrWrongDaemon, "wrong-daemon", "", ""},
		{"clock ahead", sign(privateKey, "device-1", daemonID, "mewla-probe", now.Add(20*time.Minute)), "mewla-probe", ErrUnauthorized, "clock-skew", "+1", ""},
		{"clock behind", sign(privateKey, "device-1", daemonID, "mewla-probe", now.Add(-time.Hour)), "mewla-probe", ErrUnauthorized, "clock-skew", "-3", ""},
		{"unknown device", sign(privateKey, "device-9", daemonID, "mewla-probe", now), "mewla-probe", ErrUnknownDevice, "unknown-device", "", ""},
		{"someone else's key", sign(strangerKey, "device-1", daemonID, "mewla-probe", now), "mewla-probe", ErrUnauthorized, "bad-signature", "", ""},
		{"signed for another purpose", sign(privateKey, "device-1", daemonID, "mewla-other", now), "mewla-probe", ErrUnauthorized, "bad-signature", "", ""},
		{"computer purpose from a phone", sign(privateKey, "device-1", daemonID, ComputerPurpose, now), ComputerPurpose, ErrUnauthorized, "wrong-purpose", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := manager.VerifyAuthorization(tc.header, tc.purpose, 5*time.Minute)
			if !errors.Is(err, tc.is) {
				t.Fatalf("error = %v, want %v", err, tc.is)
			}
			reason, detail := RefusalReason(err)
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
			if tc.detailHas != "" && !strings.Contains(detail, tc.detailHas) {
				t.Fatalf("detail = %q, want it to contain %q", detail, tc.detailHas)
			}
			// What the client is told is what it was always told.
			if err.Error() != tc.is.Error() {
				t.Fatalf("response text = %q, want %q", err.Error(), tc.is.Error())
			}
		})
	}

	t.Run("replay says how soon after the first", func(t *testing.T) {
		header := sign(privateKey, "device-1", daemonID, "mewla-probe", time.Now())
		if _, err := manager.VerifyAuthorization(header, "mewla-probe", time.Minute); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
		_, err := manager.VerifyAuthorization(header, "mewla-probe", time.Minute)
		if !errors.Is(err, ErrReplayDetected) {
			t.Fatalf("error = %v, want replay", err)
		}
		reason, detail := RefusalReason(err)
		if reason != "replay" || !strings.Contains(detail, "first accepted") || !strings.HasSuffix(detail, "ms ago") {
			t.Fatalf("reason=%q detail=%q", reason, detail)
		}
	})

	t.Run("an accepted request has no refusal", func(t *testing.T) {
		if reason, _ := RefusalReason(nil); reason != "" {
			t.Fatalf("reason for nil = %q", reason)
		}
		if reason, _ := RefusalReason(ErrUnauthorized); reason != "" {
			t.Fatalf("reason for a bare error = %q", reason)
		}
	})
}

func TestClaimedDeviceIDReadsOnlyTheDeviceID(t *testing.T) {
	header := AuthorizationHeaderPrefix + "v1:device-7:" + strings.Repeat("ab", 32) + ":1:" + strings.Repeat("cd", 16) + ":" + strings.Repeat("ef", 64)
	if got := ClaimedDeviceID(header); got != "device-7" {
		t.Fatalf("ClaimedDeviceID = %q", got)
	}
	for _, bad := range []string{"", "Bearer x", AuthorizationHeaderPrefix + "v2:a:b:c:d:e"} {
		if got := ClaimedDeviceID(bad); got != "" {
			t.Fatalf("ClaimedDeviceID(%q) = %q, want empty", bad, got)
		}
	}
}
