package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/auth"
)

// appSubprotocolPrefix starts the WebSocket subprotocol an app offers, beside
// its auth subprotocol, to say which app it is: mewla.app.<platform>.<version>
// ("mewla.app.android.0.4.6"). The daemon never selects it; one from before
// ignores it. `mewla devices list` shows what each device said, so it is
// known when no paired device still runs an app that needs an old path.
const appSubprotocolPrefix = "mewla.app."

var appPlatforms = map[string]bool{"android": true, "ios": true, "web": true, "desktop": true}

// clientAppFromRequest is what the app connection's handshake says about the
// app. An app that names none and signs in with an Authorization header is the
// Expo app (before 0.4); every app since signs in with the mewla.auth.
// subprotocol.
func clientAppFromRequest(r *http.Request) auth.ClientApp {
	app := auth.ClientApp{AppLegacy: strings.TrimSpace(r.Header.Get("Authorization")) != ""}
	for _, protocol := range websocket.Subprotocols(r) {
		rest, ok := strings.CutPrefix(protocol, appSubprotocolPrefix)
		if !ok {
			continue
		}
		platform, version, ok := strings.Cut(rest, ".")
		if !ok || !appPlatforms[platform] || !validAppVersion(version) {
			continue
		}
		return auth.ClientApp{AppVersion: version, AppPlatform: platform}
	}
	return app
}

// validAppVersion admits a SemVer-shaped version ("0.4.6", "0.4.7-beta.1"),
// short and in a token's characters, so a device cannot store anything else.
func validAppVersion(version string) bool {
	if version == "" || len(version) > 32 || version[0] < '0' || version[0] > '9' {
		return false
	}
	for _, c := range version {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-' || c == '+') {
			return false
		}
	}
	return true
}
