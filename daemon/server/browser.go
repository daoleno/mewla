package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/daoleno/mewla/daemon/browser"
)

func (s *Server) SetBrowser(manager *browser.Manager) { s.browsers = manager }

// handleBrowser is the app's say over the agent browser: the Virtual screen
// switch. The browser itself is a window on the agent screen, which the
// display routes list and show.
func (s *Server) handleBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authenticateRequest(w, r, "mewla-browser"); !ok {
		return
	}
	if s.browsers == nil {
		http.Error(w, "Browser is unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var q browser.Request
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil || q.Action != "settings" {
		http.Error(w, "Invalid browser request", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.browsers.Handle(ctx, q)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
