package translate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Supported reports whether a request in API client can be carried to an
// upstream that speaks API upstream. The same API needs no translation.
func Supported(client, upstream string) bool {
	switch client + ">" + upstream {
	case APIAnthropic + ">" + APIChat, APIResponses + ">" + APIChat, APIResponses + ">" + APIAnthropic:
		return true
	}
	return false
}

// UpstreamPath is the path, under the upstream's base URL, a translated
// request goes to.
func UpstreamPath(upstream string) string {
	switch upstream {
	case APIChat:
		return "/chat/completions"
	case APIAnthropic:
		return "/messages"
	}
	return "/responses"
}

// Exchange is one translated request: what the upstream is sent, and how its
// reply is carried back.
type Exchange struct {
	client, upstream string
	model            string
	stream           bool
	names            toolNames
	// Body is the request to send upstream.
	Body []byte
}

// Prepare reads a request the agent sent in API client and writes it in API
// upstream. model, when set, replaces the model the agent named.
func Prepare(client, upstream string, body []byte, model string) (*Exchange, error) {
	if !Supported(client, upstream) {
		return nil, fmt.Errorf("translate: %s to %s is not supported", client, upstream)
	}
	var req *Request
	var err error
	switch client {
	case APIAnthropic:
		req, err = ReadAnthropic(body)
	case APIResponses:
		req, err = ReadResponses(body)
	}
	if err != nil {
		return nil, err
	}
	if model != "" {
		req.Model = model
	}
	x := &Exchange{client: client, upstream: upstream, model: req.Model, stream: req.Stream}
	if client == APIResponses {
		x.names = toolNamesOf(body)
	}
	switch upstream {
	case APIChat:
		x.Body, err = WriteChat(req)
	case APIAnthropic:
		x.Body, err = WriteAnthropic(req)
	}
	if err != nil {
		return nil, err
	}
	return x, nil
}

// KeepAliveEvery is how often a quiet stream gets a keep-alive. Claude Code
// gives up on a stream that is silent for minutes, and a Chat upstream sends
// nothing while a reasoning model thinks.
var KeepAliveEvery = 15 * time.Second

// maxErrorBody bounds how much of an upstream's error reply is read.
const maxErrorBody = 1 << 20

type encoder interface {
	handle(Event)
	finish()
	fail(*Error)
	keepAlive()
	began() bool
}

// Reply carries the upstream's response to w in the agent's API. It returns
// the usage the upstream reported and, for a failure, the error the agent was
// given.
func (x *Exchange) Reply(w http.ResponseWriter, resp *http.Response) (Usage, error) {
	var enc encoder
	var usage Usage
	switch x.client {
	case APIAnthropic:
		enc = newAnthropicEncoder(w, x.model, x.stream)
	default:
		enc = newResponsesEncoder(w, x.model, x.stream, x.upstream == APIAnthropic, x.names)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		e := ReadUpstreamError(resp.StatusCode, raw)
		enc.fail(e)
		return usage, e
	}
	decode := DecodeChat
	if x.upstream == APIAnthropic {
		decode = DecodeAnthropic
	}
	events := make(chan Event, 64)
	done := make(chan error, 1)
	go func() {
		err := decode(resp.Body, resp.Header.Get("Content-Type"), func(e Event) { events <- e })
		close(events)
		done <- err
	}()
	tick := time.NewTicker(KeepAliveEvery)
	defer tick.Stop()
	var failed *Error
	for {
		select {
		case e, ok := <-events:
			if !ok {
				err := <-done
				if failed != nil {
					return usage, failed
				}
				if err != nil {
					cut := &Error{Status: http.StatusBadGateway, Kind: ErrServer, Message: "The Provider's reply was cut off before it ended."}
					if !errors.Is(err, io.ErrUnexpectedEOF) {
						cut.Message = "The Provider's reply could not be read: " + err.Error()
					}
					enc.fail(cut)
					return usage, cut
				}
				enc.finish()
				return usage, nil
			}
			if e.Kind == EvUsage {
				usage.add(e.Usage)
			}
			if e.Kind == EvError {
				if failed == nil {
					failed = e.Err
					enc.fail(e.Err)
				}
				continue
			}
			if failed == nil {
				enc.handle(e)
			}
		case <-tick.C:
			enc.keepAlive()
		}
	}
}

// CountTokens answers an Anthropic count_tokens request for an upstream that
// cannot count: an estimate from the request's size, about four characters a
// token, which is what Claude Code needs to decide when to compact.
func CountTokens(body []byte) ([]byte, error) {
	req, err := ReadAnthropic(body)
	if err != nil {
		return nil, err
	}
	chars := 0
	count := func(parts []Part) {
		for _, p := range parts {
			chars += len(p.Text) + len(p.Args) + len(p.Name)
			for _, r := range p.Result {
				chars += len(r.Text)
				if r.Kind == PartImage {
					chars += 6000
				}
			}
			if p.Kind == PartImage {
				chars += 6000 // ~1.5k tokens an image
			}
			if p.Kind == PartFile {
				chars += len(p.Data) / 2
			}
		}
	}
	count(req.System)
	for _, m := range req.Messages {
		count(m.Parts)
		chars += 16
	}
	for _, t := range req.Tools {
		chars += len(t.Name) + len(t.Description) + len(t.Schema)
	}
	tokens := chars/4 + 1
	return json.Marshal(map[string]int{"input_tokens": tokens})
}

// ProbeEndpoint is a request that tells whether a base URL serves an API
// without asking a model anything: an empty body is refused as invalid (400
// or 422) by an endpoint that exists, and 404 or 405 by one that does not.
func ProbeEndpoint(api string) (path, body string) {
	return UpstreamPath(api), `{}`
}

// EndpointServed reads the answer to ProbeEndpoint: served says whether the
// endpoint exists, sure whether the answer tells (a 5xx does not).
func EndpointServed(status int, body []byte) (served, sure bool) {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return false, true
	}
	if status >= 500 {
		return false, false
	}
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "not found") && strings.Contains(lower, "path") {
		return false, true
	}
	return true, true
}
