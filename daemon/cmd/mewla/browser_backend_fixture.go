//go:build linux && browserfixture

package main

import "github.com/daoleno/mewla/daemon/browser"

// Only a daemon built with -tags browserfixture uses the unencrypted
// credential store, and only for disposable test sites.
func init() { newBrowserBackend = browser.NewFixtureBackend }
