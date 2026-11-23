package link

import (
	"fmt"
	"sort"
	"strings"
)

// RouteVersion is the version of the route a daemon tells a paired app
// (the server's `link_route` message).
const RouteVersion = 1

// RouteCandidates are where an app reaches this computer through each relay:
// the stable addresses a pairing link names, sorted as a pairing link sorts
// them.
func RouteCandidates(config ConnectorConfig, identity *TransportIdentity) []PairingCandidate {
	candidates := make([]PairingCandidate, 0, len(config.Candidates))
	for _, candidate := range config.Candidates {
		candidates = append(candidates, PairingCandidate{
			Name:      candidate.Name,
			StableURL: candidateURL(StableHost(config, identity, candidate), candidate.ClientPort),
		})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].Name == candidates[right].Name {
			return candidates[left].StableURL < candidates[right].StableURL
		}
		return candidates[left].Name < candidates[right].Name
	})
	return candidates
}

// RouteBindingPayload is what the daemon key signs to tell a paired app this
// computer's Mewla Relay route: an app paired by an address (the hosted https
// one) trusts the route, and the transport key it pins, only on this
// signature, never on the connection it came over.
func RouteBindingPayload(daemonID, daemonPublicKey, routeID, transportPin string, issuedAtMS int64, candidates []PairingCandidate) []byte {
	parts := []string{
		fmt.Sprintf("%d", RouteVersion),
		strings.ToLower(strings.TrimSpace(daemonID)),
		strings.ToLower(strings.TrimSpace(daemonPublicKey)),
		strings.ToLower(strings.TrimSpace(routeID)),
		strings.ToLower(strings.TrimSpace(transportPin)),
		fmt.Sprintf("%d", issuedAtMS),
	}
	for _, candidate := range candidates {
		parts = append(parts, strings.TrimSpace(candidate.Name), strings.TrimSpace(candidate.StableURL))
	}
	return []byte(strings.Join(parts, "\n"))
}
