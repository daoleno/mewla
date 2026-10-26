package friendsvc

import (
	"fmt"
	"net/http"
)

// PathPrefix is where mewla-account mounts the service. Signatures cover the
// full request path, so handlers are registered with it in place.
const PathPrefix = "/friends"

const apiPrefix = PathPrefix + "/v1"

// Class is the credential a route accepts. The zero value is untagged and is
// refused when the service is built.
type Class uint8

const (
	classUntagged Class = iota
	// ClassPublic needs no credential.
	ClassPublic
	// ClassEnroll takes a human-key signature from a key that may not be
	// registered yet; the request must carry a ticket for that key.
	ClassEnroll
	// ClassHuman takes a registered human key.
	ClassHuman
	// ClassAgentLink takes an agent-scheme signature from a key that may not
	// be registered yet, and a link code from a human.
	ClassAgentLink
	// ClassAgent takes a registered agent key.
	ClassAgent
)

func (c Class) String() string {
	switch c {
	case ClassPublic:
		return "public"
	case ClassEnroll:
		return "enroll"
	case ClassHuman:
		return "human"
	case ClassAgentLink:
		return "agent-link"
	case ClassAgent:
		return "agent"
	}
	return "untagged"
}

func (c Class) human() bool  { return c == ClassEnroll || c == ClassHuman }
func (c Class) signed() bool { return c != ClassPublic }

// Principal is who a signed request is from.
type Principal struct {
	// Key is the lower-hex public key that signed the request.
	Key string
	// PersonID is empty for ClassEnroll and ClassAgentLink before the key is
	// registered.
	PersonID string
	Body     []byte
}

// Handler serves one route.
type Handler func(w http.ResponseWriter, r *http.Request, who Principal)

// Route is one entry of the route table.
type Route struct {
	Method  string
	Path    string
	Class   Class
	Handler Handler
}

// Pattern is the ServeMux pattern of the route.
func (r Route) Pattern() string { return r.Method + " " + r.Path }

// validateRoutes refuses an untagged, unhandled or duplicate route.
func validateRoutes(routes []Route) error {
	seen := map[string]bool{}
	for _, route := range routes {
		switch {
		case route.Class == classUntagged || route.Class > ClassAgent:
			return fmt.Errorf("friends route %s has no credential class", route.Pattern())
		case route.Handler == nil:
			return fmt.Errorf("friends route %s has no handler", route.Pattern())
		case route.Method == "" || route.Path == "":
			return fmt.Errorf("friends route %q is incomplete", route.Pattern())
		case seen[route.Pattern()]:
			return fmt.Errorf("friends route %s is registered twice", route.Pattern())
		}
		seen[route.Pattern()] = true
	}
	return nil
}
