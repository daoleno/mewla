// Package googleauth is the optional, publisher-hosted Google-only exchange.
// It never stores long-lived user tokens. Public daemon installations contain
// its public origin, not its Web client secret. Do not mount it on user daemons.
package googleauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/oauth2"
)

const CallbackPath = "/google/callback"
const scopeBase = "https://www.googleapis.com/auth/"

func Scopes(writes bool) []string {
	out := []string{"openid", "email", scopeBase + "drive.readonly", scopeBase + "gmail.readonly", scopeBase + "calendar.readonly"}
	if writes {
		out = append(out, scopeBase+"drive.file", scopeBase+"gmail.send", scopeBase+"calendar.events")
	}
	return out
}

type Start struct {
	PublicKey string `json:"public_key"`
	Nonce     string `json:"nonce"`
	Writes    bool   `json:"writes"`
}
type Session struct {
	ID               string    `json:"id"`
	AuthorizationURL string    `json:"authorization_url"`
	Expires          time.Time `json:"expires"`
	Status           string    `json:"status"`
	Ciphertext       string    `json:"ciphertext,omitempty"`
}
type Credential struct {
	Token   *oauth2.Token `json:"token"`
	Scopes  []string      `json:"scopes"`
	Receipt string        `json:"receipt"`
	Proof   string        `json:"proof"`
	Nonce   string        `json:"nonce"`
}
type Refresh struct {
	RefreshToken string `json:"refresh_token"`
	Receipt      string `json:"receipt"`
	Proof        string `json:"proof"`
}
type receipt struct {
	PublicKey string   `json:"public_key"`
	TokenHash string   `json:"token_hash"`
	ProofHash string   `json:"proof_hash"`
	ClientID  string   `json:"client_id"`
	Scopes    []string `json:"scopes"`
	Nonce     string   `json:"nonce"`
}
type pending struct {
	Session
	start    Start
	verifier string
	cookie   string
	proof    string
}
type Config struct {
	Origin       string
	ClientID     string
	ClientSecret string
	ReceiptKey   []byte
}
type Exchange struct {
	mu     sync.Mutex
	config Config
	client *http.Client
	oauth  oauth2.Config
	flows  map[string]*pending
	rate   map[string][]time.Time
}

