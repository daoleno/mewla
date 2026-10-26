package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/friends"
	"github.com/daoleno/mewla/daemon/link"
	"github.com/daoleno/mewla/daemon/push"
	"github.com/daoleno/mewla/daemon/remote"
)

// friendsLocal is Friends on this computer, with the service client built from
// this computer's own Mewla sign-in.
func (s *Server) friendsLocal() *friends.Local {
	return &friends.Local{StateDir: s.auth.StorageDir(), Auth: s.auth}
}

// friendsLogin is a Mewla sign-in the phone started on this computer.
type friendsLogin struct {
	serviceURL string
	caPEM      string
}

var friendsLogins = struct {
	sync.Mutex
	pending map[string]friendsLogin
}{pending: map[string]friendsLogin{}}

type friendsAction struct {
	Op        string          `json:"op"`
	AgentName string          `json:"agent_name,omitempty"`
	PetID     string          `json:"pet_id,omitempty"`
	TZ        string          `json:"tz,omitempty"`
	LoginID   string          `json:"login_id,omitempty"`
	DraftID   string          `json:"draft_id,omitempty"`
	Text      string          `json:"text,omitempty"`
	Enabled   bool            `json:"enabled,omitempty"`
	Method    string          `json:"method,omitempty"`
	Path      string          `json:"path,omitempty"`
	Body      json.RawMessage `json:"body,omitempty"`
}

type friendsStatus struct {
	// SignedIn is whether this computer is signed in to a Mewla account.
	SignedIn     bool            `json:"signed_in"`
	Account      string          `json:"account,omitempty"`
	Enabled      bool            `json:"enabled"`
	Handle       string          `json:"handle,omitempty"`
	Me           *friends.Me     `json:"me,omitempty"`
	Drafts       []friends.Draft `json:"drafts"`
	MaxDrafts    int             `json:"max_drafts"`
	ServiceError string          `json:"service_error,omitempty"`
}

// callable is what the phone may ask the service for through this computer:
// the App screens, and nothing that changes whether Friends is on. Turning on,
// turning off and auto-post go through their own operations because this
// computer has to act on them too.
var callable = regexp.MustCompile(`^/(feed|posts|comments|people|bookmarks|blocks|reports|notifications)(/[A-Za-z0-9_@.-]+)*(\?[A-Za-z0-9_=&.%-]*)?$`)

func friendsCallAllowed(method, path string) bool {
	if strings.Contains(path, "..") {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return callable.MatchString(path)
	case http.MethodPatch:
		return path == "/me/profile"
	}
	return false
}

// handleFriends is the paired phone's one door to Friends: it reads the state,
// signs the computer in to a Mewla account, turns Friends on and off, works the
// draft queue, and passes the App's screens through to the service as this
// computer's account. The phone holds no Friends credential of its own.
func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-friends:GET:/friends"); !ok {
			return
		}
		s.writeFriendsStatus(w, r.Context())
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-friends:POST:/friends:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var action friendsAction
		if err := json.Unmarshal(body, &action); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		s.friendsAction(w, r, action)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeFriendsJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func friendsFail(w http.ResponseWriter, err error) {
	status, code := http.StatusBadGateway, "service_unreachable"
	var refused *friends.RefusedError
	var service *friends.ServiceError
	switch {
	case errors.Is(err, friends.ErrSignedOut):
		status, code = http.StatusConflict, "signed_out"
	case errors.Is(err, friends.ErrNotLinked):
		status, code = http.StatusConflict, "not_enabled"
	case errors.Is(err, friends.ErrNoDraft):
		status, code = http.StatusNotFound, "no_draft"
	case errors.As(err, &refused):
		status, code = http.StatusUnprocessableEntity, "refused"
	case errors.As(err, &service):
		status, code = http.StatusBadGateway, service.Code
		if service.Status >= 400 && service.Status < 500 {
			status = http.StatusUnprocessableEntity
		}
	}
	writeFriendsJSON(w, status, map[string]any{"error": code, "message": err.Error()})
}

func (s *Server) writeFriendsStatus(w http.ResponseWriter, ctx context.Context) {
	writeFriendsJSON(w, http.StatusOK, s.friendsStatusNow(ctx))
}

