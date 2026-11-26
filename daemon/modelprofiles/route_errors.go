package modelprofiles

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
)

// ErrNoProviderSelected answers a gateway request for an agent that has no
// Provider chosen in Mewla.
var ErrNoProviderSelected = errors.New("no Provider selected")

// providerError names the Provider a local failure is about and, for an
// upstream that could not be reached, why. Neither ever holds a URL or a key.
type providerError struct {
	err    error
	name   string
	reason string
}

func (e providerError) Error() string { return e.err.Error() }
func (e providerError) Unwrap() error { return e.err }

// forProvider attaches the Provider's name (and an optional reason) to err.
func forProvider(err error, name, reason string) error {
	if err == nil {
		return nil
	}
	return providerError{err: err, name: normalizeSpace(name), reason: reason}
}

// writeRouteError answers a request the gateway or a Session route could not
// forward. The body keeps the shape both agents read ({"error":{"type",
// "message"}}: Claude Code and Codex print error.message) and says what is
// wrong and where to fix it, never "request failed".
//
// A failure that asking again cannot fix (no key, no Provider, an address
// that is not allowed) is answered with a status the agents do not retry:
// Claude Code retries a 5xx or 401 for minutes before it says anything and
// prints a 403 at once; Codex retries a 5xx 5 times over ~25 s and prints a
// 400 or 403 within seconds.
func writeRouteError(w http.ResponseWriter, status int, err error) {
	code := routeErrorCode(err)
	var named providerError
	errors.As(err, &named)
	switch code {
	case "credential_not_ready":
		status = http.StatusForbidden
	case "no_provider_selected", "upstream_ssrf", "upstream_redirect":
		status = http.StatusBadRequest
	}
	payload, marshalErr := json.Marshal(map[string]any{"error": map[string]string{
		"type":    code,
		"code":    code,
		"message": routeErrorMessage(code, named.name, named.reason, err),
	}})
	if marshalErr != nil {
		payload = []byte(`{"error":{"type":"route_error","message":"Mewla's gateway could not forward the request."}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func routeErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNoProviderSelected):
		return "no_provider_selected"
	case errors.Is(err, ErrRouteNotFound):
		return "route_not_found"
	case errors.Is(err, ErrRouteAdmissionDenied):
		return "route_admission_denied"
	case errors.Is(err, ErrRoutePathMismatch):
		return "route_path_mismatch"
	case errors.Is(err, ErrRouteProtocolMismatch):
		return "route_protocol_mismatch"
	case errors.Is(err, ErrRouteMethodMismatch):
		return "route_method_mismatch"
	case errors.Is(err, ErrRouteWebSocket):
		return "route_websocket_rejected"
	case errors.Is(err, ErrRequestBodyTooLarge):
		return "request_body_too_large"
	case errors.Is(err, ErrRequestBodyMalformed):
		return "request_body_malformed"
	case errors.Is(err, ErrRequestBodyNotPortable):
		return "request_body_not_portable"
	case errors.Is(err, ErrResponsesFeatureUnsupported):
		return "responses_feature_unsupported"
	case errors.Is(err, ErrCredentialNotReady):
		return "credential_not_ready"
	case errors.Is(err, ErrUpstreamInvalid):
		return "upstream_invalid"
	case errors.Is(err, ErrUpstreamSSRF):
		return "upstream_ssrf"
	case errors.Is(err, ErrUpstreamRedirect):
		return "upstream_redirect"
	case errors.Is(err, ErrBindingBusy):
		return "route_busy"
	}
	return "route_error"
}

func routeErrorMessage(code, name, reason string, err error) string {
	provider := "the Provider"
	if name != "" {
		provider = "Provider " + strconv.Quote(name)
	}
	switch code {
	case "no_provider_selected":
		return "No Provider is chosen for this agent in Mewla. Choose one in Mewla: Settings › Model Providers."
	case "credential_not_ready":
		return fmt.Sprintf("Mewla has no API key for %s. Add it in Mewla: Settings › Model Providers.", provider)
	case "upstream_invalid":
		if reason != "" {
			return fmt.Sprintf("Mewla could not reach %s: %s.", provider, reason)
		}
		return fmt.Sprintf("Mewla could not reach %s.", provider)
	case "upstream_ssrf":
		return fmt.Sprintf("%s's address is not one Mewla connects to (a link-local or cloud metadata address). Check its endpoint in Mewla.", capitalFirst(provider))
	case "upstream_redirect":
		return fmt.Sprintf("%s answered with a redirect, which Mewla does not follow. Check its endpoint in Mewla.", capitalFirst(provider))
	case "route_not_found":
		return "This Session's route in Mewla has ended. Start the Session again."
	case "route_admission_denied":
		return "Mewla's gateway only answers programs on this computer."
	case "route_path_mismatch":
		return "Mewla's gateway does not serve this path."
	case "route_protocol_mismatch":
		return fmt.Sprintf("%s does not speak the API this request uses.", capitalFirst(provider))
	case "route_method_mismatch":
		return "Mewla's gateway does not take this method here."
	case "route_websocket_rejected":
		return "This route does not take a WebSocket."
	case "request_body_too_large":
		return "The request is larger than Mewla's gateway takes."
	case "request_body_malformed":
		return "The request body is not the JSON this API takes."
	case "request_body_not_portable":
		return "This conversation's history cannot move to the newly chosen Provider. Start a new conversation."
	case "responses_feature_unsupported":
		detail := strings.TrimSpace(strings.TrimPrefix(err.Error(), ErrResponsesFeatureUnsupported.Error()))
		detail = strings.TrimSpace(strings.TrimPrefix(detail, ":"))
		if detail != "" {
			return fmt.Sprintf("%s does not support this request: %s.", capitalFirst(provider), detail)
		}
		return fmt.Sprintf("%s does not support this request.", capitalFirst(provider))
	case "route_busy":
		return "The Session is moving to another Provider. Try again in a moment."
	}
	return "Mewla's gateway could not forward the request."
}

func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// upstreamReachReason says in a few words why an upstream could not be
// reached, from the transport error (never its URL).
func upstreamReachReason(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "its host name does not resolve"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "the connection was refused (nothing listens at its address)"
	case errors.Is(err, syscall.ECONNRESET):
		return "the connection was reset"
	case errors.As(err, &certErr):
		return "its TLS certificate is not valid"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "it did not answer in time"
	case strings.Contains(strings.ToLower(err.Error()), "tls"):
		return "the TLS handshake failed"
	case strings.Contains(err.Error(), "EOF"):
		return "it closed the connection without an answer"
	}
	return ""
}
