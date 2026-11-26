package modelprofiles

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/daoleno/mewla/daemon/debuglog"
	"github.com/daoleno/mewla/daemon/modelprofiles/translate"
)

// apiOfProtocol is the translate API a profile protocol speaks.
func apiOfProtocol(protocol string) string {
	switch normalizeID(protocol) {
	case ProtocolAnthropicMessages:
		return translate.APIAnthropic
	case ProtocolOpenAIChat:
		return translate.APIChat
	case ProtocolOpenAIResponses, ProtocolOpenAINative:
		return translate.APIResponses
	}
	return ""
}

// apiOfEndpoint is the API an agent speaks on a route endpoint.
func apiOfEndpoint(endpoint RouteEndpoint) string {
	switch endpoint {
	case EndpointResponses:
		return translate.APIResponses
	case EndpointAnthropicMessages, EndpointAnthropicCountTokens:
		return translate.APIAnthropic
	}
	return ""
}

// translatedForward carries one request an agent sent in its own API to a
// Provider that speaks another, and the reply back.
type translatedForward struct {
	client      *http.Client
	clientAPI   string
	upstreamAPI string
	baseURL     string
	provider    string // the connection's name, for errors
	inbound     http.Header
	body        []byte
	// applyAuth puts the Provider's key on the upstream request.
	applyAuth func(http.Header) error
	// answered is told the upstream's status once it answered (0 when it
	// could not be reached), before anything is written to the client.
	answered func(status int)
}

func (t translatedForward) serve(ctx context.Context, w http.ResponseWriter) {
	answered := func(status int) {
		if t.answered != nil {
			t.answered(status)
			t.answered = nil
		}
	}
	defer answered(0)
	x, err := translate.Prepare(t.clientAPI, t.upstreamAPI, t.body, "")
	if err != nil {
		writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
		return
	}
	target, err := UpstreamRequestURL(t.baseURL, "/v1"+translate.UpstreamPath(t.upstreamAPI))
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrUpstreamInvalid, t.provider, ""))
		return
	}
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(x.Body))
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrUpstreamInvalid, t.provider, ""))
		return
	}
	// The agent's own headers are for its own API (anthropic-beta,
	// OpenAI-Beta, session ids); the Provider gets a plain request.
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", "text/event-stream")
	if ua := t.inbound.Get("User-Agent"); ua != "" {
		upReq.Header.Set("User-Agent", ua)
	}
	if t.upstreamAPI == translate.APIAnthropic {
		upReq.Header.Set("Anthropic-Version", firstNonEmpty(t.inbound.Get("Anthropic-Version"), "2023-06-01"))
	}
	upReq.ContentLength = int64(len(x.Body))
	if err := t.applyAuth(upReq.Header); err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrCredentialNotReady, t.provider, ""))
		return
	}
	resp, err := sendUpstream(t.client, upReq, x.Body, true)
	if err != nil {
		status, typed := classifyUpstreamDoError(err)
		writeRouteError(w, status, forProvider(typed, t.provider, upstreamReachReason(err)))
		return
	}
	defer resp.Body.Close()
	answered(resp.StatusCode)
	usage, replyErr := x.Reply(w, resp)
	debuglog.Printf("translate: %s to %s status=%d in=%d out=%d cache_read=%d cache_write=%d err=%v",
		t.clientAPI, t.upstreamAPI, resp.StatusCode, usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, replyErr)
}

// writeEstimatedTokenCount answers count_tokens for a Provider that cannot
// count: Claude Code decides when to compact from it.
func writeEstimatedTokenCount(w http.ResponseWriter, body []byte) {
	out, err := translate.CountTokens(body)
	if err != nil {
		writeRouteError(w, http.StatusBadRequest, ErrRequestBodyMalformed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// plainModelIDRE is a model id as Claude Code names its own models; a
// marked or mangled one (claude-opus-5-5[2m]) is refused, not replaced.
var plainModelIDRE = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

var claudeFamilyRE = regexp.MustCompile(`^claude-(?:[0-9.-]+-)?(haiku|sonnet|opus|fable)\b`)

// claudeStandIn picks a served model for a Claude model id the connection
// does not offer. Claude Code asks for its own ids on its own (a haiku for
// titles, summaries and Explore subagents, opus for opusplan) whatever the
// person chose; a Provider that serves other models would refuse each of
// those calls. A model the Provider listed of the same family is preferred,
// then fallback (the model the Session or connection already runs on this
// Provider), then the first model listed. ok is false for an id that is not
// a Claude family id, which is refused as before.
func claudeStandIn(requested string, served []string, fallback string) (string, bool) {
	m := claudeFamilyRE.FindStringSubmatch(strings.ToLower(requested))
	fallback = strings.TrimSpace(fallback)
	if m == nil || !plainModelIDRE.MatchString(requested) || (len(served) == 0 && fallback == "") {
		return "", false
	}
	family := m[1]
	for _, id := range served {
		if strings.Contains(strings.ToLower(id), family) {
			return id, true
		}
	}
	if fallback != "" && !claudeFamilyRE.MatchString(strings.ToLower(fallback)) {
		return fallback, true
	}
	if len(served) == 0 {
		return "", false
	}
	return served[0], true
}
