// Package releasesmoke runs a built release binary the way a user's computer
// would: its version, the session host with the terminal engine, a shell and a
// full-screen program in host Sessions, mewla attach in a real terminal, and
// the daemon answering /health. It tests the artifact, not this package's
// build, so the release workflow runs it on each platform's own runner.
//
//	MEWLA_RELEASE_SMOKE_BINARY=dist/mewla-darwin-arm64 MEWLA_RELEASE_SMOKE_VERSION=0.3.0 \
//	  go test -count=1 -v ./releasesmoke/
//
// Without MEWLA_RELEASE_SMOKE_BINARY the test skips.
package releasesmoke