func (s *Server) friendsStatusNow(ctx context.Context) friendsStatus {
	local := s.friendsLocal()
	status := friendsStatus{Drafts: []friends.Draft{}, MaxDrafts: friends.MaxPendingDrafts}
	if config, ok, _ := remote.LoadConfig(local.StateDir); ok {
		status.SignedIn, status.Account = true, config.Machine.AccountLogin
	}
	state, enabled := local.Enabled()
	if !enabled {
		return status
	}
	status.Enabled, status.Handle = true, state.Handle
	if drafts, err := friends.NewStore(local.StateDir).Drafts(); err == nil && drafts != nil {
		status.Drafts = drafts
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client, err := local.Client()
	if err == nil {
		var me friends.Me
		if me, err = client.Me(ctx); err == nil {
			status.Me = &me
		}
	}
	if err != nil {
		status.ServiceError = err.Error()
	}
	return status
}

func (s *Server) friendsAction(w http.ResponseWriter, r *http.Request, action friendsAction) {
	local := s.friendsLocal()
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	switch action.Op {
	case "login_start":
		s.friendsLoginStart(w, ctx, local.StateDir)
	case "login_poll":
		s.friendsLoginPoll(w, ctx, local.StateDir, action.LoginID)
	case "enable":
		if _, err := local.Enable(ctx, strings.TrimSpace(action.AgentName), strings.TrimSpace(action.PetID), action.TZ); err != nil {
			friendsFail(w, err)
			return
		}
		s.writeFriendsStatus(w, ctx)
	case "disable":
		if err := local.Disable(ctx); err != nil {
			friendsFail(w, err)
			return
		}
		s.writeFriendsStatus(w, ctx)
	case "auto_post":
		if _, ok := local.Enabled(); !ok {
			friendsFail(w, friends.ErrNotLinked)
			return
		}
		client, err := local.Client()
		if err == nil {
			err = client.Do(ctx, http.MethodPut, "/me/auto-post", map[string]any{"enabled": action.Enabled, "tz": action.TZ}, nil)
		}
		if err != nil {
			friendsFail(w, err)
			return
		}
		s.writeFriendsStatus(w, ctx)
	case "post_draft":
		post, err := local.PostDraft(ctx, action.DraftID, action.Text)
		if err != nil {
			friendsFail(w, err)
			return
		}
		writeFriendsJSON(w, http.StatusOK, map[string]any{"post": post})
	case "discard_draft":
		if err := local.Discard(action.DraftID); err != nil {
			friendsFail(w, err)
			return
		}
		s.writeFriendsStatus(w, ctx)
	case "check":
		if err := local.Check(action.Text); err != nil {
			friendsFail(w, err)
			return
		}
		writeFriendsJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case "call":
		if _, ok := local.Enabled(); !ok {
			friendsFail(w, friends.ErrNotLinked)
			return
		}
		if !friendsCallAllowed(action.Method, action.Path) && !(action.Method == http.MethodGet && action.Path == "/me") {
			http.Error(w, "that Friends request is not allowed", http.StatusBadRequest)
			return
		}
		client, err := local.Client()
		if err != nil {
			friendsFail(w, err)
			return
		}
		var body any
		if len(action.Body) > 0 && string(action.Body) != "null" {
			body = action.Body
		}
		raw, err := client.DoRaw(ctx, action.Method, action.Path, body)
		if err != nil {
			var service *friends.ServiceError
			if errors.As(err, &service) {
				writeFriendsJSON(w, http.StatusOK, map[string]any{"status": service.Status, "error": service.Code, "kinds": service.Kinds})
				return
			}
			friendsFail(w, err)
			return
		}
		writeFriendsJSON(w, http.StatusOK, map[string]any{"status": 200, "body": json.RawMessage(raw)})
	default:
		http.Error(w, "unknown Friends operation", http.StatusBadRequest)
	}
}

// friendsLoginStart begins the same device sign-in `mewla login` does, so the
// phone can show the code and open the page.
func (s *Server) friendsLoginStart(w http.ResponseWriter, ctx context.Context, stateDir string) {
	serviceURL, caPEM := remote.ServiceURL(), ""
	if current, ok, _ := remote.LoadConfig(stateDir); ok {
		serviceURL, caPEM = current.ServiceURL, current.ServiceCAPEM
	}
	if serviceURL == "" {
		friendsFail(w, errors.New("this Mewla build has no sign-in service yet"))
		return
	}
	identity, err := link.LoadOrCreateTransportIdentity(stateDir, nil)
	if err != nil {
		friendsFail(w, fmt.Errorf("initialize transport identity: %w", err))
		return
	}
	client, err := remote.NewClient(serviceURL, caPEM, s.auth)
	if err != nil {
		friendsFail(w, err)
		return
	}
	start, err := client.StartLogin(ctx, "", identity.RouteID)
	if err != nil {
		friendsFail(w, fmt.Errorf("start sign-in: %w", err))
		return
	}
	friendsLogins.Lock()
	friendsLogins.pending[start.LoginID] = friendsLogin{serviceURL: serviceURL, caPEM: caPEM}
	friendsLogins.Unlock()
	writeFriendsJSON(w, http.StatusOK, start)
}

func (s *Server) friendsLoginPoll(w http.ResponseWriter, ctx context.Context, stateDir, loginID string) {
	friendsLogins.Lock()
	login, ok := friendsLogins.pending[loginID]
	friendsLogins.Unlock()
	if !ok {
		http.Error(w, "unknown sign-in", http.StatusNotFound)
		return
	}
	client, err := remote.NewClient(login.serviceURL, login.caPEM, s.auth)
	if err != nil {
		friendsFail(w, err)
		return
	}
	machine, err := client.PollLogin(ctx, loginID)
	if errors.Is(err, remote.ErrLoginPending) {
		writeFriendsJSON(w, http.StatusOK, map[string]string{"status": "pending"})
		return
	}
	if err != nil {
		friendsLogins.Lock()
		delete(friendsLogins.pending, loginID)
		friendsLogins.Unlock()
		friendsFail(w, err)
		return
	}
	if err := remote.SaveConfig(stateDir, remote.Config{ServiceURL: login.serviceURL, ServiceCAPEM: login.caPEM, Machine: machine}); err != nil {
		friendsFail(w, fmt.Errorf("save sign-in: %w", err))
		return
	}
	friendsLogins.Lock()
	delete(friendsLogins.pending, loginID)
	friendsLogins.Unlock()
	writeFriendsJSON(w, http.StatusOK, map[string]string{"status": "done", "account": machine.AccountLogin})
}

// ---- push -----------------------------------------------------------------

type friendsPusher interface {
	Send(push.Message) error
}

const friendsPollInterval = time.Minute

// RunFriendsNotifier tells the phone about comments, follows and drafts
// waiting while Friends is on. Likes stay in the in-app list.
func (s *Server) RunFriendsNotifier(ctx context.Context) {
	ticker := time.NewTicker(friendsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.friendsNotifyOnce(ctx)
		}
	}
}

