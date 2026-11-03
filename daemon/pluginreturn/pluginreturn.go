// Package pluginreturn is Mewla's https return page for plugin sign-ins whose
// provider refuses the app's mewla:// scheme. It runs on Mewla's own origin,
// never on a user's daemon, and only hands the authorization result to the
// app: the code is useless without the PKCE verifier the daemon keeps.
package pluginreturn

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Origin is where cmd/mewla-account serves Handler.
const Origin = "https://connect.mewla.ai"

// Path is where cmd/mewla-account serves Handler.
const Path = "/plugins/return"

// Target is the app's fixed plugin return. Nothing in a request changes it.
const Target = "mewla://plugins"

// WebStatePrefix marks a sign-in a browser started from the web UI. The app
// can open Target; a browser tab can't, so for these the page shows the
// returned address instead, for the person to paste into the sign-in. The
// address stays in their own browser and is useless without the PKCE verifier
// the daemon keeps.
const WebStatePrefix = "web."

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
	if strings.HasPrefix(state, WebStatePrefix) {
		showAddress(w, Origin+Path+"?"+forward.Encode(), failure != "")
		return
	}
	http.Redirect(w, r, Target+"?"+forward.Encode(), http.StatusFound)
}

// showAddress is the page a web sign-in ends on. The address is selected by a
// single click and the page runs no script.
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
		`main{max-width:560px;margin:0 auto}h1{font-size:22px;margin:0 0 8px}p{margin:0 0 20px;color:#55555c}` +
		`code{display:block;padding:14px;border:1px solid #d6d6dc;border-radius:12px;background:#f6f6f8;word-break:break-all;user-select:all;-webkit-user-select:all;font:13px/1.5 ui-monospace,monospace}` +
		`@media(prefers-color-scheme:dark){body{background:#111114;color:#f2f2f5}p{color:#a8a8b0}code{background:#1c1c21;border-color:#34343b}}` +
		`</style></head><body><main><h1>Back to Mewla</h1><p>` + html.EscapeString(lead) + `</p><code>` + html.EscapeString(address) + `</code></main></body></html>`))
}
