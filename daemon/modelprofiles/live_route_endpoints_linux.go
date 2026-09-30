//go:build linux

package modelprofiles

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LiveClaudeRouteEndpoints reads structured argv without a shell or provider
// invocation. Unreadable/vanished processes confer no listener authority.
func LiveClaudeRouteEndpoints() ([]LiveRouteEndpoint, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var endpoints []LiveRouteEndpoint
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil || !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if endpoint, ok := claudeRouteEndpoint(strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")); ok {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints, nil
}
