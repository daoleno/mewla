package connections

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Read only expiry/account metadata; never expose cache tokens. AWS login's
// timestamp is the rotating credential expiry, not the refresh-session limit.
func awsProfileExpiry(configPath string, fields map[string]string) (*time.Time, string) {
	root := filepath.Dir(configPath)
	key := fields["login_session"]
	var path string
	source := ""
	if key != "" {
		sum := sha256.Sum256([]byte(key))
		path = filepath.Join(root, "login", "cache", hex.EncodeToString(sum[:])+".json")
		source = "AWS login credentials (automatically refreshed)"
	} else if key = firstNonEmpty(fields["sso_session"], fields["sso_start_url"]); key != "" {
		sum := sha1.Sum([]byte(key))
		path = filepath.Join(root, "sso", "cache", hex.EncodeToString(sum[:])+".json")
		source = "SSO token (may refresh automatically)"
	} else {
		return nil, ""
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 {
		return nil, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	var cache struct {
		ExpiresAt   string          `json:"expiresAt"`
		AccessToken json.RawMessage `json:"accessToken"`
	}
	if json.Unmarshal(raw, &cache) != nil {
		return nil, ""
	}
	expiry := cache.ExpiresAt
	if fields["login_session"] != "" {
		var token struct {
			ExpiresAt string `json:"expiresAt"`
		}
		if json.Unmarshal(cache.AccessToken, &token) != nil {
			return nil, ""
		}
		expiry = token.ExpiresAt
	}
	expiry = strings.TrimSuffix(expiry, "UTC") + func() string {
		if strings.HasSuffix(expiry, "UTC") {
			return "Z"
		}
		return ""
	}()
	parsed, err := time.Parse(time.RFC3339, expiry)
	if err != nil {
		return nil, ""
	}
	return &parsed, source
}
