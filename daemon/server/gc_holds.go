package server

import (
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/gc"
	"github.com/daoleno/mewla/daemon/logonce"
	"github.com/gorilla/websocket"
)

// holdsFresh is how long a summary of what the collector holds is reused:
// telemetry is read every few seconds, and the summary reads the ledger.
const holdsFresh = 30 * time.Second

type holdsCache struct {
	mu    sync.Mutex
	at    time.Time
	holds *gc.Holds
	log   logonce.Log
}

// gcLookup is where this daemon's collector records are read, with its live
// Sessions and their Work.
func (s *Server) gcLookup() (gc.WhyLookup, bool) {
	if s.watcher == nil {
		return gc.WhyLookup{}, false
	}
	lookup, ok := s.watcher.GCWhyLookup()
	if ok && s.brain != nil {
		lookup.Work = s.brain.SessionWork
	}
	return lookup, ok
}

// holds is what the collector holds and did, for the Resources page; nil
// before the daemon has a state root.
func (s *Server) holds(now time.Time) *gc.Holds {
	s.holdsCache.mu.Lock()
	defer s.holdsCache.mu.Unlock()
	if s.holdsCache.holds != nil && now.Sub(s.holdsCache.at) < holdsFresh {
		return s.holdsCache.holds
	}
	lookup, ok := s.gcLookup()
	if !ok {
		return nil
	}
	h, err := gc.SummarizeHolds(lookup, now)
	s.holdsCache.log.Note("gc holds", err)
	if err != nil {
		return s.holdsCache.holds
	}
	s.holdsCache.holds, s.holdsCache.at = &h, now
	return &h
}

// handleGCRestore puts a trashed item back where it was (the Resources
// page's Restore on something collected today).
func (s *Server) handleGCRestore(conn *websocket.Conn, raw clientMessage) {
	lookup, ok := s.gcLookup()
	if !ok {
		s.sendErrorWithRequestID(conn, raw.RequestID, "gc_unavailable", "Mewla is still starting")
		return
	}
	item, err := gc.Restore(lookup.Root, raw.ID)
	if err != nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "gc_restore_failed", err.Error())
		return
	}
	s.holdsCache.mu.Lock()
	s.holdsCache.holds = nil
	s.holdsCache.mu.Unlock()
	s.sendJSON(conn, map[string]any{"type": "gc_restored", "request_id": raw.RequestID, "id": item.ID, "original": item.Original})
}
