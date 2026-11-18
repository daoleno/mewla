package screencast

import (
	"sort"
	"strings"
)

// Fingerprints are the DTLS certificate fingerprints an SDP names, each as
// "<hash> <HEX:HEX:…>" (lowercase hash, uppercase hex), sorted: what each
// side signs so the other knows whose certificate the media is encrypted to.
// The DTLS handshake then refuses any certificate that does not match.
func Fingerprints(sdp string) []string {
	var out []string
	for _, line := range strings.Split(sdp, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "a=fingerprint:")
		if !ok {
			continue
		}
		hash, hexValue, ok := strings.Cut(strings.TrimSpace(value), " ")
		if !ok {
			continue
		}
		fp := strings.ToLower(strings.TrimSpace(hash)) + " " + strings.ToUpper(strings.TrimSpace(hexValue))
		if !contains(out, fp) {
			out = append(out, fp)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
