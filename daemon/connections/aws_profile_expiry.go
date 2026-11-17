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
	// An sso-session sign-in renews itself until the organization's session
	// length has passed since the person signed in; AWS keeps neither locally.
	// Its end is known once Mewla started this very sign-in and the length is
	// known (learned or set). The access token's expiry renews hourly and the
	// client registration's is not the sign-in's, so neither is shown.
	if session := fields["sso_session"]; session != "" {
		if cli == nil || cli.SignInAt == nil || cli.SessionLength <= 0 || cli.SignInBinding == "" || cli.SignInBinding != awsSSOBinding(configPath, session) {
			return nil, ""
		}
		end := cli.SignInAt.Add(time.Duration(cli.SessionLength) * time.Second)
		return &end, "session"
	}
	// Legacy nonrefreshable SSO has a usable local reauthentication time.
	key := fields["sso_start_url"]
	if key == "" {
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
	if session := fields["sso_session"]; session != "" {
		// The sign-in is the session's: every profile naming it starts now.
		m.awsSSOSignedIn(session, now)
		return
	}
	r.CLI.LoginAt = &now
	r.CLI.LoginSession = awsLoginSession(config, fields)
	r.Account.ProfileSignInBy, r.Account.ProfileSignInLimit = awsProfileExpiry(config, fields, r.CLI)
}
