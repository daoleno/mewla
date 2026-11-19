package brain

import (
	"testing"

	"github.com/daoleno/mewla/daemon/work"
)

// newTestService is NewService for a test. A fact or a lane pass leaves
// writers running in the background; the test's cleanup waits for them, so
// none writes into the store's directory while its TempDir is removed
// (cleanups run last first, and the store's TempDir came before the Service).
func newTestService(t testing.TB, store *Store, watcher Watcher, execs *work.ExecutorConfig) *Service {
	service := NewService(store, watcher, execs)
	t.Cleanup(service.waitBackground)
	return service
}
