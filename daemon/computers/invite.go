package computers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
)

const invitePrefix = "mewla-computer:"

// Invite lets one computer pair with this one as a computer-kind device. It
// carries a one-time pairing token and is signed by this daemon's identity.
type Invite struct {
	V               int       `json:"v"`
	DaemonID        string    `json:"daemon_id"`
	DaemonPublicKey string    `json:"daemon_public_key"`
	Name            string    `json:"name"`
	URLs            []string  `json:"urls"`
	Token           string    `json:"token"`
	ExpiresAt       time.Time `json:"expires_at"`
	Signature       string    `json:"signature,omitempty"`
}

func (i Invite) signedPayload() []byte {
	i.Signature = ""
	payload, _ := json.Marshal(i)
	return payload
}

func EncodeInvite(invite Invite) string {
	payload, _ := json.Marshal(invite)
	return invitePrefix + base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeInvite parses and checks an invite. It verifies the signature with
// the key the invite names, so callers that already trust a key must still
// compare DaemonID with it.
func DecodeInvite(raw string, now time.Time) (Invite, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, invitePrefix) {
		return Invite{}, errors.New("not a Mewla computer invite")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, invitePrefix))
	if err != nil {
		return Invite{}, errors.New("invite is damaged")
	}
	var invite Invite
	if err := json.Unmarshal(payload, &invite); err != nil {
		return Invite{}, errors.New("invite is damaged")
	}
	if invite.V != 1 || invite.Token == "" || invite.DaemonPublicKey == "" {
		return Invite{}, errors.New("invite is from an unsupported Mewla version")
	}
	if !auth.VerifyComputerInviteSignature(invite.DaemonPublicKey, invite.signedPayload(), invite.Signature) {
		return Invite{}, errors.New("invite signature does not match")
	}
	if daemonIDForKey(invite.DaemonPublicKey) != strings.ToLower(invite.DaemonID) {
		return Invite{}, errors.New("invite names the wrong computer")
	}
	if now.After(invite.ExpiresAt) {
		return Invite{}, errors.New("invite expired; create a new one")
	}
	return invite, nil
}

// checkTransport refuses plain HTTP outside loopback and Tailscale: frames
// after the WebSocket upgrade are not signed, so the path itself must be
// private.
func checkTransport(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid address %q", raw)
	}
	switch parsed.Scheme {
	case "https":
		return parsed, nil
	case "http":
		if privatePath(parsed.Hostname()) {
			return parsed, nil
		}
		return nil, fmt.Errorf("%s is plain HTTP on a shared network; use Tailscale, HTTPS or Mewla remote access", parsed.Host)
	default:
		return nil, fmt.Errorf("unsupported address %q", raw)
	}
}

var tailscaleNet = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func privatePath(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".ts.net") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && tailscaleNet.Contains(v4) {
		return true
	}
	// Tailscale's IPv6 range fd7a:115c:a1e0::/48.
	return len(ip) == net.IPv6len && ip[0] == 0xfd && ip[1] == 0x7a && ip[2] == 0x11 && ip[3] == 0x5c && ip[4] == 0xa1 && ip[5] == 0xe0
}

// usableURLs keeps the addresses another computer may dial, in order,
// without duplicates.
func usableURLs(candidates []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, candidate := range candidates {
		parsed, err := checkTransport(candidate)
		if err != nil {
			continue
		}
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			// Loopback names the dialing computer itself.
			continue
		}
		if parsed.Hostname() == "localhost" {
			continue
		}
		normalized := parsed.Scheme + "://" + parsed.Host
		if !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	return out
}
