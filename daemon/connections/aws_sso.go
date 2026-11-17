package connections

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// awsSSOProfile is a profile that signs in through IAM Identity Center. Its
// sign-in is `aws sso login`, never `aws login`: `aws login` would write a
// login_session into the profile and cap it at 12 hours.
func awsSSOProfile(fields map[string]string) bool {
	return fields["sso_session"] != "" || fields["sso_start_url"] != ""
}

// loginArgsFor picks the sign-in the profile's kind needs. An SSO profile signs
// in with a device code: the person approves a code on any device and nothing
// is pasted back; the CLI exits once AWS confirms.
func (p awsCLI) loginArgsFor(m *Manager, profile string) []string {
	if config, _, err := m.cloud.profileFiles(); err == nil {
		if raw, err := readOptional(config); err == nil && awsSSOProfile(awsSections(raw, true)[profile]) {
			return []string{"sso", "login", "--use-device-code", "--no-browser", "--profile", profile, "--no-cli-auto-prompt"}
		}
	}
	return p.loginArgs(profile)
}

var awsUserCode = regexp.MustCompile(`^[A-Z0-9]{4,8}(-[A-Z0-9]{4,8})?$`)

// deviceCode reads the address and code `aws sso login --use-device-code`
// prints: the code on a line of its own after "enter the code", and the
// address that fills it in (the last AWS address printed; older CLIs carry
// ?user_code= in it, AWS CLI 2.40 prints a signin.aws request_uri).
func (awsCLI) deviceCode(output string) (string, string) {
	code := ""
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if !strings.Contains(strings.ToLower(line), "enter the code") {
			continue
		}
		for _, next := range lines[i+1:] {
			if next = strings.TrimSpace(next); next != "" {
				if awsUserCode.MatchString(next) {
					code = next
				}
				break
			}
		}
	}
	address := ""
	for _, s := range cloudURL.FindAllString(output, -1) {
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" {
			continue
		}
		host := u.Hostname()
		if !strings.HasSuffix(host, ".amazonaws.com") && !strings.HasSuffix(host, ".awsapps.com") && !strings.HasSuffix(host, ".app.aws") && !strings.HasSuffix(host, ".signin.aws") {
			continue
		}
		inURL := u.Query().Get("user_code")
		if _, query, ok := strings.Cut(u.Fragment, "?"); ok && inURL == "" {
			values, _ := url.ParseQuery(query)
			inURL = values.Get("user_code")
		}
		if inURL != "" && (!awsUserCode.MatchString(inURL) || code != "" && inURL != code) {
			continue
		}
		if code == "" && inURL != "" {
			code = inURL
		}
		address = s
	}
	if address == "" || code == "" {
		return "", ""
	}
	return address, code
}

// awsSSOSessionEnd is the latest an sso-session sign-in can last. The CLI
// renews its token with a refresh token the Identity Center session bounds,
// and only through a client registration that ends at registrationExpiresAt.
// Neither the session's own length nor its end is stored locally, so this is
// an upper bound: Identity Center's session duration (8 hours by default, up
// to 90 days) can end it sooner.
func awsSSOSessionEnd(configPath, session string) *time.Time {
	sum := sha1.Sum([]byte(session))
	var cache struct {
		RegistrationExpiresAt string `json:"registrationExpiresAt"`
		RefreshToken          string `json:"refreshToken"`
		ExpiresAt             string `json:"expiresAt"`
	}
	if !awsCacheJSON(filepath.Join(filepath.Dir(configPath), "sso", "cache", hex.EncodeToString(sum[:])+".json"), &cache) {
		return nil
	}
	at := cache.RegistrationExpiresAt
	if cache.RefreshToken == "" {
		// Without a refresh token the access token's end is the session's end.
		at = cache.ExpiresAt
	}
	parsed, err := time.Parse(time.RFC3339, strings.ReplaceAll(at, "UTC", "Z"))
	if err != nil {
		return nil
	}
	return &parsed
}
