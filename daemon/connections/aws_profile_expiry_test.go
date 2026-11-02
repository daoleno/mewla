package connections

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAWSExpiryReadsOnlyMatchingVendorCache(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	sum := sha256.Sum256([]byte("session"))
	dir := filepath.Join(root, "login", "cache")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), []byte(`{"accessToken":{"expiresAt":"2026-11-02T09:00:00Z","secretAccessKey":"never expose"}}`), 0600)
	expiry, source := awsProfileExpiry(config, map[string]string{"login_session": "session"})
	if expiry == nil || expiry.Format(time.RFC3339) != "2026-11-02T09:00:00Z" || source == "" {
		t.Fatal("login expiry missing")
	}
	if expiry, _ = awsProfileExpiry(config, map[string]string{"login_session": "other"}); expiry != nil {
		t.Fatal("unrelated session used")
	}
	if expiry, _ = awsProfileExpiry(config, map[string]string{"aws_access_key_id": "static"}); expiry != nil {
		t.Fatal("invented static expiry")
	}
	sso := sha1.Sum([]byte("work"))
	dir = filepath.Join(root, "sso", "cache")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, hex.EncodeToString(sso[:])+".json"), []byte(`{"expiresAt":"2026-11-03T09:00:00UTC","accessToken":"opaque"}`), 0600)
	if expiry, _ = awsProfileExpiry(config, map[string]string{"sso_session": "work"}); expiry == nil || expiry.Format(time.RFC3339) != "2026-11-03T09:00:00Z" {
		t.Fatal("SSO expiry missing")
	}
}
