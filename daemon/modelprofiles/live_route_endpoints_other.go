//go:build !linux

package modelprofiles

// Non-Linux hosts continue to restore the durable sticky listener. Recovery
// from corrupted metadata needs a platform-native structured argv reader.
func LiveClaudeRouteEndpoints() ([]LiveRouteEndpoint, error) { return nil, nil }
