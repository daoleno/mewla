package control

import (
	"path/filepath"
	"testing"
)

func TestDefaultSocketPathFollowsTheStateDirOfTheDaemonThatStartedTheSession(t *testing.T) {
	t.Setenv("MEWLA_STATE_DIR", "/tmp/isolated-mewla")
	got, err := DefaultSocketPath("")
	if err != nil || got != filepath.Join("/tmp/isolated-mewla", "run", SocketName) {
		t.Fatalf("got %q, %v", got, err)
	}
	// An explicit state directory still wins.
	got, _ = DefaultSocketPath("/tmp/other")
	if got != filepath.Join("/tmp/other", "run", SocketName) {
		t.Fatalf("explicit = %q", got)
	}
}
