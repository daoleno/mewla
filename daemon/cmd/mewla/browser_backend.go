package main

import "github.com/daoleno/mewla/daemon/browser"

// newBrowserBackend is the product backend; a build tagged browserfixture
// swaps in the disposable one (see browser_backend_fixture.go).
var newBrowserBackend = browser.NewBackend
