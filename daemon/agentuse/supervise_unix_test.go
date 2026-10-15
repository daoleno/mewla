//go:build unix

package agentuse

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Claude kills its MCP servers half a second after asking them to stop, and
// a killed Worker's terminal takes its host along: the tabs still close.
func TestAKilledRelayStillClosesTheTabs(t *testing.T) {
	argv, env, log := fakePlaywright(t)
	exe, _ := os.Executable()
	relay := exec.Command(exe, "-test.run=^TestRelayHelper$")
	relay.Env = append(env, "MEWLA_RELAY_ROLE=relay", "MEWLA_RELAY_FAKE="+strings.Join(argv, " "))
	relay.Stderr = os.Stderr
	relay.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	in, _ := relay.StdinPipe()
	out, _ := relay.StdoutPipe()
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	s := &supervised{in: in, out: bufio.NewScanner(out), log: log}
	s.call(t, 1, "browser_navigate")
	s.call(t, 2, "browser_navigate")
	// The whole group, as a host's kill or a terminal's hang-up reaches it.
	_ = syscall.Kill(-relay.Process.Pid, syscall.SIGKILL)
	_ = relay.Wait()
	waitFor := time.Now().Add(10 * time.Second)
	for {
		got, _ := os.ReadFile(log)
		if string(got) == "tabs=0 closes=2" {
			return
		}
		if time.Now().After(waitFor) {
			t.Fatalf("Playwright saw %q", got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
