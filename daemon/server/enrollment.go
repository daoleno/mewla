package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/enrollment"
	"github.com/gorilla/websocket"
)

const enrollmentDecisionPurpose = "zen-enrollment:decision:POST:/enrollment/decision"

func (s *Server) allowEnrollmentRequest(r *http.Request) bool {
	key, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		key = r.RemoteAddr
	}
	now := time.Now()
	s.enrollmentRateMu.Lock()
	defer s.enrollmentRateMu.Unlock()
	window := s.enrollmentRates[key][:0]
	for _, at := range s.enrollmentRates[key] {
		if now.Sub(at) < time.Minute {
			window = append(window, at)
		}
	}
	if len(window) >= 5 {
		s.enrollmentRates[key] = window
		return false
	}
	s.enrollmentRates[key] = append(window, now)
	return true
}

func (s *Server) handleEnrollmentRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.enrollments == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.allowEnrollmentRequest(r) {
		http.Error(w, "try again later", http.StatusTooManyRequests)
		return
	}
	var raw struct {
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
		Platform        string `json:"platform"`
		Origin          string `json:"origin"`
		DevicePublicKey string `json:"device_public_key"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if json.NewDecoder(r.Body).Decode(&raw) != nil {
		http.Error(w, "invalid enrollment request", http.StatusBadRequest)
		return
	}
	request, secret, err := s.enrollments.Create(raw.DeviceID, raw.DeviceName, raw.Platform, raw.Origin, raw.DevicePublicKey)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "too many") {
			status = http.StatusTooManyRequests
		}
		http.Error(w, err.Error(), status)
		return
	}
	s.broadcastEnrollmentRequest(request)
	s.writeJSONWithAssertion(w, http.StatusCreated, "zen-enrollment-request", map[string]any{"request_id": request.ID, "secret": secret, "verification_number": request.Number, "expires_at": request.ExpiresAt})
}

func (s *Server) handleEnrollmentStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || s.enrollments == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	request, ok := s.enrollments.Status(r.URL.Query().Get("id"), r.URL.Query().Get("secret"))
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	response := map[string]any{"status": request.Status, "expires_at": request.ExpiresAt}
	if request.Status == enrollment.Approved {
		response["device_id"] = request.DeviceID
		response["daemon_id"] = s.auth.DaemonID()
		response["daemon_public_key"] = s.auth.PublicKeyHex()
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "zen-enrollment-status", response)
}

func (s *Server) handleEnrollmentPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || s.enrollments == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authenticateRequest(w, r, enrollmentDecisionPurpose); !ok {
		return
	}
	requests, err := s.enrollments.List()
	if err != nil {
		http.Error(w, "enrollment unavailable", http.StatusInternalServerError)
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "zen-enrollment-pending", map[string]any{"requests": requests})
}

func (s *Server) handleEnrollmentDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.enrollments == nil {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	approver, ok := s.authenticateRequest(w, r, enrollmentDecisionPurpose)
	if !ok {
		return
	}
	var raw struct {
		RequestID string `json:"request_id"`
		Number    string `json:"verification_number"`
		Approve   *bool  `json:"approve"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if json.NewDecoder(r.Body).Decode(&raw) != nil || strings.TrimSpace(raw.RequestID) == "" || raw.Approve == nil {
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	request, err := s.enrollments.Decide(raw.RequestID, raw.Number, approver.ID, *raw.Approve)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if *raw.Approve {
		token, tokenErr := s.auth.IssuePairingToken(auth.DefaultPairingTTL)
		if tokenErr != nil {
			http.Error(w, "enrollment unavailable", http.StatusInternalServerError)
			return
		}
		if _, enrollErr := s.auth.EnrollDevice(token.Value, s.auth.DaemonID(), s.auth.PublicKeyHex(), request.DeviceID, request.DeviceName, request.DevicePublicKey); enrollErr != nil {
			http.Error(w, "enrollment unavailable", http.StatusInternalServerError)
			return
		}
	}
	s.broadcastEnrollmentDecision(request)
	s.writeJSONWithAssertion(w, http.StatusOK, "zen-enrollment-decision", map[string]any{"status": request.Status, "request_id": request.ID})
}

func (s *Server) broadcastEnrollmentRequest(request enrollment.Request) {
	s.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		clients = append(clients, conn)
	}
	s.mu.Unlock()
	payload := map[string]any{"type": "enrollment_request", "request_id": request.ID, "device_id": request.DeviceID, "device_name": request.DeviceName, "platform": request.Platform, "origin": request.Origin, "verification_number": request.Number, "expires_at": request.ExpiresAt}
	for _, conn := range clients {
		s.sendJSON(conn, payload)
	}
}

func (s *Server) broadcastEnrollmentDecision(request enrollment.Request) {
	s.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		clients = append(clients, conn)
	}
	s.mu.Unlock()
	payload := map[string]any{"type": "enrollment_decision", "request_id": request.ID, "device_id": request.DeviceID, "status": request.Status}
	for _, conn := range clients {
		s.sendJSON(conn, payload)
	}
}
