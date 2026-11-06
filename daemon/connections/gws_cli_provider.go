package connections

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// gwsCLI is Google's official Workspace CLI (github.com/googleworkspace/cli).
// It signs in with an OAuth client in the person's own Google Cloud project, so
// neither Mewla nor any Mewla server holds Google keys or sees the mail. The
// CLI's own config directory is the single source of truth.
type gwsCLI struct{}

const gwsProfile = "default"
const gwsServices = "gmail,calendar,drive"

func (gwsCLI) binary() string { return "gws" }
func (gwsCLI) environment() []string {
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		// Keep the vendor's config directory and keyring choice, but never inherit
		// a token, credentials file, client or project override in place of the
		// signed-in account.
		if strings.HasPrefix(key, "GOOGLE_WORKSPACE_") && key != "GOOGLE_WORKSPACE_CLI_CONFIG_DIR" && key != "GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND" ||
			key == "GOOGLE_APPLICATION_CREDENTIALS" || key == "GWS_SANITIZE_TEMPLATE" {
			continue
		}
		env = append(env, v)
	}
	// A computer without a desktop keyring keeps gws's key in its config directory.
	if os.Getenv("GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND") == "" && runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		env = append(env, "GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file")
	}
	return env
}
func (gwsCLI) validProfile(p string) bool { return p == gwsProfile }
func (gwsCLI) defaultProfile() string     { return gwsProfile }

// gws has one account; nothing in argv selects another.
func (gwsCLI) commandArgs(_ string, args []string) []string { return append([]string{}, args...) }

func (gwsCLI) loginArgs(string) []string {
	return []string{"auth", "login", "-s", gwsServices}
}

var gwsRedirect = regexp.MustCompile(`^http://(localhost|127\.0\.0\.1):([0-9]{2,5})/?$`)

// gwsLoopbackPort is the port of the one-shot listener gws opened for this sign-in.
func gwsLoopbackPort(address string) (string, bool) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host != "accounts.google.com" || (u.Path != "/o/oauth2/auth" && u.Path != "/o/oauth2/v2/auth") || u.Query().Get("client_id") == "" || u.Query().Get("response_type") != "code" {
		return "", false
	}
	m := gwsRedirect.FindStringSubmatch(u.Query().Get("redirect_uri"))
	if m == nil {
		return "", false
	}
	return m[2], true
}
func (gwsCLI) loginURL(output string) string {
	for _, s := range cloudURL.FindAllString(output, -1) {
		if _, ok := gwsLoopbackPort(s); ok {
			return s
		}
	}
	return ""
}

var gwsAuthCode = regexp.MustCompile(`^[A-Za-z0-9/_.~-]{8,1000}$`)

// gwsCallback reads the address the browser ended on. Only the code and scope
// are taken from it: the pasted text never chooses the host, port or path the
// daemon requests, and it must name the port gws is listening on.
func gwsCallback(address, pasted string) (code, scope string, ok bool) {
	port, ok := gwsLoopbackPort(address)
	if !ok {
		return "", "", false
	}
	pasted = strings.TrimSpace(pasted)
	if len(pasted) > 4096 || strings.ContainsAny(pasted, "\x00\r\n \t") {
		return "", "", false
	}
	u, err := url.Parse(pasted)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.Port() != port {
		return "", "", false
	}
	q := u.Query()
	code, scope = q.Get("code"), q.Get("scope")
	if q.Get("error") != "" || !gwsAuthCode.MatchString(code) || len(scope) > 2048 {
		return "", "", false
	}
	return code, scope, true
}
func (gwsCLI) validateCode(address, pasted string) bool {
	_, _, ok := gwsCallback(address, pasted)
	return ok
}
func (gwsCLI) pasteError() string {
	return "Paste the full address of the page you land on after signing in. It starts with http://localhost."
}

// gwsDeliverHost is replaced by tests that listen on another loopback address.
var gwsDeliverHost = "127.0.0.1"

