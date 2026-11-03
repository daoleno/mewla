// Package pluginreturn is Mewla's https return page for plugin sign-ins whose
// provider refuses the app's mewla:// scheme. It runs on Mewla's own origin,
// never on a user's daemon, and only hands the authorization result on, to the
// app or to the web UI that started the sign-in: the code is useless without
// the PKCE verifier the daemon keeps.
package pluginreturn

import (
	"encoding/base64"
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

// WebCallbackPath is where a web UI origin takes a sign-in return.
const WebCallbackPath = "/plugins/oauth/callback"

// A sign-in a browser started from the web UI can't be handed to mewla://.
// Its state names the origin to send the browser back to: "web.", the origin
// in base64url, ".", then the daemon's own random state. The provider echoes
// the state untouched, and a browser tab can't be reached from here (Slack's
// pages drop window.opener), so the page redirects the same tab.
//
// Naming the origin is safe because the redirect carries only what the
// provider returned, to one fixed path, and the daemon redeems a code only
// for a state it minted, with a PKCE verifier that never leaves it. A crafted
// state can at most bounce a visitor to that path on an https (or loopback)
// origin with parameters the sender already holds.
const webStatePrefix = "web."

// WebState is the state a web UI sign-in sends: origin, then the daemon's state.
func WebState(origin, state string) string {
	return webStatePrefix + base64.RawURLEncoding.EncodeToString([]byte(origin)) + "." + state
}

// webOrigin reads the origin out of a web state.
func webOrigin(state string) (string, bool) {
	rest, ok := strings.CutPrefix(state, webStatePrefix)
	if !ok {
		return "", false
	}
	encoded, _, ok := strings.Cut(rest, ".")
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if !ok || err != nil || len(raw) > 256 {
		return "", false
	}
	u, err := url.Parse(string(raw))
	host := ""
	if err == nil {
		host = u.Hostname()
	}
	valid := err == nil && u.Scheme+"://"+u.Host == string(raw) && u.User == nil && host != "" &&
		(u.Scheme == "https" || u.Scheme == "http" && (host == "127.0.0.1" || host == "::1"))
	return string(raw), valid
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
		origin, ok := webOrigin(state)
		if !ok {
			http.Error(w, "Invalid sign-in return. Go back to Mewla and connect again.", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, origin+WebCallbackPath+"?"+forward.Encode(), http.StatusFound)
		return
	}
	http.Redirect(w, r, Target+"?"+forward.Encode(), http.StatusFound)
}
