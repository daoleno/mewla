package link

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// compactGolden is shared with app/services/connection.test.ts, so the Go
// encoder and the app decoder cannot drift apart.
const compactGolden = "AN43KVROR7TFJ6KAPCYRF2FJROTZAH4FHLTJLPWX4DRZCC5NASLGIIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCGMZTGMZTGMZTGMZTGMZTGMZTGNCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIRCEIA53FTB5QABK2LOJ5ZDR7UD6VSZJ3GNTCZ2SAFQAX6EXGTCUOC62XJM3WVZS7AMVPXIYOOBR7W373DWAZR4Z5XNYQSIWIZL2J6YTNTDXCT5323VAWEDROVUWK5BNN52HIZLSFUZWMOLBAECG2YLJNYFG2LTNMV3WYYJOMFUQDOYBKVKVKVKVKVKVKVKVKVKVKVKVKU"

func compactFixture(t *testing.T) PairingPayload {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	key := ed25519.NewKeyFromSeed(seed)
	payload := PairingPayload{
		Version:         PairingVersion,
		DaemonPublicKey: hex.EncodeToString(key.Public().(ed25519.PublicKey)),
		EnrollmentToken: strings.Repeat("22", 32),
		RouteID:         strings.Repeat("33", 16),
		TransportPin:    strings.Repeat("44", 32),
		ExpiresAtMS:     time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
		Candidates: []PairingCandidate{{
			Name:         "main",
			AdmissionURL: "wss://" + strings.Repeat("55", 16) + ".m.mewla.ai/ws",
			StableURL:    "wss://quiet-otter-3f9a.m.mewla.ai/ws",
		}},
	}
	signed := append([]byte("mewla-link-pairing-v2\x00"), PairingBindingPayload(payload)...)
	payload.Signature = hex.EncodeToString(ed25519.Sign(key, signed))
	return payload
}

func TestCompactPairingGoldenVector(t *testing.T) {
	payload := compactFixture(t)
	if err := ValidatePairingPayload(payload, time.Now()); err != nil {
		t.Fatalf("fixture signature: %v", err)
	}
	encoded, err := EncodeCompactPairing(payload)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != compactGolden {
		t.Fatalf("golden vector changed:\n%s", encoded)
	}
}

func TestCompactPairingRoundTripsAndRejectsTampering(t *testing.T) {
	payload := compactFixture(t)
	decoded, err := DecodeCompactPairing(compactGolden)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePairingPayload(decoded, time.Now()); err != nil {
		t.Fatalf("decoded golden: %v", err)
	}
	if decoded.DaemonID != "" || decoded.Signature != payload.Signature ||
		decoded.Candidates[0] != payload.Candidates[0] {
		t.Fatalf("decoded = %#v", decoded)
	}

	raw, err := compactEncoding.DecodeString(compactGolden)
	if err != nil {
		t.Fatal(err)
	}
	for index := range raw {
		for _, bit := range []byte{0x01, 0x80} {
			tampered := append([]byte(nil), raw...)
			tampered[index] ^= bit
			changed, err := DecodeCompactPairing(compactEncoding.EncodeToString(tampered))
			if err == nil && ValidatePairingPayload(changed, time.Now()) == nil {
				t.Fatalf("flipping bit %#x of byte %d still validates", bit, index)
			}
		}
	}

	for name, value := range map[string][]byte{
		"truncated":     raw[:len(raw)-1],
		"trailing byte": append(append([]byte(nil), raw...), 0),
		"version 2":     append([]byte{2}, raw[1:]...),
	} {
		if _, err := DecodeCompactPairing(compactEncoding.EncodeToString(value)); err == nil {
			t.Fatalf("%s decoded", name)
		}
	}
	if _, err := DecodeCompactPairing(strings.ToLower(compactGolden)); err == nil {
		t.Fatal("lowercase base32 decoded")
	}
}

func TestCompactPairingRejectsCandidateCountsAndMissingAdmission(t *testing.T) {
	payload := compactFixture(t)
	noAdmission := payload
	noAdmission.Candidates = []PairingCandidate{{Name: "main", StableURL: payload.Candidates[0].StableURL}}
	if _, err := EncodeCompactPairing(noAdmission); err == nil {
		t.Fatal("a payload without an admission encoded")
	}
	for _, count := range []int{0, compactMaxCandidates + 1} {
		changed := payload
		changed.Candidates = nil
		for index := range count {
			candidate := payload.Candidates[0]
			candidate.Name = "relay-" + string(rune('a'+index))
			changed.Candidates = append(changed.Candidates, candidate)
		}
		if _, err := EncodeCompactPairing(changed); err == nil {
			t.Fatalf("%d candidates encoded", count)
		}
	}
	raw, _ := compactEncoding.DecodeString(compactGolden)
	countAt := 1 + 32 + 32 + 16 + 32 + compactExpiryBytes + compactSignatureBytes + 1 + len("quiet-otter-3f9a")
	if raw[countAt] != 1 {
		t.Fatalf("candidate count is not at byte %d", countAt)
	}
	for _, count := range []byte{0, compactMaxCandidates + 1} {
		changed := append([]byte(nil), raw...)
		changed[countAt] = count
		if _, err := DecodeCompactPairing(compactEncoding.EncodeToString(changed)); err == nil {
			t.Fatalf("candidate count %d decoded", count)
		}
	}
}

func TestCompactPairingKeepsTheRouteIDLabel(t *testing.T) {
	payload := compactFixture(t)
	payload.Candidates[0].StableURL = "wss://" + payload.RouteID + ".relay.test:8443/ws"
	payload.Candidates[0].AdmissionURL = "wss://" + strings.Repeat("66", 16) + ".relay.test:8443/ws"
	encoded, err := EncodeCompactPairing(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCompactPairing(encoded)
	if err != nil || decoded.Candidates[0] != payload.Candidates[0] {
		t.Fatalf("decoded = %#v, %v", decoded.Candidates, err)
	}
	payload.Candidates[0].StableURL = "wss://" + payload.RouteID + ".relay.test:443/ws"
	if _, err := EncodeCompactPairing(payload); err == nil {
		t.Fatal("a URL that would not round-trip encoded")
	}
}
