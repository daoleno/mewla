package computers

import (
	"os"
	"strings"
)

// FriendlyName is what people call this computer: on a Mac the name set in
// System Settings → General → About, elsewhere the pretty hostname, and
// otherwise the short hostname. The app shows it instead of an address.
func FriendlyName() string {
	if name := strings.TrimSpace(platformName()); name != "" {
		return name
	}
	hostname, _ := os.Hostname()
	return ShortHostname(hostname)
}

// prettyHostname reads PRETTY_HOSTNAME from a machine-info file, which is
// shell-style KEY=VALUE lines with optional quotes.
func prettyHostname(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "PRETTY_HOSTNAME" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			quote := value[0]
			value = value[1 : len(value)-1]
			if quote == '"' {
				value = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(value)
			}
		}
		return strings.TrimSpace(value)
	}
	return ""
}