// deliverCode hands the authorization code to gws's own loopback listener, as
// the browser would have if it ran on this computer. gws exchanges the code and
// stores the credentials itself.
func (gwsCLI) deliverCode(address, pasted string) error {
	code, scope, ok := gwsCallback(address, pasted)
	port, _ := gwsLoopbackPort(address)
	if !ok {
		return errors.New("Paste the full address of the page you land on after signing in.")
	}
	target := url.URL{Scheme: "http", Host: net.JoinHostPort(gwsDeliverHost, port), Path: "/"}
	q := url.Values{"code": {code}}
	if scope != "" {
		q.Set("scope", scope)
	}
	target.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("This sign-in ended. Start again.")
	}
	resp.Body.Close()
	return nil
}
func (gwsCLI) syncProfiles(m *Manager, ctx context.Context, force bool) error {
	return m.syncGWSProfiles(ctx, force)
}
func (gwsCLI) profileRecord(m *Manager, p string) *record { return m.gwsProfileRecord(p) }

var gwsHelperReads = map[string]bool{"gmail +triage": true, "gmail +read": true, "calendar +agenda": true}
var gwsGetMethod = regexp.MustCompile(`^get[A-Z][A-Za-z]*$`)

// Reads are an allowlist: list/get-style methods, free/busy and the read
// helpers. Anything else, including sending mail, creating or changing events
// and writing to Drive, is a change that needs the person's authorization.
func (gwsCLI) classify(args []string) (bool, error) {
	if len(args) == 0 || len(args) > 256 {
		return false, callRefused("Pass gws arguments as literal words")
	}
	size := 0
	for _, a := range args {
		size += len(a)
		if strings.ContainsAny(a, "\x00\r\n") {
			return false, callRefused("Pass gws arguments as literal words")
		}
	}
	if size > 65536 {
		return false, callRefused("Pass gws arguments as literal words")
	}
	path := []string{}
	flags := false
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = true
			key, _, _ := strings.Cut(a, "=")
			lower := strings.ToLower(key)
			if !strings.HasPrefix(lower, "--") && lower != "-h" && lower != "-v" {
				return false, callRefused("Use long options such as --params")
			}
			for _, blocked := range []string{"upload", "output", "attach", "file", "dir", "token", "credential", "config", "account", "impersonat", "sanitize"} {
				if strings.Contains(lower, blocked) {
					return false, callRefused("This option cannot override the connected account or use local files")
				}
			}
			continue
		}
		if strings.HasPrefix(a, "@") || strings.Contains(a, "file://") {
			return false, callRefused("Local file loading is unavailable through this connection")
		}
		if !flags {
			path = append(path, a)
		}
	}
	if len(path) == 0 {
		for _, a := range args {
			if a != "--help" && a != "-h" && a != "--version" && a != "-V" {
				return false, callRefused("Start with gmail, calendar or drive")
			}
		}
		return true, nil
	}
	service := path[0]
	if service == "schema" {
		return true, nil
	}
	if service != "gmail" && service != "calendar" && service != "drive" {
		return false, callRefused("Google Workspace covers gmail, calendar and drive")
	}
	for _, p := range path {
		if strings.Contains(strings.ToLower(p), "watch") {
			return false, callRefused("Push subscriptions are unavailable through this connection")
		}
	}
	if len(path) == 1 {
		return true, nil // service help
	}
	if strings.HasPrefix(path[1], "+") {
		if service == "drive" && path[1] == "+upload" {
			return false, callRefused("Local file upload is unavailable through this connection")
		}
		return gwsHelperReads[service+" "+path[1]], nil
	}
	last := path[len(path)-1]
	if last == "list" || last == "get" || gwsGetMethod.MatchString(last) {
		return true, nil
	}
	if last == "query" && len(path) > 2 && path[len(path)-2] == "freebusy" {
		return true, nil
	}
	return false, nil
}

func (gwsCLI) installSupport() (string, string) {
	return gwsInstallSupport(runtime.GOOS + "/" + runtime.GOARCH)
}
func (gwsCLI) installPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", "gws"), filepath.Join(home, ".cargo", "bin", "gws"), "/opt/homebrew/bin/gws", "/usr/local/bin/gws"}
}
