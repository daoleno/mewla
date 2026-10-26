package friendsvc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/clientaddr"
	"github.com/daoleno/mewla/daemon/reqsign"
)

const (
	defaultMinAccountAge = 30 * 24 * time.Hour
	enrollTTL            = 10 * time.Minute
	linkCodeTTL          = 10 * time.Minute
	maxDeviceLabel       = 40
	maxAgentName         = 40

	enrollStartsPerIPHour    = 10
	enrollCallbacksPerIPHour = 30
	linkCodesPerPersonHour   = 5
	linkAttemptsPerKeyHour   = 10
)

// HumanScheme is the dialect of phone-signed requests. The phone signs the
// payload with its Friends key directly.
var HumanScheme = reqsign.Scheme{Name: "MewlaFriendsHuman", Domain: "mewla-friends-human-v1"}

// AgentScheme is the dialect of daemon-signed requests. The daemon signs with
// auth.Manager.CreateLinkSignature, as it does for the account service.
var AgentScheme = reqsign.Scheme{Name: "MewlaFriendsAgent", Domain: "mewla-friends-agent-v1"}

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,29}$`)

// Config configures a Service.
type Config struct {
	Store *Store
	OAuth OAuth
	// PublicURL is the base URL of mewla-account as clients reach it.
	PublicURL string
	// MinAccountAge defaults to 30 days.
	MinAccountAge time.Duration
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

// Service serves the Friends API.
type Service struct {
	config Config
	human  *reqsign.Verifier
	agent  *reqsign.Verifier
	routes []Route

	enrollStarts    reqsign.RateLimiter
	enrollCallbacks reqsign.RateLimiter
	linkCodes       reqsign.RateLimiter
	linkAttempts    reqsign.RateLimiter
}

// New validates config and builds the route table.
func New(config Config) (*Service, error) {
	if config.Store == nil {
		return nil, errors.New("friends store is required")
	}
	if config.OAuth == nil {
		return nil, errors.New("friends oauth provider is required")
	}
	u, err := url.Parse(config.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("friends public url must be an absolute http(s) URL")
	}
	config.PublicURL = strings.TrimRight(config.PublicURL, "/")
	if config.MinAccountAge <= 0 {
		config.MinAccountAge = defaultMinAccountAge
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logf == nil {
		config.Logf = log.Printf
	}
	s := &Service{config: config}
	s.human = &reqsign.Verifier{Scheme: HumanScheme, Verify: verifyPlain, Now: config.Now, KeyBytes: ed25519.PublicKeySize}
	s.agent = &reqsign.Verifier{Scheme: AgentScheme, Verify: auth.VerifyLinkSignature, Now: config.Now, KeyBytes: ed25519.PublicKeySize}
	s.enrollStarts = reqsign.RateLimiter{Limit: enrollStartsPerIPHour, Window: time.Hour}
	s.enrollCallbacks = reqsign.RateLimiter{Limit: enrollCallbacksPerIPHour, Window: time.Hour}
	s.linkCodes = reqsign.RateLimiter{Limit: linkCodesPerPersonHour, Window: time.Hour}
	s.linkAttempts = reqsign.RateLimiter{Limit: linkAttemptsPerKeyHour, Window: time.Hour}
	s.routes = s.routeTable()
	if err := validateRoutes(s.routes); err != nil {
		return nil, err
	}
	return s, nil
}

func verifyPlain(publicKeyHex string, payload []byte, signatureHex string) bool {
	key, err := hex.DecodeString(publicKeyHex)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false
	}
	signature, err := hex.DecodeString(signatureHex)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(key, payload, signature)
}

// Routes lists the route table.
func (s *Service) Routes() []Route { return append([]Route(nil), s.routes...) }

func (s *Service) routeTable() []Route {
	return []Route{
		{http.MethodGet, apiPrefix + "/enroll/start", ClassPublic, s.handleEnrollStart},
		{http.MethodGet, apiPrefix + "/enroll/callback", ClassPublic, s.handleEnrollCallback},
		{http.MethodPost, apiPrefix + "/enroll/finish", ClassEnroll, s.handleEnrollFinish},
		{http.MethodGet, apiPrefix + "/me", ClassHuman, s.handleMe},
		{http.MethodPut, apiPrefix + "/me/handle", ClassHuman, s.handleSetHandle},
		{http.MethodPost, apiPrefix + "/me/link-code", ClassHuman, s.handleLinkCode},
		{http.MethodPost, apiPrefix + "/agent/link", ClassAgentLink, s.handleAgentLink},
		{http.MethodGet, apiPrefix + "/agent/status", ClassAgent, s.handleAgentStatus},
	}
}

// Handler serves every route under PathPrefix.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range s.routes {
		mux.HandleFunc(route.Pattern(), s.guard(route))
	}
	return mux
}

func (s *Service) now() time.Time { return s.config.Now() }

// guard authenticates a request as its route's class, then calls the handler.
func (s *Service) guard(route Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !route.Class.signed() {
			route.Handler(w, r, Principal{})
			return
		}
		body, rejection := reqsign.ReadBody(w, r)
		if rejection != nil {
			writeError(w, rejection.Status, rejection.Message)
			return
		}
		verifier, other := s.agent, s.human
		if route.Class.human() {
			verifier, other = s.human, s.agent
		}
		result, rejection := verifier.AuthenticateBody(r, body)
		if rejection != nil {
			// A correct signature of the other credential class is a
			// refusal, not a bad request.
			if rejection.Status == http.StatusUnauthorized {
				if _, ok := other.Valid(r, body); ok {
					writeError(w, http.StatusForbidden, "wrong credential")
					return
				}
			}
			writeError(w, rejection.Status, rejection.Message)
			return
		}
		who, status, message := s.principal(r.Context(), route.Class, result.Key)
		if status != 0 {
			writeError(w, status, message)
			return
		}
		who.Body = result.Body
		r = reqsign.WithBody(r, r.Context(), result.Body)
		route.Handler(w, r, who)
	}
}

// principal maps a verified key to its owner for a class. A key registered
// as the other class is a 403; an unknown key is a 401.
func (s *Service) principal(ctx context.Context, class Class, key string) (Principal, int, string) {
	nowMS := s.now().UnixMilli()
	fail := func(err error) (Principal, int, string) {
		s.config.Logf("friends: key lookup failed: %v", err)
		return Principal{}, http.StatusInternalServerError, "internal error"
	}
	isHuman, err := s.config.Store.isHumanKey(ctx, key)
	if err != nil {
		return fail(err)
	}
	isAgent, err := s.config.Store.isAgentKey(ctx, key)
	if err != nil {
		return fail(err)
	}
	switch class {
	case ClassEnroll:
		if isAgent {
			return Principal{}, http.StatusForbidden, "wrong credential"
		}
		return Principal{Key: key}, 0, ""
	case ClassAgentLink:
		if isHuman {
			return Principal{}, http.StatusForbidden, "wrong credential"
		}
		return Principal{Key: key}, 0, ""
	case ClassHuman, ClassAgent:
		if (class == ClassHuman && isAgent) || (class == ClassAgent && isHuman) {
			return Principal{}, http.StatusForbidden, "wrong credential"
		}
		var person personRecord
		if class == ClassHuman {
			person, err = s.config.Store.humanPerson(ctx, key, nowMS)
		} else {
			person, err = s.config.Store.agentPerson(ctx, key, nowMS)
		}
		if errors.Is(err, errNotFound) {
			return Principal{}, http.StatusUnauthorized, "unauthorized"
		}
		if err != nil {
			return fail(err)
		}
		if person.Disabled {
			return Principal{}, http.StatusForbidden, "account disabled"
		}
		return Principal{Key: key, PersonID: person.ID}, 0, ""
	}
	return Principal{}, http.StatusInternalServerError, "internal error"
}

// ---- enrollment -----------------------------------------------------------

// returnAllowed reports whether url is an app return link the service will
// redirect to: only the app's own scheme and path.
func returnAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "mewla" || u.Host != "friends" || u.Path != "/enroll" {
		return false
	}
	return u.RawQuery == "" && u.Fragment == ""
}

func (s *Service) callbackURL() string { return s.config.PublicURL + apiPrefix + "/enroll/callback" }

func (s *Service) handleEnrollStart(w http.ResponseWriter, r *http.Request, _ Principal) {
	if !s.enrollStarts.Allow("ip:"+clientaddr.Key(clientaddr.IP(r)), s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many sign-ins from this address; try again later")
		return
	}
	query := r.URL.Query()
	key := strings.ToLower(query.Get("key"))
	if !isHex(key, ed25519.PublicKeySize) {
		writeError(w, http.StatusBadRequest, "invalid key")
		return
	}
	returnURL := query.Get("return")
	if !returnAllowed(returnURL) {
		writeError(w, http.StatusBadRequest, "invalid return")
		return
	}
	state, err := randomHex(16)
	if err != nil {
		s.internalError(w, "state", err)
		return
	}
	now := s.now()
	if err := s.config.Store.createEnrollSession(r.Context(), state, key, returnURL, now.UnixMilli(), now.Add(enrollTTL).UnixMilli()); err != nil {
		s.internalError(w, "create enroll session", err)
		return
	}
	http.Redirect(w, r, s.config.OAuth.AuthorizeURL(state, s.callbackURL(), query), http.StatusFound)
}

func (s *Service) handleEnrollCallback(w http.ResponseWriter, r *http.Request, _ Principal) {
	if !s.enrollCallbacks.Allow("ip:"+clientaddr.Key(clientaddr.IP(r)), s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many sign-ins from this address; try again later")
		return
	}
	query := r.URL.Query()
	session, err := s.config.Store.enrollByState(r.Context(), query.Get("state"))
	if errors.Is(err, errNotFound) || (err == nil && (session.Used || session.Ticket != "" || session.ExpiresAtMS < s.now().UnixMilli())) {
		writeError(w, http.StatusBadRequest, "sign-in expired; start again")
		return
	}
	if err != nil {
		s.internalError(w, "enroll session", err)
		return
	}
	back := func(values url.Values) {
		http.Redirect(w, r, session.ReturnURL+"?"+values.Encode(), http.StatusFound)
	}
	code := query.Get("code")
	if code == "" || query.Get("error") != "" {
		back(url.Values{"error": {"denied"}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identity, err := s.config.OAuth.Exchange(ctx, code, s.callbackURL())
	if err != nil {
		s.config.Logf("friends: github exchange failed: %v", err)
		back(url.Values{"error": {"provider"}})
		return
	}
	if s.now().Sub(identity.CreatedAt) < s.config.MinAccountAge {
		back(url.Values{"error": {"account_too_new"}})
		return
	}
	ticket, err1 := randomHex(16)
	challenge, err2 := randomHex(16)
	if err1 != nil || err2 != nil {
		s.internalError(w, "ticket", errors.Join(err1, err2))
		return
	}
	ok, err := s.config.Store.completeEnrollSession(r.Context(), session.State, ticket, challenge, identity)
	if err != nil || !ok {
		s.internalError(w, "complete enroll session", err)
		return
	}
	back(url.Values{"ticket": {ticket}, "challenge": {challenge}})
}

type enrollFinishRequest struct {
	Ticket      string `json:"ticket"`
	Challenge   string `json:"challenge"`
	DeviceLabel string `json:"device_label"`
}

type personView struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	GitHubLogin string `json:"github_login"`
}

func (s *Service) handleEnrollFinish(w http.ResponseWriter, r *http.Request, who Principal) {
	var request enrollFinishRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := s.config.Store.enrollByTicket(r.Context(), strings.ToLower(request.Ticket))
	if errors.Is(err, errNotFound) || (err == nil && (session.Used || session.HumanKey != who.Key ||
		session.Challenge == "" || session.Challenge != strings.ToLower(request.Challenge) ||
		session.ExpiresAtMS < s.now().UnixMilli())) {
		writeError(w, http.StatusBadRequest, "invalid or expired sign-in")
		return
	}
	if err != nil {
		s.internalError(w, "enroll ticket", err)
		return
	}
	label := strings.TrimSpace(request.DeviceLabel)
	if len(label) > maxDeviceLabel {
		label = label[:maxDeviceLabel]
	}
	personID, err1 := randomHex(16)
	handle, err2 := s.chooseHandle(r.Context(), session.GitHubLogin)
	if err1 != nil || err2 != nil {
		s.internalError(w, "new person", errors.Join(err1, err2))
		return
	}
	person, err := s.config.Store.finishEnroll(r.Context(), session, personID, handle, label, s.now().UnixMilli())
	switch {
	case errors.Is(err, errAlreadyLinked):
		writeError(w, http.StatusConflict, "already_enrolled")
	case errors.Is(err, errKeyInUse):
		writeError(w, http.StatusForbidden, "wrong credential")
	case errors.Is(err, errHandleTaken):
		writeError(w, http.StatusConflict, "handle_taken")
	case err != nil:
		s.internalError(w, "finish enroll", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"person": personView{person.ID, person.Handle, person.GitHubLogin}})
	}
}

// chooseHandle starts from the GitHub login and, when it is unusable or taken
// by someone else, adds a short random suffix.
func (s *Service) chooseHandle(ctx context.Context, login string) (string, error) {
	base := sanitizeHandle(login)
	candidate := base
	for attempt := 0; attempt < 8; attempt++ {
		if candidate != "" && handlePattern.MatchString(candidate) {
			taken, err := s.config.Store.handleIsTaken(ctx, candidate)
			if err != nil {
				return "", err
			}
			if !taken {
				return candidate, nil
			}
		}
		suffix, err := randomHex(2)
		if err != nil {
			return "", err
		}
		prefix := base
		if len(prefix) > 24 {
			prefix = prefix[:24]
		}
		if prefix == "" {
			prefix = "friend"
		}
		candidate = prefix + "-" + suffix
	}
	return "", errors.New("no free handle")
}

func sanitizeHandle(login string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(login) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		}
	}
	out := strings.TrimLeft(b.String(), "-_")
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// ---- human routes ---------------------------------------------------------

func (s *Service) loadPerson(w http.ResponseWriter, r *http.Request, who Principal) (personRecord, bool) {
	person, err := s.config.Store.humanPerson(r.Context(), who.Key, s.now().UnixMilli())
	if err != nil {
		s.internalError(w, "load person", err)
		return personRecord{}, false
	}
	return person, true
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request, who Principal) {
	person, ok := s.loadPerson(w, r, who)
	if !ok {
		return
	}
	devices, err1 := s.config.Store.deviceCount(r.Context(), person.ID)
	agentName, linked, err2 := s.config.Store.agentName(r.Context(), person.ID)
	if err := errors.Join(err1, err2); err != nil {
		s.internalError(w, "load me", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"person":         personView{person.ID, person.Handle, person.GitHubLogin},
		"handle_changed": person.HandleChanged,
		"auto_post":      person.AutoPost,
		"daily_cap":      person.DailyCap,
		"devices":        devices,
		"agent":          map[string]any{"linked": linked, "name": agentName},
	})
}

func (s *Service) handleSetHandle(w http.ResponseWriter, r *http.Request, who Principal) {
	var request struct {
		Handle string `json:"handle"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	handle := strings.ToLower(strings.TrimSpace(request.Handle))
	if !handlePattern.MatchString(handle) {
		writeError(w, http.StatusBadRequest, "invalid_handle")
		return
	}
	err := s.config.Store.changeHandle(r.Context(), who.PersonID, handle)
	switch {
	case errors.Is(err, errHandleTaken):
		writeError(w, http.StatusConflict, "handle_taken")
	case errors.Is(err, errHandleChanged):
		writeError(w, http.StatusConflict, "handle_already_changed")
	case err != nil:
		s.internalError(w, "change handle", err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"handle": handle})
	}
}

