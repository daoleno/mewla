// Package pairlink builds the one-time connect link a phone or browser uses
// to pair with this computer: mewla://settings?p=<payload>, where the payload
// carries the server URL, the daemon public key and the pairing token.
package pairlink

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

const (
	payloadParam     = "p"
	payloadVersion   = 1
	publicKeyBytes   = 32
	tokenBytes       = 32
	defaultSocketDir = "/ws"
)

// NormalizeEndpoint turns an http(s) or ws(s) address into the daemon
// WebSocket URL.
func NormalizeEndpoint(rawValue string) (string, error) {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("endpoint must include scheme and host")
	}

	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported endpoint scheme %q", parsed.Scheme)
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws"
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

// ConnectLink is the compact connect link for serverURL. A trailing /ws is
// left out of the payload: every app version puts it back, and the shorter
// payload keeps the QR code a size smaller.
func ConnectLink(serverURL, daemonPublicKeyHex, tokenHex string) (string, error) {
	compact := strings.TrimSuffix(strings.TrimSpace(serverURL), defaultSocketDir)
	payload, err := EncodePayload(compact, daemonPublicKeyHex, tokenHex)
	if err != nil {
		return "", err
	}
	params := url.Values{}
	params.Set(payloadParam, payload)
	return "mewla://settings?" + params.Encode(), nil
}

// EncodePayload packs the version, server URL, public key and token.
func EncodePayload(serverURL, daemonPublicKeyHex, enrollmentTokenHex string) (string, error) {
	urlBytes := []byte(strings.TrimSpace(serverURL))
	if len(urlBytes) == 0 {
		return "", fmt.Errorf("server URL is empty")
	}
	if len(urlBytes) > 0xffff {
		return "", fmt.Errorf("server URL is too long")
	}

	publicKey, err := hex.DecodeString(strings.TrimSpace(daemonPublicKeyHex))
	if err != nil {
		return "", fmt.Errorf("decode daemon public key: %w", err)
	}
	if len(publicKey) != publicKeyBytes {
		return "", fmt.Errorf("daemon public key must be %d bytes", publicKeyBytes)
	}

	token, err := hex.DecodeString(strings.TrimSpace(enrollmentTokenHex))
	if err != nil {
		return "", fmt.Errorf("decode enrollment token: %w", err)
	}
	if len(token) != tokenBytes {
		return "", fmt.Errorf("enrollment token must be %d bytes", tokenBytes)
	}

	payload := make([]byte, 1+2+len(urlBytes)+len(publicKey)+len(token))
	payload[0] = payloadVersion
	binary.BigEndian.PutUint16(payload[1:3], uint16(len(urlBytes)))

	offset := 3
	copy(payload[offset:], urlBytes)
	offset += len(urlBytes)
	copy(payload[offset:], publicKey)
	offset += len(publicKey)
	copy(payload[offset:], token)

	return base64.RawURLEncoding.EncodeToString(payload), nil
}
