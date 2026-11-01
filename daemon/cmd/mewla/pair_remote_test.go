package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// A failed remote access admission must not leave the phone QR as the hosted
// address in a direct (non-Link) pairing: a phone that scans it connects past
// the pinned Mewla Link route, to the computer's own TLS 1.3 server.
func TestFailedRemoteAdmissionIsNotASilentDirectQR(t *testing.T) {
	const remote = "https://warm-cedar-b69f.m.mewla.ai"
	direct := pairingLinks{phoneAddress: remote + "/", phoneLink: "mewla://settings?p=AQ-direct"}
	admission := errors.New("no relay issued an admission")

	var note bytes.Buffer
	_, err := applyRemotePairing(direct, remote, "", "", admission, &note)
	if err == nil || !strings.Contains(err.Error(), "could not set up phone pairing through remote access") ||
		!strings.Contains(err.Error(), "no relay issued an admission") || !strings.Contains(err.Error(), "mewla pair again") {
		t.Fatalf("a failed admission with the hosted address as the QR: err = %v", err)
	}

	// Wi-Fi ranked first: its QR is a direct pairing the person chose by being
	// on that network, so it stays, with the reason noted.
	lan := pairingLinks{phoneAddress: "http://192.168.0.73:9876", phoneLink: "mewla://settings?p=AQ-lan"}
	note.Reset()
	got, err := applyRemotePairing(lan, remote, "", "", admission, &note)
	if err != nil || got.phoneLink != lan.phoneLink || !strings.Contains(note.String(), "Remote access pairing unavailable") {
		t.Fatalf("lan QR: got %+v err %v note %q", got, err, note.String())
	}

	// Admission worked: the Link QR replaces the direct one.
	got, err = applyRemotePairing(direct, remote, "mewla://settings?v=3&p=AMlink", "Remote access, "+remote, nil, &note)
	if err != nil || got.phoneLink != "mewla://settings?v=3&p=AMlink" || got.phoneAddress != "Remote access, "+remote {
		t.Fatalf("link QR: got %+v err %v", got, err)
	}

	// Remote access off: nothing to apply.
	got, err = applyRemotePairing(direct, "", "", "", nil, &note)
	if err != nil || got.phoneLink != direct.phoneLink {
		t.Fatalf("remote off: got %+v err %v", got, err)
	}
}
