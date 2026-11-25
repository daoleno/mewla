package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/usage"
)

// recordUsage adds an anonymous usage count for this computer (nothing when
// counts are off; see daemon/usage).
func (s *Server) recordUsage(name string, props map[string]string) {
	if s.auth == nil {
		return
	}
	usage.Record(s.auth.StorageDir(), name, props)
}

// pairFailureClass names why a pairing failed (usage.PairFailures).
func pairFailureClass(err error) string {
	switch {
	case errors.Is(err, auth.ErrExpiredPairingToken):
		return "expired"
	case errors.Is(err, auth.ErrInvalidPairingToken):
		return "admission"
	case errors.Is(err, auth.ErrWrongDaemon), errors.Is(err, auth.ErrDeviceKeyMismatch):
		return "pin"
	default:
		return "other"
	}
}

// usagePlatform maps an app's reported platform to usage.Platforms.
func usagePlatform(platform string) string {
	switch platform {
	case "desktop", "macos":
		return "mac"
	default:
		return usage.OneOf(platform, usage.Platforms)
	}
}

const (
	usageGetPurpose  = "mewla-usage:get:GET:/usage"
	usagePostPurpose = "mewla-usage:post:POST:/usage"
)

// usageViewFor is what the app shows: "enabled" (this computer and its
// phones send usage counts) and "locked", what keeps them off regardless of
// the switch ("environment", "ci", "development"), empty when the switch decides.
func usageViewFor(stateDir string) map[string]any {
	switch reason := usage.OffReason(stateDir); reason {
	case "":
		return map[string]any{"enabled": true, "locked": ""}
	case usage.OffBySetting:
		return map[string]any{"enabled": false, "locked": ""}
	default:
		return map[string]any{"enabled": false, "locked": reason}
	}
}

type usageRequest struct {
	// Action is "set" (Enabled) or "app_daily" (the app's day: App, Platform, OSMajor, Version).
	Action   string `json:"action"`
	Enabled  *bool  `json:"enabled,omitempty"`
	App      string `json:"app,omitempty"`
	Platform string `json:"platform,omitempty"`
	OSMajor  string `json:"os_major,omitempty"`
	Version  string `json:"version,omitempty"`
}

// handleUsage is the paired app's view of this computer's usage counts: the
// switch in Settings, and the app's own once-a-day count, which the app
// sends through its computer rather than to any server of its own.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	stateDir := s.auth.StorageDir()
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authenticateRequest(w, r, usageGetPurpose); !ok {
			return
		}
	case http.MethodPost:
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		sum := sha256.Sum256(body)
		if _, ok := s.authenticateRequest(w, r, usagePostPurpose+":sha256="+hex.EncodeToString(sum[:])); !ok {
			return
		}
		var request usageRequest
		if err := json.Unmarshal(body, &request); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		switch request.Action {
		case "set":
			if request.Enabled == nil {
				http.Error(w, "enabled is required", http.StatusBadRequest)
				return
			}
			if err := usage.SetEnabled(stateDir, *request.Enabled); err != nil {
				http.Error(w, "could not save the setting", http.StatusInternalServerError)
				return
			}
		case "app_daily":
			props := map[string]string{"app": request.App, "platform": usagePlatform(request.Platform), "os_major": request.OSMajor, "version": request.Version}
			if usage.Validate(usage.Event{Name: "app_daily", Props: props}) != nil {
				http.Error(w, "not a usage count", http.StatusBadRequest)
				return
			}
			usage.Record(stateDir, "app_daily", props)
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeJSONWithAssertion(w, http.StatusOK, usageGetPurpose, usageViewFor(stateDir))
}
