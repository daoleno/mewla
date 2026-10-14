//go:build darwin

package macapp

import _ "embed"

// Icon is Mewla.icns, made from the app icon by scripts/generate-mac-icon.py.
//
//go:embed Mewla.icns
var Icon []byte
