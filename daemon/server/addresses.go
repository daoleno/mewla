package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/daoleno/mewla/daemon/addressbook"
)

const addressBookPurpose = "zen-address-book"

func (s *Server) handleAddresses(w http.ResponseWriter, r *http.Request) {
	if s.addresses == nil {
		http.Error(w, "address book unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := s.authenticateRequest(w, r, addressBookPurpose); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		entries, err := s.addresses.List()
		if err != nil {
			http.Error(w, "address book unavailable", http.StatusInternalServerError)
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, addressBookPurpose, map[string]any{"addresses": entries})
	case http.MethodPost:
		var raw struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&raw) != nil {
			http.Error(w, "invalid address", http.StatusBadRequest)
			return
		}
		entry, err := s.addresses.Add(strings.TrimSpace(raw.URL), addressbook.SourceManual)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeJSONWithAssertion(w, http.StatusCreated, addressBookPurpose, map[string]any{"address": entry})
	case http.MethodDelete:
		var raw struct {
			URL string `json:"url"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&raw) != nil {
			http.Error(w, "invalid address", http.StatusBadRequest)
			return
		}
		if err := s.addresses.Remove(raw.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.writeJSONWithAssertion(w, http.StatusOK, addressBookPurpose, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
