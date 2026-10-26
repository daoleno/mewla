package friendsvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/reqsign"
)

// PathPrefix is where mewla-account mounts the service. Signatures cover the
// full request path, so handlers are registered with it in place.
const PathPrefix = "/friends"

const apiPrefix = PathPrefix + "/v1"

const (
	defaultMinAccountAge = 30 * 24 * time.Hour
	maxPostRunes         = friends.MaxPostRunes
	maxCommentRunes      = 280
	maxBioRunes          = 160
	maxAgentNameRunes    = 40
	defaultDailyCap      = 3
	// hardDailyPosts bounds every kind of post a person can make in a day.
	hardDailyPosts = 10
	pageSize       = 30
	// reportsToHide is how many different people must report an item before
	// it is hidden until an operator looks.
	reportsToHide = 3
)

var handlePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,29}$`)

// Account is who a computer is signed in as.
type Account struct {
	ID    string
	Login string
	// CreatedAt is the age basis of the account: when the provider account
	// was made, or when the Mewla account was when the provider did not say.
	CreatedAt time.Time
	Disabled  bool
}

// Accounts resolves a daemon key to its account. The bool is false when the
// key is not signed in to any account.
type Accounts interface {
	AccountForKey(ctx context.Context, daemonKeyHex string) (Account, bool, error)
}

// Config configures a Service.
type Config struct {
	Store    *Store
	Accounts Accounts
	// MinAccountAge defaults to 30 days.
	MinAccountAge time.Duration
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

// Service serves the Friends API.
type Service struct {
	config   Config
	verifier *reqsign.Verifier

	comments reqsign.RateLimiter
	likes    reqsign.RateLimiter
	follows  reqsign.RateLimiter
	reports  reqsign.RateLimiter
	reads    reqsign.RateLimiter
}

// New validates config and builds the service.
func New(config Config) (*Service, error) {
	if config.Store == nil {
		return nil, errors.New("friends store is required")
	}
	if config.Accounts == nil {
		return nil, errors.New("friends needs an account resolver")
	}
	if config.MinAccountAge <= 0 {
		config.MinAccountAge = defaultMinAccountAge
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logf == nil {
		config.Logf = log.Printf
	}
	return &Service{
		config:   config,
		verifier: &reqsign.Verifier{Scheme: reqsign.AccountScheme, Verify: auth.VerifyLinkSignature, Now: config.Now, KeyBytes: 32},
		comments: reqsign.RateLimiter{Limit: 30, Window: time.Hour},
		likes:    reqsign.RateLimiter{Limit: 300, Window: time.Hour},
		follows:  reqsign.RateLimiter{Limit: 100, Window: 24 * time.Hour},
		reports:  reqsign.RateLimiter{Limit: 20, Window: 24 * time.Hour},
		reads:    reqsign.RateLimiter{Limit: 240, Window: time.Minute},
	}, nil
}

func (s *Service) now() time.Time { return s.config.Now() }

type handlerFunc func(w http.ResponseWriter, r *http.Request, me personRecord)

// Handler serves every route under PathPrefix.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	v1 := func(method, path string, h handlerFunc) {
		mux.HandleFunc(method+" "+apiPrefix+path, s.person(h))
	}
	mux.HandleFunc("POST "+apiPrefix+"/me/enable", s.signed(s.handleEnable))
	v1("GET", "/me", s.handleMe)
	v1("DELETE", "/me", s.handleDeleteMe)
	v1("PATCH", "/me/profile", s.handlePatchProfile)
	v1("PUT", "/me/auto-post", s.handleAutoPost)

	v1("GET", "/feed", s.handleFeed)
	v1("POST", "/posts", s.handleCreatePost)
	v1("GET", "/posts/{id}", s.handleGetPost)
	v1("DELETE", "/posts/{id}", s.handleDeletePost)
	v1("PUT", "/posts/{id}/like", s.handleLike)
	v1("DELETE", "/posts/{id}/like", s.handleUnlike)
	v1("PUT", "/posts/{id}/bookmark", s.handleBookmark)
	v1("DELETE", "/posts/{id}/bookmark", s.handleUnbookmark)
	v1("GET", "/bookmarks", s.handleBookmarks)
	v1("POST", "/posts/{id}/comments", s.handleComment)
	v1("DELETE", "/comments/{id}", s.handleDeleteComment)

	v1("GET", "/people/{id}", s.handleGetPerson)
	v1("GET", "/people/{id}/posts", s.handlePersonPosts)
	v1("PUT", "/people/{id}/follow", s.handleFollow)
	v1("DELETE", "/people/{id}/follow", s.handleUnfollow)
	v1("PUT", "/people/{id}/block", s.handleBlock)
	v1("DELETE", "/people/{id}/block", s.handleUnblock)
	v1("GET", "/blocks", s.handleBlocks)
	v1("POST", "/reports", s.handleReport)

	v1("GET", "/notifications", s.handleNotifications)
	v1("POST", "/notifications/read", s.handleNotificationsRead)
	return mux
}

// signed authenticates the request as a signed-in computer and resolves its
// account, whether or not that account has turned Friends on.
func (s *Service) signed(next func(w http.ResponseWriter, r *http.Request, account Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, rejection := s.verifier.Authenticate(w, r)
		if rejection != nil {
			writeError(w, rejection.Status, rejection.Message)
			return
		}
		account, ok, err := s.config.Accounts.AccountForKey(r.Context(), strings.ToLower(strings.TrimSpace(result.Key)))
		if err != nil {
			s.internalError(w, "resolve account", err)
			return
		}
		if !ok {
			// Signed by a key that is not signed in to any account.
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if account.Disabled {
			writeError(w, http.StatusForbidden, "account disabled")
			return
		}
		next(w, reqsign.WithBody(r, r.Context(), result.Body), account)
	}
}

// person additionally needs the account to have turned Friends on.
func (s *Service) person(next handlerFunc) http.HandlerFunc {
	return s.signed(func(w http.ResponseWriter, r *http.Request, account Account) {
		me, err := s.config.Store.personByAccount(r.Context(), account.ID)
		if errors.Is(err, errNotFound) {
			writeError(w, http.StatusNotFound, "not_enabled")
			return
		}
		if err != nil {
			s.internalError(w, "load person", err)
			return
		}
		if r.Method == http.MethodGet && !s.reads.Allow("r:"+me.ID, s.now()) {
			writeError(w, http.StatusTooManyRequests, "slow down")
			return
		}
		next(w, r, me)
	})
}

// ---- me -------------------------------------------------------------------

type meView struct {
	ID            string `json:"id"`
	Handle        string `json:"handle"`
	HandleChanged bool   `json:"handle_changed"`
	AgentName     string `json:"agent_name"`
	PetID         string `json:"pet_id"`
	Bio           string `json:"bio"`
	AutoPost      bool   `json:"auto_post"`
	DailyCap      int    `json:"daily_cap"`
	AutoToday     int    `json:"auto_today"`
	Unread        int    `json:"unread"`
}

func (s *Service) meView(ctx context.Context, p personRecord) (meView, error) {
	autoToday, err := s.countToday(ctx, p, true)
	if err != nil {
		return meView{}, err
	}
	var unread int
	if err := s.config.Store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE person_id = ? AND read_at IS NULL`, p.ID).Scan(&unread); err != nil {
		return meView{}, err
	}
	return meView{p.ID, p.Handle, p.HandleChanged, p.AgentName, p.PetID, p.Bio, p.AutoPost, p.DailyCap, autoToday, unread}, nil
}