func (s *Service) handleLinkCode(w http.ResponseWriter, r *http.Request, who Principal) {
	if !s.linkCodes.Allow("person:"+who.PersonID, s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many link codes; try again later")
		return
	}
	code, err := randomHex(16)
	if err != nil {
		s.internalError(w, "link code", err)
		return
	}
	now := s.now()
	expires := now.Add(linkCodeTTL)
	if err := s.config.Store.createLinkCode(r.Context(), who.PersonID, code, now.UnixMilli(), expires.UnixMilli()); err != nil {
		s.internalError(w, "store link code", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "expires_at_ms": expires.UnixMilli()})
}

// ---- agent routes ---------------------------------------------------------

func (s *Service) handleAgentLink(w http.ResponseWriter, r *http.Request, who Principal) {
	if !s.linkAttempts.Allow("key:"+who.Key, s.now()) {
		writeError(w, http.StatusTooManyRequests, "too many link attempts; try again later")
		return
	}
	var request struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	code := strings.ToLower(strings.TrimSpace(request.Code))
	if !isHex(code, 16) {
		writeError(w, http.StatusBadRequest, "invalid_code")
		return
	}
	name := strings.TrimSpace(request.Name)
	if len(name) > maxAgentName {
		name = name[:maxAgentName]
	}
	person, replaced, err := s.config.Store.consumeLinkCode(r.Context(), code, who.Key, name, s.now().UnixMilli())
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusBadRequest, "invalid_code")
	case errors.Is(err, errKeyInUse):
		writeError(w, http.StatusConflict, "key_in_use")
	case err != nil:
		s.internalError(w, "link agent", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"handle": person.Handle, "replaced": replaced})
	}
}

func (s *Service) handleAgentStatus(w http.ResponseWriter, r *http.Request, who Principal) {
	person, err := s.config.Store.agentPerson(r.Context(), who.Key, s.now().UnixMilli())
	if err != nil {
		s.internalError(w, "agent status", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"handle":    person.Handle,
		"auto_post": person.AutoPost,
		"daily_cap": person.DailyCap,
		"remaining": person.DailyCap,
	})
}

// ---- helpers --------------------------------------------------------------

func (s *Service) internalError(w http.ResponseWriter, operation string, err error) {
	s.config.Logf("friends: %s failed: %v", operation, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func isHex(value string, bytes int) bool {
	if len(value) != bytes*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
