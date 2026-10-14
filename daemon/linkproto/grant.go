package linkproto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	RouteGrantVersion = 1
	routeGrantDomain  = "mewla-route-grant-v1\x00"
	maxLabelLength    = 63
	maxGrantBytes     = 4 << 10
)

// RouteGrant is the hosted remote access service's statement that one daemon
// key owns one route and one hostname label until ExpiresAtMS. The relay
// trusts grants signed by the service key instead of a shared token.
type RouteGrant struct {
	Version         int    `json:"v"`
	AccountID       string `json:"a"`
	MachineID       string `json:"m"`
	RouteID         string `json:"r"`
	Label           string `json:"l"`
	DaemonPublicKey string `json:"k"`
	IssuedAtMS      int64  `json:"i"`
	ExpiresAtMS     int64  `json:"x"`
}

// SignRouteGrant encodes grant as base64url(json) "." base64url(signature).
func SignRouteGrant(key ed25519.PrivateKey, grant RouteGrant) (string, error) {
	grant.Version = RouteGrantVersion
	grant = normalizeGrant(grant)
	if err := validateGrantFields(grant); err != nil {
		return "", err
	}
	raw, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(key, append([]byte(routeGrantDomain), raw...))
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

// VerifyRouteGrant accepts a grant signed by any of keys that is unexpired at now.
func VerifyRouteGrant(keys []ed25519.PublicKey, token string, now time.Time) (RouteGrant, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > maxGrantBytes {
		return RouteGrant{}, errors.New("route grant is missing or too large")
	}
	encodedGrant, encodedSignature, ok := strings.Cut(token, ".")
	if !ok {
		return RouteGrant{}, errors.New("route grant is malformed")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encodedGrant)
	if err != nil {
		return RouteGrant{}, errors.New("route grant is malformed")
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return RouteGrant{}, errors.New("route grant signature is malformed")
	}
	signed := append([]byte(routeGrantDomain), raw...)
	verified := false
	for _, key := range keys {
		if len(key) == ed25519.PublicKeySize && ed25519.Verify(key, signed, signature) {
			verified = true
			break
		}
	}
	if !verified {
		return RouteGrant{}, errors.New("route grant signature is invalid")
	}
	var grant RouteGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		return RouteGrant{}, errors.New("route grant is malformed")
	}
	if grant.Version != RouteGrantVersion {
		return RouteGrant{}, errors.New("unsupported route grant version")
	}
	if normalizeGrant(grant) != grant {
		return RouteGrant{}, errors.New("route grant is not canonical")
	}
	if err := validateGrantFields(grant); err != nil {
		return RouteGrant{}, err
	}
	if now.UnixMilli() >= grant.ExpiresAtMS {
		return RouteGrant{}, errors.New("route grant expired")
	}
	return grant, nil
}

// IsHostLabel reports whether label can name a machine hostname. A label
// always contains a hyphen, so it never collides with a 32-hex route ID or a
// one-time admission alias.
func IsHostLabel(label string) bool {
	if len(label) < 3 || len(label) > maxLabelLength ||
		!strings.Contains(label, "-") ||
		strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, character := range label {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '-' {
			return false
		}
	}
	return true
}

func normalizeGrant(grant RouteGrant) RouteGrant {
	grant.AccountID = strings.TrimSpace(grant.AccountID)
	grant.MachineID = strings.TrimSpace(grant.MachineID)
	grant.RouteID = strings.ToLower(strings.TrimSpace(grant.RouteID))
	grant.Label = strings.ToLower(strings.TrimSpace(grant.Label))
	grant.DaemonPublicKey = strings.ToLower(strings.TrimSpace(grant.DaemonPublicKey))
	return grant
}

func validateGrantFields(grant RouteGrant) error {
	if grant.AccountID == "" || grant.MachineID == "" {
		return errors.New("route grant has no account or machine")
	}
	if !IsOpaqueID(grant.RouteID, 16) {
		return errors.New("route grant has an invalid route")
	}
	if !IsHostLabel(grant.Label) {
		return errors.New("route grant has an invalid hostname label")
	}
	key, err := hex.DecodeString(grant.DaemonPublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("route grant has an invalid daemon key")
	}
	if grant.ExpiresAtMS <= grant.IssuedAtMS {
		return errors.New("route grant lifetime is invalid")
	}
	return nil
}

// RouteGrantTimes reads a grant's issue and expiry times without verifying
// it, so its holder can schedule renewal. Never use it for authorization.
func RouteGrantTimes(token string) (issued, expires time.Time, ok bool) {
	encodedGrant, _, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return time.Time{}, time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encodedGrant)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	var grant RouteGrant
	if json.Unmarshal(raw, &grant) != nil || grant.ExpiresAtMS <= grant.IssuedAtMS {
		return time.Time{}, time.Time{}, false
	}
	return time.UnixMilli(grant.IssuedAtMS), time.UnixMilli(grant.ExpiresAtMS), true
}
