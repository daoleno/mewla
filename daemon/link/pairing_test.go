package link

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

func TestPairingBindsRoutePinCandidatesAndEnrollment(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	identity, err := LoadOrCreateTransportIdentity(t.TempDir(), []string{"link.test"})
	if err != nil {
		t.Fatalf("LoadOrCreateTransportIdentity: %v", err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatalf("IssuePairingToken: %v", err)
	}
	config, err := normalizeConnectorConfig(ConnectorConfig{
		ConnectorToken: "0123456789abcdef0123456789abcdef",
		Candidates: []RelayCandidate{
			{
				Name:              "region-b",
				ControlAddress:    "region-b.test:8443",
				ControlServerName: "region-b.test",
				ClientDomain:      "b.link.test",
				ClientPort:        443,
			},
			{
				Name:              "region-a",
				ControlAddress:    "region-a.test:8443",
				ControlServerName: "region-a.test",
				ClientDomain:      "a.link.test",
				ClientPort:        443,
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizeConnectorConfig: %v", err)
	}
	pairingLink, payload, err := BuildPairingLink(
		manager,
		identity,
		config,
		pairing,
		[]AdmissionOffer{{
			RelayName:    "region-a",
			AdmissionURL: "wss://11111111111111111111111111111111.a.link.test/ws",
		}},
	)
	if err != nil {
		t.Fatalf("BuildPairingLink: %v", err)
	}
	if err := ValidatePairingPayload(payload, time.Now()); err != nil {
		t.Fatalf("ValidatePairingPayload: %v", err)
	}
	if payload.Version != PairingVersion ||
		payload.RouteID != identity.RouteID ||
		payload.TransportPin != identity.SPKISHA256 ||
		payload.EnrollmentToken != pairing.Value ||
		len(payload.Candidates) != 2 {
		t.Fatalf("unexpected pairing payload: %#v", payload)
	}
	if payload.Candidates[0].Name != "region-a" ||
		payload.Candidates[0].AdmissionURL == "" ||
		!strings.Contains(payload.Candidates[1].StableURL, identity.RouteID) {
		t.Fatalf("unexpected sorted candidates: %#v", payload.Candidates)
	}

	compactPrefix := "mewla://settings?v=3&p="
	if !strings.HasPrefix(pairingLink, compactPrefix) {
		t.Fatalf("unexpected pairing link: %s", pairingLink)
	}
	compact, err := DecodeCompactPairing(strings.TrimPrefix(pairingLink, compactPrefix))
	if err != nil || !reflect.DeepEqual(compact, payload) {
		t.Fatalf("compact link = %#v, %v", compact, err)
	}

	tampered := payload
	tampered.TransportPin = strings.Repeat("a", 64)
	if tampered.TransportPin == payload.TransportPin {
		tampered.TransportPin = strings.Repeat("b", 64)
	}
	if err := ValidatePairingPayload(tampered, time.Now()); err == nil {
		t.Fatal("tampered transport pin passed daemon identity proof")
	}
	if err := ValidatePairingPayload(payload, pairing.ExpiresAt.Add(time.Second)); err == nil {
		t.Fatal("expired pairing payload was accepted")
	}
}

func TestBuildPairingLinkFailsWhenTheCompactLinkCannotCarryTheRelays(t *testing.T) {
	manager, err := auth.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := LoadOrCreateTransportIdentity(t.TempDir(), []string{"link.test"})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := manager.IssuePairingToken(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var config ConnectorConfig
	var offers []AdmissionOffer
	for index := 0; index <= compactMaxCandidates; index++ {
		name := fmt.Sprintf("relay-%02d", index)
		config.Candidates = append(config.Candidates, RelayCandidate{Name: name, ClientDomain: "link.test", ClientPort: 443})
		offers = append(offers, AdmissionOffer{
			RelayName:    name,
			AdmissionURL: "wss://11111111111111111111111111111111.link.test/ws",
		})
	}
	if _, _, err := BuildPairingLink(manager, identity, config, pairing, offers); err == nil {
		t.Fatal("more relays than the compact link carries produced a pairing link")
	}
}
