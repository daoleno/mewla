package link

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

// PairingVersion is the one pairing link format: the signed fields in fixed
// binary, base32 encoded, so a terminal QR code holds it in alphanumeric mode.
// The daemon ID is left out: it is the SHA-256 of the signed public key, and
// enrollment checks that key.
const PairingVersion = 3

type PairingCandidate struct {
	Name         string
	AdmissionURL string
	StableURL    string
}

type PairingPayload struct {
	Version         int
	DaemonID        string
	DaemonPublicKey string
	EnrollmentToken string
	RouteID         string
	TransportPin    string
	Candidates      []PairingCandidate
	ExpiresAtMS     int64
	Signature       string
}

func BuildPairingLink(
	authManager *auth.Manager,
	identity *TransportIdentity,
	config ConnectorConfig,
	pairing auth.PairingToken,
	admissions []AdmissionOffer,
) (string, PairingPayload, error) {
	if authManager == nil || identity == nil {
		return "", PairingPayload{}, errors.New("daemon auth and Link transport identity are required")
	}
	admissionByRelay := make(map[string]AdmissionOffer, len(admissions))
	for _, admission := range admissions {
		admissionByRelay[admission.RelayName] = admission
	}
	candidates := make([]PairingCandidate, 0, len(config.Candidates))
	for _, candidate := range config.Candidates {
		stableHost := StableHost(config, identity, candidate)
		entry := PairingCandidate{
			Name:      candidate.Name,
			StableURL: candidateURL(stableHost, candidate.ClientPort),
		}
		if admission, ok := admissionByRelay[candidate.Name]; ok {
			entry.AdmissionURL = admission.AdmissionURL
		}
		candidates = append(candidates, entry)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Name == candidates[right].Name {
			return candidates[left].StableURL < candidates[right].StableURL
		}
		return candidates[left].Name < candidates[right].Name
	})
	hasAdmission := false
	for _, candidate := range candidates {
		if candidate.AdmissionURL != "" {
			hasAdmission = true
			break
		}
	}
	if !hasAdmission {
		return "", PairingPayload{}, errors.New("no connected Link relay issued a pairing admission")
	}
	payload := PairingPayload{
		Version:         PairingVersion,
		RouteID:         identity.RouteID,
		DaemonPublicKey: authManager.PublicKeyHex(),
		EnrollmentToken: strings.ToLower(strings.TrimSpace(pairing.Value)),
		TransportPin:    identity.SPKISHA256,
		Candidates:      candidates,
		ExpiresAtMS:     pairing.ExpiresAt.UnixMilli(),
	}
	payload.Signature = authManager.CreateLinkPairingSignature(
		PairingBindingPayload(payload),
	)
	encoded, err := EncodeCompactPairing(payload)
	if err != nil {
		return "", PairingPayload{}, fmt.Errorf("encode Link pairing link: %w", err)
	}
	return CompactPairingLink(encoded), payload, nil
}

// CompactPairingLink is the connect link for an encoded payload. The payload
// comes last, so a QR encoder can put it in one alphanumeric segment.
func CompactPairingLink(encoded string) string {
	return fmt.Sprintf("mewla://settings?v=%d&%s=%s", PairingVersion, connectPayloadParameter, encoded)
}

const connectPayloadParameter = "p"

func PairingBindingPayload(payload PairingPayload) []byte {
	parts := []string{
		fmt.Sprintf("%d", payload.Version),
		strings.ToLower(strings.TrimSpace(payload.DaemonID)),
		strings.ToLower(strings.TrimSpace(payload.DaemonPublicKey)),
		strings.ToLower(strings.TrimSpace(payload.EnrollmentToken)),
		strings.ToLower(strings.TrimSpace(payload.RouteID)),
		strings.ToLower(strings.TrimSpace(payload.TransportPin)),
		fmt.Sprintf("%d", payload.ExpiresAtMS),
	}
	for _, candidate := range payload.Candidates {
		parts = append(parts,
			strings.TrimSpace(candidate.Name),
			strings.TrimSpace(candidate.AdmissionURL),
			strings.TrimSpace(candidate.StableURL),
		)
	}
	return []byte(strings.Join(parts, "\n"))
}

func ValidatePairingPayload(payload PairingPayload, now time.Time) error {
	if payload.Version != PairingVersion ||
		!linkID(payload.RouteID, 16) ||
		len(strings.TrimSpace(payload.TransportPin)) != 64 ||
		len(payload.Candidates) == 0 {
		return errors.New("invalid Link pairing payload")
	}
	if now.UnixMilli() > payload.ExpiresAtMS {
		return errors.New("Link pairing payload expired")
	}
	if !auth.VerifyLinkPairingSignature(
		payload.DaemonPublicKey,
		PairingBindingPayload(payload),
		payload.Signature,
	) {
		return errors.New("Link pairing payload failed daemon identity proof")
	}
	return nil
}

func linkID(value string, bytes int) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != bytes*2 {
		return false
	}
	for _, character := range normalized {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
