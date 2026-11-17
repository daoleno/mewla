package connections

import (
	"net/url"
	"regexp"
	"strings"
)

// awsSSOProfile is a profile that signs in through IAM Identity Center. Its
// sign-in is `aws sso login`, never `aws login`: `aws login` would write a
// login_session into the profile and cap it at 12 hours.
func awsSSOProfile(fields map[string]string) bool {
	return fields["sso_session"] != "" || fields["sso_start_url"] != ""
}

// loginArgsFor picks the sign-in the flow needs. An Identity Center sign-in
// (adding accounts from a session, or an SSO profile signing in again) uses a
// device code: the person approves a code on any device and nothing is pasted
// back; the CLI exits once AWS confirms.
func (p awsCLI) loginArgsFor(m *Manager, f *connectFlow) []string {
	if f.ssoAdd {
		return []string{"sso", "login", "--use-device-code", "--no-browser", "--sso-session", f.ssoSession, "--no-cli-auto-prompt"}
	}
	if config, _, err := m.cloud.profileFiles(); err == nil {
		if raw, err := readOptional(config); err == nil {
			if fields := awsSections(raw, true)[f.profile]; awsSSOProfile(fields) {
				f.ssoSession = fields["sso_session"]
				return []string{"sso", "login", "--use-device-code", "--no-browser", "--profile", f.profile, "--no-cli-auto-prompt"}
			}
		}
	}
	return p.loginArgs(f.profile)
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
