package modelprofiles

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// LiveRouteEndpoint is the address a surviving client actually uses, coupled
// to its opaque route capability. It carries no upstream credentials.
type LiveRouteEndpoint struct {
	RouteID string
	Addr    string
}

func claudeRouteEndpoint(argv []string) (LiveRouteEndpoint, bool) {
	if len(argv) == 0 {
		return LiveRouteEndpoint{}, false
	}
	executable := filepath.Base(argv[0])
	if executable != "claude" && executable != "cc" {
		return LiveRouteEndpoint{}, false
	}
	var raw string
	for i := 1; i < len(argv); i++ {
		if argv[i] == "--" {
			break
		}
		if argv[i] == "--settings" && i+1 < len(argv) {
			raw = argv[i+1]
			break
		}
		if strings.HasPrefix(argv[i], "--settings=") {
			raw = strings.TrimPrefix(argv[i], "--settings=")
			break
		}
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return LiveRouteEndpoint{}, false
	}
	u, err := url.Parse(settings.Env[EnvAnthropicBaseURL])
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return LiveRouteEndpoint{}, false
	}
	// Only literal loopback addresses can become listening capabilities. Never
	// resolve a process-supplied hostname or widen exposure to another interface.
	host, port, err := net.SplitHostPort(u.Host)
	ip := net.ParseIP(host)
	n, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || n < 1 || n > 65535 {
		return LiveRouteEndpoint{}, false
	}
	route := strings.TrimPrefix(u.Path, RoutePathPrefix)
	if route == u.Path || route == "" || strings.ContainsAny(route, "/\\?#") {
		return LiveRouteEndpoint{}, false
	}
	return LiveRouteEndpoint{RouteID: route, Addr: net.JoinHostPort(host, port)}, true
}

// restoreLiveRouteListenersLocked serves the same Router on the exact local
// endpoints still held by live clients. A corrupted listener metadata file can
// name a newer port while older processes retain their immutable launch URL.
// No fallback port or forwarding process is introduced. http.Server owns and
// closes every listener, including these, through the existing Close lifecycle.
func (o *Owner) restoreLiveRouteListenersLocked(endpoints []LiveRouteEndpoint) error {
	routes := map[string]bool{}
	for _, state := range o.table.Snapshot() {
		routes[state.Binding.RouteID] = true
	}
	seen := map[string]bool{o.addr: true}
	for _, endpoint := range endpoints {
		if !routes[endpoint.RouteID] || endpoint.RouteID == "" || seen[endpoint.Addr] {
			continue
		}
		if _, _, err := splitHostPortStrict(endpoint.Addr); err != nil {
			return fmt.Errorf("%w: live client address: %v", ErrListenerFailed, err)
		}
		ln, _, err := bindLoopbackListener(o.listenNetwork, endpoint.Addr, true)
		if err != nil {
			return err
		}
		seen[endpoint.Addr] = true
		srv := o.srv
		go func() { _ = srv.Serve(ln) }()
	}
	return nil
}
