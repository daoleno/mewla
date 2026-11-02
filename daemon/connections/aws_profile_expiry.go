package connections

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func awsCacheJSON(path string, value any) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 {
		return false
	}
	raw, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(raw, value) == nil
}

// The ID token is used only to bind local timing metadata to a session, never
// to authenticate it. Its iat/exp and accessToken.expiresAt rotate every 15m.
// Persist a digest of the session ARN, not the token or any credential.
func awsLoginSession(configPath string, fields map[string]string) string {
	key := fields["login_session"]
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	var cache struct {
		IDToken string `json:"idToken"`
	}
	if !awsCacheJSON(filepath.Join(filepath.Dir(configPath), "login", "cache", hex.EncodeToString(sum[:])+".json"), &cache) {
		return ""
	}
	parts := strings.Split(cache.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Session string `json:"session_arn"`
	}
	if err != nil || json.Unmarshal(raw, &claims) != nil || claims.Session == "" {
		return ""
	}
	binding := sha256.Sum256([]byte(key + "\x00" + claims.Session))
	return hex.EncodeToString(binding[:])
}

// Only reauthentication timing belongs in the UI. The documented 12h limit
// is an upper bound: the principal or selected console session can end sooner.
func awsProfileExpiry(configPath string, fields map[string]string, cli *cloudAccount) (*time.Time, string) {
	if fields["login_session"] != "" {
		if cli == nil || cli.LoginAt == nil || cli.LoginSession == "" || cli.LoginSession != awsLoginSession(configPath, fields) {
			return nil, ""
		}
		end := cli.LoginAt.Add(12 * time.Hour)
		return &end, "maximum"
	}
	// Modern sso-session tokens refresh; their expiresAt is not session end.
	// Only legacy nonrefreshable SSO has a usable local reauthentication time.
	key := fields["sso_start_url"]
	if fields["sso_session"] != "" || key == "" {
		return nil, ""
	}
	sum := sha1.Sum([]byte(key))
	var cache struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if !awsCacheJSON(filepath.Join(filepath.Dir(configPath), "sso", "cache", hex.EncodeToString(sum[:])+".json"), &cache) {
		return nil, ""
	}
	parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(cache.ExpiresAt, "UTC", "Z"))
	if err != nil {
		return nil, ""
	}
	return &parsed, "sso"
}

func (awsCLI) loginCompleted(m *Manager, r *record) {
	config, _, err := m.cloud.profileFiles()
	if err != nil {
		return
	}
	raw, err := readOptional(config)
	if err != nil {
		return
	}
	fields := awsSections(raw, true)[r.CLI.Profile]
	now := time.Now().UTC()
	r.CLI.LoginAt = &now
	r.CLI.LoginSession = awsLoginSession(config, fields)
	r.Account.ProfileSignInBy, r.Account.ProfileSignInLimit = awsProfileExpiry(config, fields, r.CLI)
}
