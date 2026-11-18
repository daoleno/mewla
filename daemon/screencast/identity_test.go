package screencast

import (
	"reflect"
	"testing"

	"github.com/daoleno/mewla/daemon/auth"
)

// The fingerprints and the purpose both sides sign, pinned to the same values
// as the app's (client/core/screen/screenIdentity.test.ts).
func TestFingerprintsAndTheirPurpose(t *testing.T) {
	sdp := "v=0\r\na=fingerprint:SHA-256 ab:cd:01\r\nm=video\r\na=fingerprint:sha-256 AB:CD:01\r\na=fingerprint:sha-1 ff:00\r\n"
	fps := Fingerprints(sdp)
	if want := []string{"sha-1 FF:00", "sha-256 AB:CD:01"}; !reflect.DeepEqual(fps, want) {
		t.Fatalf("fingerprints %q, want %q", fps, want)
	}
	if got, want := auth.ScreenDTLSPurpose(fps), "mewla-screen-dtls:sha256=52c2bfeb09d883ca02cca46379637625422a008cf4c5d1136a9ef5a5b1982e70"; got != want {
		t.Fatalf("purpose %s, want %s", got, want)
	}
	if len(Fingerprints("v=0\r\n")) != 0 {
		t.Fatal("an SDP with no certificate named one")
	}
}
