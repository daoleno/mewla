// Package pluginreturn is Mewla's https return page for plugin sign-ins whose
// provider refuses the app's mewla:// scheme. It runs on Mewla's own origin,
// never on a user's daemon, and only hands the authorization result to the
// app: the code is useless without the PKCE verifier the daemon keeps.
package pluginreturn

import (
	"net/http"
	"net/url"
	"regexp"
)

// Path is where cmd/mewla-account serves Handler.
const Path = "/plugins/return"

// Target is the app's fixed plugin return. Nothing in a request changes it.
const Target = "mewla://plugins"

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
	http.Redirect(w, r, Target+"?"+forward.Encode(), http.StatusFound)
}
