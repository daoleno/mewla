package server

import (
	"encoding/json"
	"time"

	"github.com/daoleno/mewla/daemon/whatsnew"
	"github.com/gorilla/websocket"
)

// SetWhatsNew installs the record of what is news to this person after an
// update (whatsnew.Store); the app reads it to show its What's New sheet once.
func (s *Server) SetWhatsNew(store *whatsnew.Store) { s.whatsNew = store }

// whatsNewMessage is the part of a whats_new_* message the shared
// clientMessage has no field for.
type whatsNewMessage struct {
	IDs     []string `json:"ids"`
	Version string   `json:"version"`
}

// handleWhatsNew answers whats_new_get (the record and every release's
// line and fixes, without highlights, for an app older than this computer), whats_new_seen (ids the
// person has seen, added to the record) and whats_new_note (a version the
// app updated to). Each replies whats_new with the record.
func (s *Server) handleWhatsNew(conn *websocket.Conn, raw clientMessage, msg []byte) {
	if s.whatsNew == nil {
		s.sendErrorWithRequestID(conn, raw.RequestID, "whats_new_unavailable", "What's New is not available on this computer")
		return
	}
	var body whatsNewMessage
	_ = json.Unmarshal(msg, &body)
	record := s.whatsNew.Record()
	var err error
	switch raw.Type {
	case "whats_new_seen":
		if len(body.IDs) > 512 {
			body.IDs = body.IDs[:512]
		}
		record, err = s.whatsNew.MarkSeen(body.IDs)
	case "whats_new_note":
		record, err = s.whatsNew.NoteUpdate(body.Version, time.Now())
	}
	if err != nil && raw.Type == "whats_new_seen" {
		s.sendErrorWithRequestID(conn, raw.RequestID, "whats_new_failed", err.Error())
		return
	}
	reply := map[string]any{"type": "whats_new", "request_id": raw.RequestID, "record": record}
	if raw.Type == "whats_new_get" {
		reply["releases"] = whatsnew.Notes()
	}
	s.sendJSON(conn, reply)
}
