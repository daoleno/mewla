package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/gorilla/websocket"
)

func (s *Server) SetResourceSampler(sampler *watcher.ResourceSampler) { s.resourceSampler = sampler }

func (s *Server) resourceTelemetryPayload(requestID string) map[string]any {
	snap := s.resourceSampler.Snapshot()
	if s.brain != nil {
		if consumers, _, _, err := s.brain.ResourceWorkContext(snap.Consumers); err == nil {
			snap.Consumers = consumers
		}
	}
	raw, _ := json.Marshal(snap)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	payload["type"] = "resource_telemetry"
	if requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}
func (s *Server) handleGetResourceTelemetry(conn *websocket.Conn, raw clientMessage) {
	if s.resourceSampler == nil || s.resourceSampler.Snapshot().SampledAt.IsZero() {
		s.sendErrorWithRequestID(conn, raw.RequestID, "resource_telemetry_unavailable", "The first resource sample is not available yet")
		return
	}
	s.sendJSON(conn, s.resourceTelemetryPayload(raw.RequestID))
}
func (s *Server) handleResourceTelemetryHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authenticateRequest(w, r, "zen-resource-telemetry"); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.resourceSampler == nil || s.resourceSampler.Snapshot().SampledAt.IsZero() {
		http.Error(w, "resource telemetry unavailable", http.StatusServiceUnavailable)
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, "zen-resource-telemetry", s.resourceTelemetryPayload(""))
}
func (s *Server) runResourceTelemetry(ctx context.Context) {
	if s.resourceSampler == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	wake := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				if s.brain == nil {
					continue
				}
				for _, event := range s.resourceSampler.PendingResourceEvents() {
					if ctx.Err() != nil {
						return
					}
					if err := s.brain.RouteResourcePressure(event); err != nil {
						log.Printf("brain resource pressure delivery deferred: %v", err)
						break
					}
					if err := s.resourceSampler.AcknowledgeResourceEvent(event.SampledAt); err != nil {
						log.Printf("resource event acknowledgement persistence: %v", err)
					}
				}
			}
		}
	}()
	defer func() { <-done }()
	for {
		s.resourceSampler.Sample(s.watcher, time.Now().UTC())
		select {
		case wake <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
