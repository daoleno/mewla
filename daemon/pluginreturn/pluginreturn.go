// Package pluginreturn is Mewla's https return page for plugin sign-ins whose
// provider refuses the app's mewla:// scheme. It runs on Mewla's own origin,
// never on a user's daemon, and only hands the authorization result on, to the
// app or to the web UI that started the sign-in: the code is useless without
// the PKCE verifier the daemon keeps.
package pluginreturn

import (
	"encoding/base64"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Origin is where cmd/mewla-account serves Handler.
const Origin = "https://connect.mewla.ai"

// Path is where cmd/mewla-account serves Handler.
const Path = "/plugins/return"

// Target is the app's fixed plugin return. Nothing in a request changes it.
const Target = "mewla://plugins"

// WebCallbackPath is where a web UI origin takes a sign-in return.
const WebCallbackPath = "/plugins/oauth/callback"

// A sign-in a browser started from the web UI can't be handed to mewla://.
// Its state is "web.", the web UI's origin in base64url, ".", then the
// daemon's own random state. What happens next depends on that origin:
//
//   - A loopback origin (http://127.0.0.1, [::1] or localhost, with a port):
//     the page redirects the same tab to its /plugins/oauth/callback. Only a
//     browser on the visitor's own computer can reach that address, so the
//     code can't go anywhere else.
//   - Any other origin: the page shows the returned address and the person
//     pastes it into their own Mewla. It must never forward the code, and it
//     must not postMessage it to an opener either. The origin comes from a
//     link anyone can craft: an attacker who starts a Slack sign-in on their
//     own server (so they hold the PKCE verifier) could send the authorize link
//     to a victim and have this page hand the victim's approved code to the
//     attacker's server. The app's mewla:// return is safe because it lands on
//     the victim's own device and fails the state check there.
const webStatePrefix = "web."

// WebState is the state a web UI sign-in sends: origin, then the daemon's state.
func WebState(origin, state string) string {
	return webStatePrefix + base64.RawURLEncoding.EncodeToString([]byte(origin)) + "." + state
}

var loopbackOrigin = regexp.MustCompile(`^http://(127\.0\.0\.1|\[::1\]|localhost):([1-9][0-9]{0,4})$`)

// IsLoopbackOrigin reports whether origin is exactly a loopback http origin
// with a port: no userinfo, path, other spelling of the address (decimal,
// octal, a longer host that merely starts with one) or port beyond 65535.
func IsLoopbackOrigin(origin string) bool {
	m := loopbackOrigin.FindStringSubmatch(origin)
	if m == nil {
		return false
	}
	port, _ := strconv.Atoi(m[2])
	return port <= 65535
}

// webOrigin reads the origin out of a web state, and whether it is loopback.
func webOrigin(state string) (string, bool) {
	rest, ok := strings.CutPrefix(state, webStatePrefix)
	if !ok {
		return "", false
	}
	encoded, _, ok := strings.Cut(rest, ".")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if !ok || err != nil || len(raw) > 256 || !IsLoopbackOrigin(string(raw)) {
		return "", false
	}
	return string(raw), true
}

var errorCode = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Handler forwards state with code or error (and the issuer, which keeps the
// daemon's mix-up check) to Target. Every other parameter is dropped. It
// writes no log: a code is a bearer value until the daemon redeems it.
func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(r.URL.RawQuery) > 16384 {
		http.Error(w, "Invalid sign-in return", http.StatusBadRequest)
		return
	}
	forward := url.Values{}
	for _, key := range []string{"code", "state", "error", "iss"} {
		switch values := query[key]; len(values) {
		case 0:
		case 1:
			forward.Set(key, values[0])
		default:
			http.Error(w, "Invalid sign-in return", http.StatusBadRequest)
			return
		}
	}
	state, code, failure, issuer := forward.Get("state"), forward.Get("code"), forward.Get("error"), forward.Get("iss")
	valid := state != "" && len(state) <= 256 && len(code) <= 8192 && (code != "") != (failure != "")
	if failure != "" && !errorCode.MatchString(failure) {
		valid = false
	}
	if issuer != "" {
		parsed, err := url.Parse(issuer)
		valid = valid && err == nil && len(issuer) <= 512 && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
	}
	if !valid {
		http.Error(w, "Invalid sign-in return. Go back to Mewla and connect again.", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(state, webStatePrefix) {
		if origin, ok := webOrigin(state); ok {
			http.Redirect(w, r, origin+WebCallbackPath+"?"+forward.Encode(), http.StatusFound)
			return
		}
		showAddress(w, Origin+Path+"?"+forward.Encode(), failure != "")
		return
	}
	http.Redirect(w, r, Target+"?"+forward.Encode(), http.StatusFound)
}

// showAddress is the page a web sign-in to a non-loopback server ends on. The
// address is selected by a single click and the page runs no script.
func showAddress(w http.ResponseWriter, address string, denied bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page's own style only; no script, image or frame is allowed.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	lead := "Copy this address, go back to Mewla and paste it."
	if denied {
		lead = "Access was not granted. Copy this address and paste it in Mewla to finish."
	}
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Back to Mewla</title><style>` +
		`body{font:16px/1.5 system-ui,sans-serif;margin:0;padding:48px 20px;background:#fff;color:#1b1b1f}` +
		`main{max-width:560px;margin:0 auto}h1{font-size:22px;margin:0 0 8px}p{margin:0 0 20px;color:#55555c}p.warn{color:#9a3412}` +
		`code{display:block;padding:14px;border:1px solid #d6d6dc;border-radius:12px;background:#f6f6f8;word-break:break-all;user-select:all;-webkit-user-select:all;font:13px/1.5 ui-monospace,monospace}` +
		`@media(prefers-color-scheme:dark){body{background:#111114;color:#f2f2f5}p{color:#a8a8b0}p.warn{color:#fdba74}code{background:#1c1c21;border-color:#34343b}}` +
		`</style></head><body><main><h1>Back to Mewla</h1><p>` + html.EscapeString(lead) + `</p><p class="warn">Only paste this into your own Mewla.</p><code>` + html.EscapeString(address) + `</code></main></body></html>`))
}