func (s *Service) handleEnable(w http.ResponseWriter, r *http.Request, account Account) {
	var request struct {
		TZ        string `json:"tz"`
		AgentName string `json:"agent_name"`
		PetID     string `json:"pet_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	existing, err := s.config.Store.personByAccount(r.Context(), account.ID)
	switch {
	case err == nil:
		s.writeMe(w, r, existing)
		return
	case !errors.Is(err, errNotFound):
		s.internalError(w, "load person", err)
		return
	}
	if s.now().Sub(account.CreatedAt) < s.config.MinAccountAge {
		writeError(w, http.StatusForbidden, "account_too_new")
		return
	}
	id, err1 := randomHex(16)
	handle, err2 := s.chooseHandle(r.Context(), account.Login)
	if err := errors.Join(err1, err2); err != nil {
		s.internalError(w, "new person", err)
		return
	}
	person := personRecord{
		ID: id, AccountID: account.ID, Handle: handle,
		AgentName: clip(strings.TrimSpace(request.AgentName), maxAgentNameRunes),
		PetID:     clip(strings.TrimSpace(request.PetID), 40),
		TZ:        validTZ(request.TZ), CreatedAt: s.now().UnixMilli(),
	}
	if err := s.config.Store.createPerson(r.Context(), person); err != nil {
		s.internalError(w, "create person", err)
		return
	}
	created, err := s.config.Store.personByID(r.Context(), id)
	if err != nil {
		s.internalError(w, "load person", err)
		return
	}
	s.writeMe(w, r, created)
}

func (s *Service) writeMe(w http.ResponseWriter, r *http.Request, p personRecord) {
	view, err := s.meView(r.Context(), p)
	if err != nil {
		s.internalError(w, "load me", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request, me personRecord) {
	s.writeMe(w, r, me)
}

// handleDeleteMe is turning Friends off: the person and everything that
// hangs on them are deleted in one transaction.
func (s *Service) handleDeleteMe(w http.ResponseWriter, r *http.Request, me personRecord) {
	if err := s.config.Store.deletePerson(r.Context(), me.ID); err != nil {
		s.internalError(w, "delete person", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Service) handlePatchProfile(w http.ResponseWriter, r *http.Request, me personRecord) {
	var request struct {
		Handle    *string `json:"handle"`
		AgentName *string `json:"agent_name"`
		Bio       *string `json:"bio"`
		PetID     *string `json:"pet_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	db := s.config.Store.db
	if request.Handle != nil {
		handle := strings.ToLower(strings.TrimSpace(*request.Handle))
		if handle != me.Handle {
			if !handlePattern.MatchString(handle) {
				writeError(w, http.StatusBadRequest, "invalid_handle")
				return
			}
			if me.HandleChanged {
				writeError(w, http.StatusConflict, "handle_already_changed")
				return
			}
			taken, err := s.config.Store.handleTaken(r.Context(), handle)
			if err != nil {
				s.internalError(w, "handle", err)
				return
			}
			if taken {
				writeError(w, http.StatusConflict, "handle_taken")
				return
			}
			if _, err := db.ExecContext(r.Context(), `UPDATE people SET handle = ?, handle_changed = 1 WHERE id = ?`, handle, me.ID); err != nil {
				s.internalError(w, "set handle", err)
				return
			}
		}
	}
	if request.AgentName != nil {
		if _, err := db.ExecContext(r.Context(), `UPDATE people SET agent_name = ? WHERE id = ?`, clip(strings.TrimSpace(*request.AgentName), maxAgentNameRunes), me.ID); err != nil {
			s.internalError(w, "set agent name", err)
			return
		}
	}
	if request.PetID != nil {
		if _, err := db.ExecContext(r.Context(), `UPDATE people SET pet_id = ? WHERE id = ?`, clip(strings.TrimSpace(*request.PetID), 40), me.ID); err != nil {
			s.internalError(w, "set pet", err)
			return
		}
	}
	if request.Bio != nil {
		bio := strings.TrimSpace(*request.Bio)
		if len([]rune(bio)) > maxBioRunes || len(friends.CheckBasic(bio)) > 0 {
			writeError(w, http.StatusBadRequest, "invalid_bio")
			return
		}
		if _, err := db.ExecContext(r.Context(), `UPDATE people SET bio = ? WHERE id = ?`, bio, me.ID); err != nil {
			s.internalError(w, "set bio", err)
			return
		}
	}
	updated, err := s.config.Store.personByID(r.Context(), me.ID)
	if err != nil {
		s.internalError(w, "load person", err)
		return
	}
	s.writeMe(w, r, updated)
}

func (s *Service) handleAutoPost(w http.ResponseWriter, r *http.Request, me personRecord) {
	var request struct {
		Enabled bool   `json:"enabled"`
		TZ      string `json:"tz"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	tz := me.TZ
	if request.TZ != "" {
		tz = validTZ(request.TZ)
	}
	enabled := 0
	if request.Enabled {
		enabled = 1
	}
	if _, err := s.config.Store.db.ExecContext(r.Context(), `UPDATE people SET auto_post = ?, tz = ? WHERE id = ?`, enabled, tz, me.ID); err != nil {
		s.internalError(w, "set auto-post", err)
		return
	}
	updated, err := s.config.Store.personByID(r.Context(), me.ID)
	if err != nil {
		s.internalError(w, "load person", err)
		return
	}
	s.writeMe(w, r, updated)
}

// chooseHandle starts from the account login and, when it is unusable or
// taken, adds a short random suffix.
func (s *Service) chooseHandle(ctx context.Context, login string) (string, error) {
	base := sanitizeHandle(login)
	candidate := base
	for attempt := 0; attempt < 8; attempt++ {
		if handlePattern.MatchString(candidate) {
			taken, err := s.config.Store.handleTaken(ctx, candidate)
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

// validTZ keeps a time zone name that Go knows, and drops anything else.
func validTZ(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ""
	}
	return name
}

func clip(text string, runes int) string {
	r := []rune(text)
	if len(r) > runes {
		return string(r[:runes])
	}
	return text
}

// ---- helpers --------------------------------------------------------------

func (s *Service) internalError(w http.ResponseWriter, operation string, err error) {
	s.config.Logf("friends: %s failed: %v", operation, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	err := json.NewDecoder(r.Body).Decode(out)
	if err != nil && !errors.Is(err, io.EOF) {
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