func (s *Server) friendsNotifyOnce(ctx context.Context) {
	pusher, ok := any(s.pusher).(friendsPusher)
	if !ok || s.pusher == nil {
		return
	}
	local := s.friendsLocal()
	store := friends.NewStore(local.StateDir)
	state, err := store.State()
	if err != nil {
		return
	}
	changed := false
	if drafts, err := store.Drafts(); err == nil {
		if n := len(drafts); n > state.DraftsPushed {
			title, body := "Friends", fmt.Sprintf("%d draft%s waiting for you", n, plural(n))
			_ = pusher.Send(push.Message{Title: title, Body: body, Data: map[string]string{"type": "friends", "screen": "drafts"}})
		}
		if n := len(drafts); n != state.DraftsPushed {
			state.DraftsPushed, changed = n, true
		}
	}
	if client, err := local.Client(); err == nil {
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		notes, err := client.Notifications(callCtx, state.NotifySeq)
		cancel()
		if err == nil {
			first := state.NotifySeq == 0
			for i := len(notes) - 1; i >= 0; i-- { // oldest first
				n := notes[i]
				if n.Seq > state.NotifySeq {
					state.NotifySeq, changed = n.Seq, true
				}
				// The first look after turning on only sets the mark.
				if first || (n.Kind != "comment" && n.Kind != "follow") {
					continue
				}
				who := n.Actor.AgentName
				if who == "" {
					who = "@" + n.Actor.Handle
				}
				body := who + " followed you"
				if n.Kind == "comment" {
					body = who + " commented on your post"
				}
				_ = pusher.Send(push.Message{Title: "Friends", Body: body, Data: map[string]string{"type": "friends", "screen": "notifications"}})
			}
		}
	}
	if changed {
		_ = store.SaveState(state)
	}
}

func plural(n int) string {
	if n == 1 {
		return " is"
	}
	return "s are"
}
