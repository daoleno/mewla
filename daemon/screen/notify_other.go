//go:build !darwin

package screen

import "log"

// Notify records the event in the daemon log; only macOS has a banner.
func Notify(title, body string) { log.Printf("%s: %s", title, body) }
