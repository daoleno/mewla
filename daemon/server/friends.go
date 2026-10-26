package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/friends"
)

// friendsService is the hosted Friends service this computer links to. A test
// replaces it; MEWLA_FRIENDS_URL points a development daemon at a local one.
func friendsServiceURL() string {
	if value := strings.TrimSpace(os.Getenv("MEWLA_FRIENDS_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return friends.DefaultServiceURL
}

type friendsStatus struct {
	Linked bool   `json:"linked"`
	Handle string `json:"handle,omitempty"`
	Drafts int    `json:"drafts"`
}

type friendsAction struct {
	// LinkCode links this computer to the account that made the code.
	LinkCode string `json:"link_code,omitempty"`
	// Name is the agent's display name.
	Name string `json:"name,omitempty"`
}

// handleFriends lets a paired phone link this computer to its owner's Friends
// account, read the link, and forget it. The phone is the only place a link
// code can be made; the computer signs the link with its own identity and
// never learns anything the phone signs with.
func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	store := friends.NewStore(s.auth.StorageDir())
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, "mewla-friends:GET:/friends"); !ok {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, "mewla-friends:POST:/friends:sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var action friendsAction
		if err := json.Unmarshal(body, &action); err != nil || strings.TrimSpace(action.LinkCode) == "" {
			http.Error(w, "send link_code", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		service := friendsServiceURL()
		result, err := (&friends.Client{BaseURL: service, Auth: s.auth}).Link(ctx, strings.TrimSpace(action.LinkCode), action.Name)
		if err != nil {
			var refused *friends.ServiceError
			if errors.As(err, &refused) && refused.Status >= 400 && refused.Status < 500 {
				http.Error(w, refused.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if result.Replaced {
			_ = store.Clear()
		}
		if err := store.SaveLink(friends.Link{Handle: result.Handle, ServiceURL: service, LinkedAt: time.Now().UTC()}); err != nil {
			http.Error(w, "could not save the link", http.StatusInternalServerError)
			return
		}
	case http.MethodDelete:
		if _, ok := s.authenticateRequest(w, r, "mewla-friends:DELETE:/friends"); !ok {
			return
		}
		if err := store.Clear(); err != nil {
			http.Error(w, "could not forget the link", http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := friendsStatus{}
	if link, err := store.Link(); err == nil {
		status.Linked, status.Handle = true, link.Handle
		if drafts, err := store.Drafts(); err == nil {
			status.Drafts = len(drafts)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(status)
}