func New(config Config) (*Exchange, error) {
	u, err := url.Parse(config.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || config.ClientID == "" || config.ClientSecret == "" || len(config.ReceiptKey) < 32 {
		return nil, errors.New("configure a publisher-owned HTTPS origin, Google Web client and private 32-byte receipt key")
	}
	return &Exchange{config: config, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}, oauth: oauth2.Config{ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.Origin + CallbackPath, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}, flows: map[string]*pending{}, rate: map[string][]time.Time{}}, nil
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func decodePublic(raw string) (*[32]byte, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) != 32 {
		return nil, errors.New("invalid key")
	}
	var key [32]byte
	copy(key[:], data)
	return &key, nil
}
func Seal(value Credential, public string) (string, error) {
	key, err := decodePublic(public)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	encrypted, err := box.SealAnonymous(nil, raw, key, rand.Reader)
	return base64.RawURLEncoding.EncodeToString(encrypted), err
}
func Open(raw string, public, private *[32]byte) (Credential, error) {
	var value Credential
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return value, errors.New("invalid exchange result")
	}
	plain, ok := box.OpenAnonymous(nil, data, public, private)
	if !ok {
		return value, errors.New("exchange result belongs to another daemon")
	}
	if json.Unmarshal(plain, &value) != nil || value.Token == nil || value.Token.AccessToken == "" {
		return value, errors.New("invalid exchange credential")
	}
	return value, nil
}
func (e *Exchange) sign(r receipt) string {
	raw, _ := json.Marshal(r)
	mac := hmac.New(sha256.New, e.config.ReceiptKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (e *Exchange) verify(raw string) (receipt, error) {
	var out receipt
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return out, errors.New("invalid binding")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return out, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, err
	}
	mac := hmac.New(sha256.New, e.config.ReceiptKey)
	mac.Write(data)
	if !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(data, &out) != nil || out.ClientID != e.config.ClientID {
		return out, errors.New("invalid binding")
	}
	return out, nil
}
func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "JSON POST required", 400)
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		http.Error(w, "Invalid request", 400)
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		http.Error(w, "Invalid request", 400)
		return false
	}
	return true
}
func (e *Exchange) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	// Never accept an Origin-bearing cross-site API request or trust forwarded
	// headers. Deployment ingress supplies TLS and additional global rate limits.
	if origin := r.Header.Get("Origin"); origin != "" && origin != e.config.Origin {
		http.Error(w, "Origin rejected", 403)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	for id, f := range e.flows {
		if now.After(f.Expires) {
			delete(e.flows, id)
		}
	}
	if r.URL.Path == "/google/start" || r.URL.Path == "/google/refresh" {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		recent := []time.Time{}
		for _, at := range e.rate[host] {
			if now.Sub(at) < time.Minute {
				recent = append(recent, at)
			}
		}
		if len(recent) >= 10 || len(e.flows) >= 1000 {
			http.Error(w, "Try again later", 429)
			return
		}
		if len(e.rate) > 2000 {
			e.rate = map[string][]time.Time{}
		}
		e.rate[host] = append(recent, now)
		if r.URL.Path == "/google/refresh" {
			e.refresh(w, r)
			return
		}
		var input Start
		if !readJSON(w, r, &input) {
			return
		}
		if _, err := decodePublic(input.PublicKey); err != nil || len(input.Nonce) < 32 || len(input.Nonce) > 128 {
			http.Error(w, "Invalid binding", 400)
			return
		}
		id := oauth2.GenerateVerifier()
		f := &pending{Session: Session{ID: id, AuthorizationURL: e.config.Origin + "/google/authorize?id=" + url.QueryEscape(id), Expires: now.Add(10 * time.Minute), Status: "waiting"}, start: input, verifier: oauth2.GenerateVerifier(), proof: oauth2.GenerateVerifier()}
		e.flows[id] = f
		jsonResponse(w, f.Session)
		return
	}
	if r.URL.Path == "/google/refresh" {
		e.refresh(w, r)
		return
	}
	id := r.URL.Query().Get("id")
	if r.URL.Path == CallbackPath {
		id = r.URL.Query().Get("state")
	}
	f := e.flows[id]
	if f == nil {
		http.Error(w, "Connection expired. Return to Mewla and connect again.", 410)
		return
	}
	switch r.URL.Path {
	case "/google/authorize":
		if r.Method != "GET" || f.Status != "waiting" || f.cookie != "" {
			http.Error(w, "Connection already opened", 409)
			return
		}
		f.cookie = oauth2.GenerateVerifier()
		http.SetCookie(w, &http.Cookie{Name: "mewla_google_" + id, Value: f.cookie, Path: "/google/callback", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
		config := e.oauth
		config.Scopes = Scopes(f.start.Writes)
		http.Redirect(w, r, config.AuthCodeURL(id, oauth2.S256ChallengeOption(f.verifier), oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent")), 302)
	case CallbackPath:
		cookie, err := r.Cookie("mewla_google_" + id)
		if r.Method != "GET" || len(r.URL.RawQuery) > 16384 || err != nil || f.cookie == "" || !hmac.Equal([]byte(cookie.Value), []byte(f.cookie)) || f.Status != "waiting" {
			http.Error(w, "Authorization return rejected", 400)
			return
		}
		f.cookie = ""
		http.SetCookie(w, &http.Cookie{Name: "mewla_google_" + id, Path: "/google/callback", Secure: true, HttpOnly: true, MaxAge: -1})
		f.Status = "failed"
		if r.URL.Query().Get("error") == "" && r.URL.Query().Get("code") != "" && len(r.URL.Query()["state"]) == 1 && len(r.URL.Query()["code"]) == 1 && (r.URL.Query().Get("iss") == "" || r.URL.Query().Get("iss") == "https://accounts.google.com") {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			token, err := e.oauth.Exchange(context.WithValue(ctx, oauth2.HTTPClient, e.client), r.URL.Query().Get("code"), oauth2.VerifierOption(f.verifier))
			if err == nil && token.AccessToken != "" && token.RefreshToken != "" && len(token.AccessToken) <= 16384 && len(token.RefreshToken) <= 16384 {
				scopes := Scopes(f.start.Writes)
				if granted, ok := token.Extra("scope").(string); ok {
					scopes = strings.Fields(granted)
				}
				binding := receipt{PublicKey: f.start.PublicKey, TokenHash: hash(token.RefreshToken), ProofHash: hash(f.proof), ClientID: e.config.ClientID, Scopes: scopes, Nonce: f.start.Nonce}
				f.Ciphertext, err = Seal(Credential{Token: token, Scopes: scopes, Proof: f.proof, Receipt: e.sign(binding), Nonce: f.start.Nonce}, f.start.PublicKey)
				if err == nil {
					f.Status = "connected"
				}
			}
		}
		// Browser carries no code/token/retrieval capability back to the app.
		http.Redirect(w, r, "mewla://plugins", http.StatusFound)
	case "/google/status":
		if !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+f.start.Nonce)) {
			http.Error(w, "Flow binding rejected", 403)
			return
		}
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		jsonResponse(w, f.Session)
	case "/google/cancel":
		if !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+f.start.Nonce)) {
			http.Error(w, "Flow binding rejected", 403)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		delete(e.flows, id)
		jsonResponse(w, map[string]bool{"cancelled": true})
	default:
		http.NotFound(w, r)
	}
}
func (e *Exchange) refresh(w http.ResponseWriter, r *http.Request) {
	var input Refresh
	if !readJSON(w, r, &input) {
		return
	}
	binding, err := e.verify(input.Receipt)
	if err != nil || !hmac.Equal([]byte(binding.TokenHash), []byte(hash(input.RefreshToken))) || !hmac.Equal([]byte(binding.ProofHash), []byte(hash(input.Proof))) {
		http.Error(w, "Refresh binding rejected", 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := e.oauth.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, e.client), &oauth2.Token{RefreshToken: input.RefreshToken}).Token()
	if err != nil || token.AccessToken == "" || len(token.AccessToken) > 16384 || len(token.RefreshToken) > 16384 {
		http.Error(w, "Reconnect this Google account", 401)
		return
	}
	binding.TokenHash = hash(token.RefreshToken)
	if scope, ok := token.Extra("scope").(string); ok {
		binding.Scopes = strings.Fields(scope)
	}
	encrypted, err := Seal(Credential{Token: token, Scopes: binding.Scopes, Proof: input.Proof, Receipt: e.sign(binding), Nonce: binding.Nonce}, binding.PublicKey)
	if err != nil {
		http.Error(w, "Exchange failed", 500)
		return
	}
	jsonResponse(w, Session{Status: "connected", Ciphertext: encrypted})
}
