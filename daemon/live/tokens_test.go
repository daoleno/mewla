package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenIssueVerifyRevoke(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	token, entry, err := store.Issue("device-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, TokenPrefix) || entry.DeviceID != "device-1" {
		t.Fatalf("token %q entry %+v", token, entry)
	}
	if got, ok := store.Verify(token); !ok || got.ID != entry.ID {
		t.Fatal("a fresh token must verify")
	}
	for _, bad := range []string{"", "garbage", TokenPrefix, TokenPrefix + entry.ID + ".", token + "0", strings.Replace(token, ".", ".0", 1), "Bearer " + token} {
		if _, ok := store.Verify(bad); ok {
			t.Fatalf("%q must not verify", bad)
		}
	}
	if !store.Revoke(entry.ID) {
		t.Fatal("revoke")
	}
	if _, ok := store.Verify(token); ok {
		t.Fatal("a revoked token must stop verifying")
	}
	if store.Revoke(entry.ID) {
		t.Fatal("revoking twice reports nothing removed")
	}
}

func TestTokenOnePerDeviceAndDeviceRevocation(t *testing.T) {
	store, _ := OpenTokenStore(t.TempDir())
	first, _, _ := store.Issue("a")
	second, _, _ := store.Issue("a")
	other, _, _ := store.Issue("b")
	if _, ok := store.Verify(first); ok {
		t.Fatal("issuing again must revoke the device's earlier token")
	}
	if _, ok := store.Verify(second); !ok {
		t.Fatal("the newest token verifies")
	}
	if n := store.RevokeDevice("a"); n != 1 {
		t.Fatalf("revoked %d", n)
	}
	if _, ok := store.Verify(second); ok {
		t.Fatal("device revocation must kill its token")
	}
	if _, ok := store.Verify(other); !ok {
		t.Fatal("another device's token is untouched")
	}
}

func TestTokenStorePersistsHashOnly(t *testing.T) {
	dir := t.TempDir()
	store, _ := OpenTokenStore(dir)
	token, _, _ := store.Issue("a")
	raw, err := os.ReadFile(filepath.Join(dir, tokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.SplitN(token, ".", 2)[1]
	if strings.Contains(string(raw), secret) {
		t.Fatal("the plaintext secret must never be written")
	}
	info, _ := os.Stat(filepath.Join(dir, tokenFileName))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	reloaded, err := OpenTokenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Verify(token); !ok {
		t.Fatal("a token must survive a restart")
	}
	reloaded.Revoke(strings.TrimPrefix(strings.SplitN(token, ".", 2)[0], TokenPrefix))
	again, _ := OpenTokenStore(dir)
	if _, ok := again.Verify(token); ok {
		t.Fatal("a revocation must survive a restart")
	}
}
