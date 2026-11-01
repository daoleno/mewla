package server

import (
	"os"
	"testing"

	"github.com/daoleno/mewla/daemon/brain"
)

// TestMain makes every Brain executor command "installed", so these tests do
// not depend on which agent CLIs the machine running them has (CI has none).
func TestMain(m *testing.M) {
	restore := brain.SetHostLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })
	code := m.Run()
	restore()
	os.Exit(code)
}
