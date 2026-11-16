package telegram

import (
	"os"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/brain"
)

// TestMain makes every Brain executor command "installed", so these tests do
// not depend on which agent CLIs the machine running them has (CI has none),
// and spaces outbound messages 20 ms apart instead of Telegram's second.
func TestMain(m *testing.M) {
	outboundGap = 20 * time.Millisecond
	restore := brain.SetHostLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })
	code := m.Run()
	restore()
	os.Exit(code)
}
