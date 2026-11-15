package watcher

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// newOriginNonce is the private part of a Session's origin marker.
func newOriginNonce() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// originMarkerCommand prints the marker, concealed (SGR 8), as the very first
// output of the Session, before the login shell and anything its startup
// prints.
func originMarkerCommand(nonce string) string {
	return "printf '\\033[8m%s\\033[0m\\n' " + shellquote.Quote(classifier.OriginMarkerLine(nonce)) + "; "
}
