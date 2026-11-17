// Package slacktoken is the account service's Slack token exchange. Slack
// renews a rotating token issued to an https redirect only with the app's
// client secret, and daemons never hold it: a signed-in daemon asks this
// service to redeem its sign-in code and to renew its token, and gets Slack's
// own answer back. The service keeps no token and logs none.
package slacktoken

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/reqsign"
)

const (
	// PathPrefix is where the account service mounts the handler.
	PathPrefix   = "/plugins/slack"
	ExchangePath = PathPrefix + "/v1/exchange"
	RefreshPath  = PathPrefix + "/v1/refresh"
	// TokenURL is Slack's oauth.v2.access.
	TokenURL = "https://slack.com/api/oauth.v2.access"
)

// Exchange is a daemon's sign-in code to redeem.
type Exchange struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	RedirectURI  string `json:"redirect_uri"`
}

// Refresh is a daemon's rotating refresh token to renew.
type Refresh struct {
	RefreshToken string `json:"refresh_token"`
}

// Accounts says whether a verified daemon key is signed in to an account
// that may use the service, and which one.
type Accounts interface {
	AccountForKey(ctx context.Context, daemonKeyHex string) (id string, ok bool, err error)
}

type Config struct {
	ClientID     string
	ClientSecret string
	// RedirectURI is the one return address codes may be redeemed for: the
	// account service's own plugin return page.
	RedirectURI string
	Accounts    Accounts
	// TokenURL defaults to Slack's; tests point it elsewhere.
	TokenURL string
	HTTP     *http.Client
	Now      func() time.Time
}

type Service struct {
	config    Config
	verifier  *reqsign.Verifier
	exchanges reqsign.RateLimiter
	refreshes reqsign.RateLimiter
}

func New(config Config) *Service {
	if config.TokenURL == "" {
		config.TokenURL = TokenURL
	}
	if config.HTTP == nil {
		config.HTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{
		config:   config,
		verifier: &reqsign.Verifier{Scheme: reqsign.AccountScheme, Verify: auth.VerifyLinkSignature, Now: config.Now, KeyBytes: 32},
		// A person signs in a few times; a daemon renews each account every 12
		// hours, more when a call races an expiry.
		exchanges: reqsign.RateLimiter{Limit: 20, Window: time.Hour},
		refreshes: reqsign.RateLimiter{Limit: 120, Window: time.Hour},
	}
}

// ready reports a service with Mewla's Slack app configured. Without it the
// account service still runs; only Slack sign-in is unavailable.
func (s *Service) ready() bool {
	return s.config.ClientID != "" && s.config.ClientSecret != "" && s.config.RedirectURI != ""
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+ExchangePath, s.signed(&s.exchanges, s.exchange))
	mux.HandleFunc("POST "+RefreshPath, s.signed(&s.refreshes, s.refresh))
	return mux
}

func (s *Service) signed(limit *reqsign.RateLimiter, next func(w http.ResponseWriter, r *http.Request, body []byte)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, rejection := s.verifier.Authenticate(w, r)
		if rejection != nil {
			writeError(w, rejection.Status, rejection.Message)
			return
		}
		account, ok, err := s.config.Accounts.AccountForKey(r.Context(), strings.ToLower(result.Key))
		if err != nil {
			log.Printf("slack token: resolve account: %v", err)
			writeError(w, http.StatusServiceUnavailable, "busy")
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !s.ready() {
			writeError(w, http.StatusServiceUnavailable, "slack_unavailable")
			return
		}
		if !limit.Allow(account, s.config.Now()) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next(w, r, result.Body)
	}
}

func (s *Service) exchange(w http.ResponseWriter, r *http.Request, body []byte) {
	var in Exchange
	if !decode(body, &in) || in.Code == "" || in.CodeVerifier == "" || len(in.Code) > 1024 || len(in.CodeVerifier) > 256 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if in.RedirectURI != s.config.RedirectURI {
		writeError(w, http.StatusBadRequest, "invalid_redirect_uri")
		return
	}
	s.token(w, r.Context(), url.Values{"grant_type": {"authorization_code"}, "code": {in.Code}, "code_verifier": {in.CodeVerifier}, "redirect_uri": {in.RedirectURI}})
}

func (s *Service) refresh(w http.ResponseWriter, r *http.Request, body []byte) {
	var in Refresh
	if !decode(body, &in) || in.RefreshToken == "" || len(in.RefreshToken) > 16384 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	s.token(w, r.Context(), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {in.RefreshToken}})
}

// token posts one grant to Slack with Mewla's client credentials and answers
// with Slack's JSON as it came, so the daemon reads Slack's own ok/error.
func (s *Service) token(w http.ResponseWriter, ctx context.Context, form url.Values) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	// Slack asks for the client credentials in HTTP Basic auth.
	request.SetBasicAuth(url.QueryEscape(s.config.ClientID), url.QueryEscape(s.config.ClientSecret))
	response, err := s.config.HTTP.Do(request)
	if err != nil {
		// The error names the URL and the network failure, never the form.
		log.Printf("slack token: %s: no answer", form.Get("grant_type"))
		writeError(w, http.StatusBadGateway, "slack_unreachable")
		return
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		log.Printf("slack token: %s: Slack answered %d", form.Get("grant_type"), response.StatusCode)
		writeError(w, http.StatusBadGateway, "slack_unreachable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// decode reads one JSON object with only the known fields.
func decode(body []byte, out any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(out) == nil && !decoder.More()
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":"`+code+`"}`)
}
