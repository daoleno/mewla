package link

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

const PairingVersion = 2

// CompactPairingVersion carries the same signed fields as PairingVersion in
// fixed binary, base32 encoded, so a terminal QR code holds it in
// alphanumeric mode at about half the size. The daemon ID is left out: it is
// the SHA-256 of the signed public key, and enrollment checks that key.
const CompactPairingVersion = 3

// PairingLinks is one Link pairing code as links. Link is version 3 for the
// QR code and current apps; Legacy is version 2 for apps that predate it.
type PairingLinks struct {
	Link   string
	Legacy string
}

type PairingCandidate struct {
	Name         string `json:"n"`
	AdmissionURL string `json:"a,omitempty"`
	StableURL    string `json:"s"`
}

type PairingPayload struct {
	Version         int                `json:"v"`
	DaemonID        string             `json:"d"`
	DaemonPublicKey string             `json:"k"`
	EnrollmentToken string             `json:"e"`
	RouteID         string             `json:"r"`
	TransportPin    string             `json:"p"`
	Candidates      []PairingCandidate `json:"c"`
	ExpiresAtMS     int64              `json:"x"`
	Signature       string             `json:"z"`
}

func BuildPairingLink(
	authManager *auth.Manager,
	identity *TransportIdentity,
	config ConnectorConfig,
	pairing auth.PairingToken,
	admissions []AdmissionOffer,
) (PairingLinks, PairingPayload, error) {
	if authManager == nil || identity == nil {
		return PairingLinks{}, PairingPayload{}, errors.New("daemon auth and Link transport identity are required")
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
		return PairingLinks{}, PairingPayload{}, errors.New("no connected Link relay issued a pairing admission")
	}
	legacy := PairingPayload{
		Version:         PairingVersion,
		DaemonID:        authManager.DaemonID(),
		DaemonPublicKey: authManager.PublicKeyHex(),
		EnrollmentToken: strings.ToLower(strings.TrimSpace(pairing.Value)),
		RouteID:         identity.RouteID,
		TransportPin:    identity.SPKISHA256,
		Candidates:      candidates,
		ExpiresAtMS:     pairing.ExpiresAt.UnixMilli(),
	}
	legacy.Signature = authManager.CreateLinkPairingSignature(
		PairingBindingPayload(legacy),
	)
	raw, err := json.Marshal(legacy)
	if err != nil {
		return PairingLinks{}, PairingPayload{}, fmt.Errorf("encode Link pairing payload: %w", err)
	}
	values := url.Values{}
	values.Set("v", fmt.Sprintf("%d", PairingVersion))
	values.Set(connectPayloadParameter, base64.RawURLEncoding.EncodeToString(raw))
	legacyLink := "mewla://settings?" + values.Encode()

	compact := legacy
	compact.Version = CompactPairingVersion
	compact.DaemonID = ""
	compact.Signature = authManager.CreateLinkPairingSignature(
		PairingBindingPayload(compact),
	)
	encoded, err := EncodeCompactPairing(compact)
	if err != nil {
		// A relay shape version 3 cannot express keeps the version 2 link.
		return PairingLinks{Link: legacyLink}, legacy, nil
	}
	return PairingLinks{Link: CompactPairingLink(encoded), Legacy: legacyLink}, compact, nil
}

// CompactPairingLink is the version 3 connect link for an encoded payload.
// The payload comes last, so a QR encoder can put it in one alphanumeric
// segment.
func CompactPairingLink(encoded string) string {
	return fmt.Sprintf("mewla://settings?v=%d&%s=%s", CompactPairingVersion, connectPayloadParameter, encoded)
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
	if (payload.Version != PairingVersion && payload.Version != CompactPairingVersion) ||
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
