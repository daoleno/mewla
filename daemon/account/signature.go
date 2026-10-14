package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/linkproto"
)

const (
	// AuthorizationScheme prefixes daemon-signed account requests.
	AuthorizationScheme = "MewlaMachine"
	signatureDomain     = "mewla-account-v1"
	maxSignedBodyBytes  = 64 << 10
	maxClockSkew        = 2 * time.Minute
	nonceRetention      = 5 * time.Minute
	maxRetainedNonces   = 1 << 17
)

// SignaturePayload is the byte string a daemon signs (with
// auth.Manager.CreateLinkSignature) for one account service request.
func SignaturePayload(method, path, timestampMS, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(signatureDomain + "\n" + method + "\n" + path + "\n" +
		timestampMS + "\n" + nonce + "\n" + hex.EncodeToString(sum[:]))
}

// AuthorizationHeader formats the Authorization header value.
func AuthorizationHeader(publicKeyHex, timestampMS, nonce, signatureHex string) string {
	return AuthorizationScheme + " v1:" + publicKeyHex + ":" + timestampMS + ":" + nonce + ":" + signatureHex
}

type daemonKeyContextKey struct{}

// daemonKey returns the verified daemon public key (lower hex) of a request.
func daemonKey(r *http.Request) string {
	key, _ := r.Context().Value(daemonKeyContextKey{}).(string)
	return key
}

// nonceCache remembers (key, nonce) pairs for nonceRetention.
type nonceCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

// remember reports false when the nonce was already used or the cache is full.
func (c *nonceCache) remember(id string, now time.Time) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]time.Time)
	}
	if expiresAt, exists := c.entries[id]; exists && now.Before(expiresAt) {
		return false, nil
	}
	if len(c.entries) >= maxRetainedNonces {
		for entry, expiresAt := range c.entries {
			if !now.Before(expiresAt) {
				delete(c.entries, entry)
			}
		}
		if len(c.entries) >= maxRetainedNonces {
			return false, errors.New("replay cache is full")
		}
	}
	c.entries[id] = now.Add(nonceRetention)
	return true, nil
}

// requireSignature authenticates a daemon-signed request and exposes the
// daemon key and body to next.
func (s *Service) requireSignature(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSignedBodyBytes))
		if err != nil {
			var maxBytes *http.MaxBytesError
			if errors.As(err, &maxBytes) {
				writeError(w, http.StatusRequestEntityTooLarge, "request too large")
				return
			}
			writeError(w, http.StatusBadRequest, "unreadable request")
			return
		}
		key, nonce, ok := s.verifySignature(r, body)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		fresh, err := s.nonces.remember(key+":"+strings.ToLower(nonce), s.now())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "busy")
			return
		}
		if !fresh {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), daemonKeyContextKey{}, key))
		r.Body = io.NopCloser(bytes.NewReader(body))
		next(w, r)
	}
}

func parseAuthorization(value string) (key, timestamp, nonce, signature string, ok bool) {
	rest, found := strings.CutPrefix(value, AuthorizationScheme+" ")
	if !found {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimSpace(rest), ":")
	if len(parts) != 5 || parts[0] != "v1" {
		return "", "", "", "", false
	}
	return strings.ToLower(parts[1]), parts[2], parts[3], parts[4], true
}

// verifySignature returns the lower-hex daemon key and the nonce of a validly
// signed, fresh request. The caller enforces nonce single use.
func (s *Service) verifySignature(r *http.Request, body []byte) (string, string, bool) {
	key, timestamp, nonce, signature, ok := parseAuthorization(r.Header.Get("Authorization"))
	if !ok || !linkproto.IsOpaqueID(key, 32) || !linkproto.IsOpaqueID(nonce, 16) {
		return "", "", false
	}
	timestampMS, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || timestampMS <= 0 || strconv.FormatInt(timestampMS, 10) != timestamp {
		return "", "", false
	}
	skew := s.now().Sub(time.UnixMilli(timestampMS))
	if skew > maxClockSkew || skew < -maxClockSkew {
		return "", "", false
	}
	payload := SignaturePayload(r.Method, r.URL.Path, timestamp, nonce, body)
	if !auth.VerifyLinkSignature(key, payload, signature) {
		return "", "", false
	}
	return key, nonce, true
}
