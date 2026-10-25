package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/catpark"
	"github.com/daoleno/mewla/daemon/debuglog"
)

// The cat: Brain's real Work as a small life (it leaves, sends postcards,
// comes home). This file owns the daemon side; server.go carries only the
// route line, and live_state.go the observer and dispatch lines.

const (
	catBundlePrefix  = "/cat/"
	catBundlePurpose = "mewla-cat"
)

type catHub struct {
	once     sync.Once
	service  *catpark.Service
	initErr  error
	manifest catpark.Manifest
	manErr   error
	archive  []byte
}

func (s *Server) catRuntime() *catHub {
	hub := &s.cat
	hub.once.Do(func() {
		hub.manifest, hub.manErr = catpark.BuildManifest(catpark.Bundle())
		if hub.manErr == nil {
			hub.archive, hub.manErr = catpark.Archive(catpark.Bundle(), hub.manifest)
		}
		dir := ""
		if s.auth != nil {
			dir = s.auth.StorageDir()
		}
		hub.service, hub.initErr = catpark.Open(dir, s.catLookup, func(view catpark.View) {
			s.broadcastJSON(map[string]any{"type": "cat_state", "cat": view})
		})
	})
	return hub
}

func (s *Server) catLookup(workID string) catpark.Outcome {
	if s.brain == nil {
		return catpark.OutcomeOpen
	}
	work, err := s.brain.WorkByID(workID)
	return catpark.OutcomeOf(work, err)
}

// startCat reads Brain once at boot, so trips that ended while the daemon was
// down are settled before anyone asks; the live safety net keeps it current.
func (s *Server) startCat(ctx context.Context) {
	go func() {
		if snapshot, err := s.liveSnapshot(); err == nil {
			s.observeCat(snapshot)
		}
	}()
}

// observeCat is called wherever a Brain snapshot is broadcast. The cat never
// takes anything down with it.
func (s *Server) observeCat(snapshot brain.Snapshot) {
	defer func() {
		if recovered := recover(); recovered != nil {
			debuglog.Printf("cat: observe panicked: %v", recovered)
		}
	}()
	if hub := s.catRuntime(); hub.service != nil {
		hub.service.Observe(snapshot)
	}
}

type catClientMessage struct {
	RequestID string `json:"request_id"`
	// Act is one thing the person did in the house (catpark.Acts); the
	// daemon ignores anything it does not know.
	Act string `json:"act"`
	Arg string `json:"arg"`
	// On is the Settings → Labs switch carried by cat_labs.
	On bool `json:"on"`
}

// handleCatMessage serves the cat_* messages and reports whether it did.
func (s *Server) handleCatMessage(conn *websocket.Conn, msg []byte, raw clientMessage) bool {
	switch raw.Type {
	case "cat_get", "cat_act", "cat_labs":
	default:
		return false
	}
	var message catClientMessage
	if err := json.Unmarshal(msg, &message); err != nil {
		return false
	}
	hub := s.catRuntime()
	if hub.service == nil {
		s.sendErrorWithRequestID(conn, message.RequestID, "cat_unavailable", "the cat has no home on this computer")
		return true
	}
	switch raw.Type {
	case "cat_act":
		hub.service.Act(message.Act, message.Arg)
	case "cat_labs":
		hub.service.SetEnabled(message.On)
		fallthrough // switched on, the cat looks at Brain at once
	case "cat_get":
		if snapshot, err := s.liveSnapshot(); err == nil {
			hub.service.Observe(snapshot)
		}
	}
	s.sendJSON(conn, map[string]any{"type": "cat_state", "request_id": message.RequestID, "cat": hub.service.View()})
	return true
}

var catContentTypes = map[string]string{
	".css": "text/css; charset=utf-8", ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".json": "application/json", ".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp",
	".woff2": "font/woff2", ".ttf": "font/ttf",
}

// catBundleAllowed admits a browser on a web UI origin (the same people who
// may load the web UI) or a phone with a signed request for this purpose.
func (s *Server) catBundleAllowed(r *http.Request) bool {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		if s.auth == nil {
			return false
		}
		_, err := s.auth.VerifyAuthorization(header, catBundlePurpose, 5*time.Minute)
		return err == nil
	}
	return s.webUIAdmitted(r)
}

// handleCatBundle serves the game page and its art from the daemon binary.
// Only /cat/ may be framed, and only by the web UI itself.
func (s *Server) handleCatBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.catBundleAllowed(r) {
		http.NotFound(w, r)
		return
	}
	hub := s.catRuntime()
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, catBundlePrefix)), "/")
	header := w.Header()
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Frame-Options", "SAMEORIGIN")
	header.Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'self'",
	}, "; "))
	if name == "manifest.json" {
		if hub.manErr != nil {
			http.Error(w, "cat bundle unavailable", http.StatusInternalServerError)
			return
		}
		header.Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hub.manifest)
		return
	}
	if name == catpark.ArchiveName {
		if hub.manErr != nil {
			http.Error(w, "cat bundle unavailable", http.StatusInternalServerError)
			return
		}
		header.Set("Content-Type", "application/x-tar")
		if r.Method == http.MethodGet {
			_, _ = w.Write(hub.archive)
		}
		return
	}
	if name == "" {
		name = "index.html"
	}
	data, err := fs.ReadFile(catpark.Bundle(), name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || path.Ext(name) == "" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "cat bundle unavailable", http.StatusInternalServerError)
		return
	}
	contentType := catContentTypes[path.Ext(name)]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}
