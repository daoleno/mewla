package modelprofiles

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/debuglog"
	"github.com/daoleno/mewla/daemon/modelprofiles/translate"
)

// A connection may name a fallback. When it fails a model request, after
// sendUpstream asked it again, and nothing has reached the agent yet, the
// request goes to the fallback, and the failing connection rests: while it
// rests its requests go straight to the fallback, and when the rest is over
// they go back to it.
//
// One pair is never moved: a connection and a fallback that both speak the
// agent's own API (say two Anthropic endpoints for Claude Code). Each would
// refuse what the other leaves in the agent's history (Claude's thinking
// signatures, Codex's encrypted reasoning), so the conversation would break
// when it came back. Every other pair is safe: translation drops the other
// side's state on the way out, and NativeHistory drops the translation's on
// the way back.

// How long a failing connection rests, by what it said.
const (
	restBusy    = 30 * time.Second // a 5xx, or no answer at all
	restRate    = time.Minute      // a rate limit that does not say until when
	restLongest = 10 * time.Minute // a failure that keeps coming back
	restQuota   = 15 * time.Minute // out of credit, balance or quota
	restRefused = 10 * time.Minute // the key is refused
	restRateMax = time.Hour        // the most a Retry-After is believed
)

// failoverRest says whether a failure is one the fallback may not share, and
// how long the connection rests: first base, then twice as long each time it
// fails again, up to most.
func failoverRest(status int, header http.Header, body []byte, err error) (base, most time.Duration, ok bool) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0, 0, false // the agent went away
		}
		return restBusy, restLongest, true
	}
	if quotaUsedUp(body) && (status == http.StatusBadRequest || status == http.StatusPaymentRequired ||
		status == http.StatusForbidden || status == http.StatusTooManyRequests) {
		return restQuota, restQuota, true
	}
	switch {
	case status == http.StatusPaymentRequired:
		return restQuota, restQuota, true
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return restRefused, restRefused, true
	case status == http.StatusTooManyRequests:
		if wait, known := upstreamRetryAfter(header, time.Now()); known && wait > 0 {
			wait = min(wait, restRateMax)
			return wait, wait, true
		}
		return restRate, restLongest, true
	case status == http.StatusRequestTimeout, status >= 500:
		return restBusy, restLongest, true
	}
	return 0, 0, false
}

// connectionRests is when each failing connection may be asked again.
type connectionRests struct {
	mu   sync.Mutex
	now  func() time.Time
	byID map[string]connectionRest
}

type connectionRest struct {
	until   time.Time
	strikes int
}

func newConnectionRests() *connectionRests {
	return &connectionRests{now: time.Now, byID: map[string]connectionRest{}}
}

func (r *connectionRests) resting(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rest, ok := r.byID[normalizeID(id)]
	return ok && r.now().Before(rest.until)
}

func (r *connectionRests) fail(id string, base, most time.Duration) time.Duration {
	if r == nil || id == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rest := r.byID[normalizeID(id)]
	wait := base
	for i := 0; i < rest.strikes && wait < most; i++ {
		wait *= 2
	}
	wait = min(wait, most)
	rest.strikes++
	rest.until = r.now().Add(wait)
	r.byID[normalizeID(id)] = rest
	return wait
}

func (r *connectionRests) ok(id string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, normalizeID(id))
}

// settle records how a connection answered.
func (r *connectionRests) settle(id string, status int, header http.Header, body []byte, err error) {
	if err == nil && status >= 200 && status < 300 {
		r.ok(id)
		return
	}
	if base, most, ok := failoverRest(status, header, body, err); ok {
		r.fail(id, base, most)
	}
}

// failoverTarget is a fallback connection, ready to send to.
type failoverTarget struct {
	GatewayUpstream
	// Model is the fallback connection's own model, "" when the request
	// names it.
	Model string
	// Listed are the models the fallback's Provider listed.
	Listed []string
}

// failover moves requests from a failing connection to its fallback. The
// router and the gateway share one, so a connection rests for both.
type failover struct {
	rests *connectionRests
	// fallbackOf gives a connection's fallback, when it has a ready one.
	fallbackOf func(profileID string) (failoverTarget, bool)
}

// forwardVia is how the caller reaches upstreams.
type forwardVia struct {
	client *http.Client
	creds  CredentialStore
	lookup func(string) (string, bool)
}

// failoverRequest is one model request that may be moved.
type failoverRequest struct {
	from      GatewayUpstream // the connection it was for
	clientAPI string
	apiPath   string // the agent's path, as /v1/messages
	body      []byte // plain JSON, as it would go to from
	model     string // the model the agent asked for
	inbound   http.Header
}

// resting reports whether a connection rests. The caller still asks send,
// which answers false when there is no fallback to take the request; then
// the request goes to the resting connection after all.
func (f *failover) resting(profileID string) bool {
	return f != nil && f.rests.resting(profileID)
}

