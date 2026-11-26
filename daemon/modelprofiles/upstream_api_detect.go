package modelprofiles

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/modelprofiles/translate"
)

// upstreamAPIProbeTimeout bounds each endpoint probe.
const upstreamAPIProbeTimeout = 8 * time.Second

// detectUpstreamAPI finds which API a custom endpoint speaks, without asking
// a model anything: each candidate endpoint is sent an empty request, which
// one that exists refuses as invalid (400/422) and one that does not answers
// 404 or 405. The agent's own API is tried first, so an endpoint that speaks
// it is never translated. For Codex, Anthropic Messages comes before Chat
// Completions: thinking and prompt caching survive it, and an Anthropic
// endpoint often offers a lossy Chat one beside it. It returns the profile
// protocol to store ("" for the agent's own) and whether it could tell.
func detectUpstreamAPI(client *http.Client, executorID, baseURL, secret string) (string, bool) {
	var order []string
	switch executorID {
	case ExecutorCodex:
		order = []string{ProtocolOpenAIResponses, ProtocolAnthropicMessages, ProtocolOpenAIChat}
	case ExecutorClaude:
		order = []string{ProtocolAnthropicMessages, ProtocolOpenAIChat}
	default:
		return "", false
	}
	for i, protocol := range order {
		served, answered := probeUpstreamAPI(client, baseURL, apiOfProtocol(protocol), secret)
		if !answered {
			return "", false
		}
		if served {
			if i == 0 {
				return "", true
			}
			return protocol, true
		}
	}
	return "", false
}

func probeUpstreamAPI(client *http.Client, baseURL, api, secret string) (served, answered bool) {
	path, body := translate.ProbeEndpoint(api)
	target, err := UpstreamRequestURL(baseURL, "/v1"+path)
	if err != nil {
		return false, false
	}
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return false, false
	}
	req.Header.Set("Content-Type", "application/json")
	if api == translate.APIAnthropic {
		req.Header.Set("X-Api-Key", secret)
		req.Header.Set("Anthropic-Version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	return translate.EndpointServed(resp.StatusCode, bytes.TrimSpace(raw))
}
