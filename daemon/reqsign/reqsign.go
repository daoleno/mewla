// Package reqsign is the signed-request machinery shared by the hosted
// services: a payload that binds method, path, time, nonce and body digest
// to a domain string; the Authorization header; a single-use nonce cache;
// and a sliding-window rate limiter.
//
// A Scheme is one dialect (header name and payload domain). Services that
// accept more than one kind of credential give each kind its own Scheme, so a
// signature made for one kind never verifies as another.
package reqsign

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
)

const (
	// MaxSignedBodyBytes bounds a signed request body.
	MaxSignedBodyBytes = 64 << 10
	maxClockSkew       = 2 * time.Minute
	nonceRetention     = 5 * time.Minute
	maxRetainedNonces  = 1 << 17
)

// Scheme is one signed-request dialect.
type Scheme struct {
	// Name is the Authorization scheme, e.g. "MewlaMachine".
	Name string
	// Domain prefixes the signed payload, e.g. "mewla-account-v1".
	Domain string
}

// AccountScheme is the dialect of daemon-signed requests to the account
// service. Installed daemons sign with exactly these bytes.
var AccountScheme = Scheme{Name: "MewlaMachine", Domain: "mewla-account-v1"}

// Payload is the byte string a client signs for one request.
func (s Scheme) Payload(method, path, timestampMS, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(s.Domain + "\n" + method + "\n" + path + "\n" +
		timestampMS + "\n" + nonce + "\n" + hex.EncodeToString(sum[:]))
}

// Header formats the Authorization header value.
func (s Scheme) Header(publicKeyHex, timestampMS, nonce, signatureHex string) string {
	return s.Name + " v1:" + publicKeyHex + ":" + timestampMS + ":" + nonce + ":" + signatureHex
}

func (s Scheme) parse(value string) (key, timestamp, nonce, signature string, ok bool) {
	rest, found := strings.CutPrefix(value, s.Name+" ")
	if !found {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimSpace(rest), ":")
	if len(parts) != 5 || parts[0] != "v1" {
		return "", "", "", "", false
	}
	return strings.ToLower(parts[1]), parts[2], parts[3], parts[4], true
}

// Verifier authenticates requests of one Scheme.
type Verifier struct {
	Scheme Scheme
	// Verify checks signatureHex over payload for publicKeyHex.
	Verify func(publicKeyHex string, payload []byte, signatureHex string) bool
	// Now defaults to time.Now.
	Now func() time.Time
	// KeyBytes is the byte length of the public key (hex on the wire).
	KeyBytes int

	nonces NonceCache
}

// Result is a request that authenticated.
type Result struct {
	// Key is the lower-hex public key that signed the request.
	Key  string
	Body []byte
}

// Rejection says why a request did not authenticate.
type Rejection struct {
	Status  int
	Message string
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Authenticate reads the body, verifies the signature and clock window, and
// spends the nonce. The caller restores the body for its handler.
func (v *Verifier) Authenticate(w http.ResponseWriter, r *http.Request) (Result, *Rejection) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxSignedBodyBytes))
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return Result{}, &Rejection{http.StatusRequestEntityTooLarge, "request too large"}
		}
		return Result{}, &Rejection{http.StatusBadRequest, "unreadable request"}
	}
	key, nonce, ok := v.verify(r, body)
	if !ok {
		return Result{}, &Rejection{http.StatusUnauthorized, "unauthorized"}
	}
	fresh, err := v.nonces.Remember(v.Scheme.Name+":"+key+":"+strings.ToLower(nonce), v.now())
	if err != nil {
		return Result{}, &Rejection{http.StatusServiceUnavailable, "busy"}
	}
	if !fresh {
		return Result{}, &Rejection{http.StatusUnauthorized, "unauthorized"}
	}
	return Result{Key: key, Body: body}, nil
}

func (v *Verifier) verify(r *http.Request, body []byte) (string, string, bool) {
	key, timestamp, nonce, signature, ok := v.Scheme.parse(r.Header.Get("Authorization"))
	if !ok || !isOpaqueHex(key, v.KeyBytes) || !isOpaqueHex(nonce, 16) {
		return "", "", false
	}
	timestampMS, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || timestampMS <= 0 || strconv.FormatInt(timestampMS, 10) != timestamp {
		return "", "", false
	}
	skew := v.now().Sub(time.UnixMilli(timestampMS))
	if skew > maxClockSkew || skew < -maxClockSkew {
		return "", "", false
	}
	payload := v.Scheme.Payload(r.Method, r.URL.Path, timestamp, nonce, body)
	if !v.Verify(key, payload, signature) {
		return "", "", false
	}
	return key, nonce, true
}

// WithBody returns r with ctx and the already-read body.
func WithBody(r *http.Request, ctx context.Context, body []byte) *http.Request {
	r = r.WithContext(ctx)
	r.Body = io.NopCloser(bytes.NewReader(body))
	return r
}

func isOpaqueHex(value string, bytes int) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if len(normalized) != bytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(normalized)
	return err == nil && len(decoded) == bytes
}

// NonceCache remembers ids for nonceRetention.
type NonceCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

// Remember reports false when the id was already used or the cache is full.
func (c *NonceCache) Remember(id string, now time.Time) (bool, error) {
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

// RateLimiter is a per-key sliding window counter.
type RateLimiter struct {
	Limit  int
	Window time.Duration

	mu     sync.Mutex
	events map[string][]time.Time
}

// Allow records one event for key and reports whether it is within the limit.
func (l *RateLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.events == nil {
		l.events = make(map[string][]time.Time)
	}
	cutoff := now.Add(-l.Window)
	kept := l.events[key][:0]
	for _, at := range l.events[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.Limit {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, now)
	if len(l.events) > 4096 {
		for entry, times := range l.events {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(l.events, entry)
			}
		}
	}
	return true
}

// Forget drops every key that starts with prefix+":".
func (l *RateLimiter) Forget(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for entry := range l.events {
		if strings.HasPrefix(entry, prefix+":") {
			delete(l.events, entry)
		}
	}
}