// fromFailure rests the connection that failed and sends the request to its
// fallback. It returns false, having written nothing, when the failure is
// the request's own or there is no fallback to send it to.
func (f *failover) fromFailure(ctx context.Context, w http.ResponseWriter, via forwardVia, r failoverRequest, status int, header http.Header, body []byte, err error) bool {
	if f == nil {
		return false
	}
	base, most, ok := failoverRest(status, header, body, err)
	if !ok {
		return false
	}
	wait := f.rests.fail(r.from.ProfileID, base, most)
	moved := f.send(ctx, w, via, r)
	if moved {
		log.Printf("model providers: connection %q failed (%s); it rests %s and its requests go to its fallback", r.from.Name, failureWords(status, err), wait.Round(time.Second))
	}
	return moved
}

// send sends the request to the fallback of r.from. It returns false, having
// written nothing, when there is none it may go to.
func (f *failover) send(ctx context.Context, w http.ResponseWriter, via forwardVia, r failoverRequest) bool {
	if f == nil || f.fallbackOf == nil || r.clientAPI == "" {
		return false
	}
	target, ok := f.fallbackOf(r.from.ProfileID)
	if !ok || normalizeID(target.ProfileID) == normalizeID(r.from.ProfileID) || f.rests.resting(target.ProfileID) {
		return false
	}
	upstreamAPI := apiOfProtocol(target.Protocol)
	if upstreamAPI == r.clientAPI && apiOfProtocol(r.from.Protocol) == r.clientAPI {
		debuglog.Printf("failover: %q and its fallback %q both speak the agent's own API; the request stays", r.from.Name, target.Name)
		return false
	}
	if upstreamAPI != r.clientAPI && !translate.Supported(r.clientAPI, upstreamAPI) {
		return false
	}
	body := r.body
	if model := failoverModel(r.clientAPI, r.model, target); model != "" && model != r.model {
		if rewritten, err := rewriteRequestModel(body, model); err == nil {
			body = rewritten
		}
	}
	applyAuth := func(h http.Header) error {
		return applyGatewayAuth(h, target.GatewayUpstream, r.inbound, via.lookup, via.creds)
	}
	if upstreamAPI != r.clientAPI {
		translatedForward{
			client: via.client, clientAPI: r.clientAPI, upstreamAPI: upstreamAPI,
			baseURL: target.BaseURL, provider: target.Name, inbound: r.inbound, body: body,
			applyAuth: applyAuth,
			answered: func(status int) {
				var err error
				if status == 0 {
					err = ErrUpstreamInvalid
				}
				// Only the status reaches here, so a fallback that is rate
				// limited or out of quota gets the default rest for it.
				f.rests.settle(target.ProfileID, status, nil, nil, err)
			},
		}.serve(ctx, w)
		return true
	}
	// The fallback speaks the agent's own API (the connection that failed
	// was translated): the request goes as it is, without what only the
	// translation wrote.
	body = translate.NativeHistory(r.clientAPI, body)
	target2, err := UpstreamRequestURL(target.BaseURL, r.apiPath)
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrUpstreamInvalid, target.Name, ""))
		return true
	}
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target2, bytes.NewReader(body))
	if err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrUpstreamInvalid, target.Name, ""))
		return true
	}
	copyInboundHeaders(upReq.Header, r.inbound)
	upReq.Header.Del("Content-Encoding")
	upReq.Header.Set("Content-Type", "application/json")
	upReq.ContentLength = int64(len(body))
	if err := applyAuth(upReq.Header); err != nil {
		writeRouteError(w, http.StatusBadGateway, forProvider(ErrCredentialNotReady, target.Name, ""))
		return true
	}
	resp, err := sendUpstream(via.client, upReq, body, true)
	if err != nil {
		f.rests.settle(target.ProfileID, 0, nil, nil, err)
		status, typed := classifyUpstreamDoError(err)
		writeRouteError(w, status, forProvider(typed, target.Name, upstreamReachReason(err)))
		return true
	}
	defer resp.Body.Close()
	f.rests.settle(target.ProfileID, resp.StatusCode, resp.Header, nil, nil)
	copySafeResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_ = streamCopyFlush(w, resp.Body)
	return true
}

// failoverModel is the model a request goes to the fallback as: the one the
// agent asked for when the fallback lists it (or lists nothing); for a
// Claude id it does not list, a Claude stand-in; else the fallback's own.
func failoverModel(clientAPI, asked string, target failoverTarget) string {
	if len(target.Listed) == 0 {
		return firstNonEmpty(target.Model, asked)
	}
	for _, id := range target.Listed {
		if id == asked {
			return asked
		}
	}
	if clientAPI == translate.APIAnthropic {
		if standIn, ok := claudeStandIn(asked, target.Listed, target.Model); ok {
			return standIn
		}
	}
	return firstNonEmpty(target.Model, asked)
}

func failureWords(status int, err error) string {
	if err != nil {
		if reason := upstreamReachReason(err); reason != "" {
			return reason
		}
		return "no answer"
	}
	return strconv.Itoa(status) + " " + http.StatusText(status)
}

// peekFailure reads a failed reply's body (bounded) for failoverRest and
// puts it back, so the reply can still go to the agent as it came.
func peekFailure(resp *http.Response) []byte {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, upstreamPeekLimit))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(raw), resp.Body), resp.Body}
	return raw
}
