package modelprofiles

// upstreamAdapters hold the request reshaping one provider's upstream needs
// before a request is forwarded, keyed by the route's provider ID (the
// preset's ProviderID). The router asks the registry; it names no provider.
// A provider with no entry is forwarded as the client sent it.
var upstreamAdapters = map[string]upstreamAdapter{
	// DeepSeek's Responses endpoint takes a subset of the shape Codex sends;
	// the rest is removed or refused with a clear error.
	"deepseek": {
		Responses: func(body []byte) ([]byte, error) {
			return sanitizeDeepSeekResponsesRequest(body, envelopeDeepSeekV4Flash())
		},
	},
}

type upstreamAdapter struct {
	// Responses reshapes an OpenAI Responses request body. An error that
	// wraps ErrResponsesFeatureUnsupported is answered as such; any other is a
	// malformed body.
	Responses func(body []byte) ([]byte, error)
}

// responsesAdapterFor returns the Responses request adapter of the binding's
// provider, or nil when its upstream takes the request as is.
func responsesAdapterFor(b RouteBinding) func([]byte) ([]byte, error) {
	if normalizeID(b.RouteProtocol) != RouteProtocolResponses {
		return nil
	}
	return upstreamAdapters[normalizeID(b.ProviderID)].Responses
}
