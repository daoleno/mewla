package compatsweep

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestUpdateFromRelease: a computer runs a released Mewla
// (MEWLA_SWEEP_UPDATE_FROM, the binary from its release archive) with its
// executors.toml. `mewla update` installs the latest published release over
// it (MEWLA_SWEEP_UPDATE_TO is its version), the daemon is started again on
// it, Brain on claude replies in Chat, and a Worker
// Brain dispatches returns its result and is closed. It downloads from the
// real release feed and spends model calls; MEWLA_SWEEP_REAL must include
// claude.
func TestUpdateFromRelease(t *testing.T) {
	from, to := os.Getenv("MEWLA_SWEEP_UPDATE_FROM"), os.Getenv("MEWLA_SWEEP_UPDATE_TO")
	if from == "" || to == "" {
		t.Skip("set MEWLA_SWEEP_UPDATE_FROM to a released mewla binary and MEWLA_SWEEP_UPDATE_TO to the version it updates to")
	}
	realAgent(t, "claude")
	e := newRealEnv(t, realOptions{})
	installed := filepath.Join(e.home, ".local", "bin", "mewla")
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, installed, string(raw), 0o755)
	e.bin = installed
	executors := filepath.Join(e.state, "executors.toml")
	writeFile(t, executors, realExecutors, 0o600)
	step := func(name string, run func() (string, error)) bool {
		detail, err := run()
		if err != nil {
			record(t, name, "fail", err.Error()+"\n"+e.logTail(8))
			t.Errorf("%s: %v\n%s", name, err, e.logTail(30))
			return false
		}
		record(t, name, "pass", oneLine(detail))
		return true
	}

	e.start()
	fromVersion := e.healthVersion()
	if !step("update-from-release", func() (string, error) {
		cmd := exec.Command(installed, "update")
		cmd.Dir, cmd.Env = e.home, e.environ()
		out, _ := cmd.CombinedOutput()
		version, err := exec.Command(installed, "version").Output()
		if err != nil || !strings.Contains(string(version), to) {
			return "", fmt.Errorf("after mewla update the binary says %q (%v); update said: %s", strings.TrimSpace(string(version)), err, oneLine(string(out)))
		}
		return fmt.Sprintf("daemon %s; mewla update said: %s", fromVersion, oneLine(string(out))), nil
	}) {
		return
	}
	if !step("daemon-on-new-version", func() (string, error) {
		// A daemon started from a terminal is not restarted by mewla update;
		// it says so, and the person starts it again.
		e.stopServe()
		e.start()
		if got := e.healthVersion(); got != to {
			return "", fmt.Errorf("/health says %q, want %s", got, to)
		}
		return fmt.Sprintf("/health %s -> %s", fromVersion, to), nil
	}) {
		return
	}
	c := e.connect(e.pair())
	hostID, last := brainHost(c, "claude", 3*time.Minute)
	if !step("brain-claude-replies", func() (string, error) {
		if hostID == "" {
			return "", fmt.Errorf("no running claude Brain host; problem=%v", find(last, "host_problem"))
		}
		reply, err := e.askBrain(t, c, hostID, 19, 23)
		return "claude Brain replied: " + reply, err
	}) {
		return
	}
	step("worker-dispatched-and-closed", func() (string, error) {
		project := filepath.Join(e.home, "projects", "update app")
		if err := os.MkdirAll(project, 0o700); err != nil {
			return "", err
		}
		name := "UpdateW"
		done := "UPDATEBRAIN-" + uuid.NewString()[:6]
		ask := fmt.Sprintf("Compatibility check. Start exactly one Worker now with mewla worker spawn: name %q, executor claude, "+
			"working directory %q, profile quick, prompt \"Reply with the single word UPDATEW-OK and report done.\". "+
			"Do not do the task yourself. When its result arrives, close that Worker with mewla worker close, then reply to me with only %s.",
			name, project, done)
		if _, err := chat(c, hostID, ask); err != nil {
			return "", err
		}
		worker, spawned := e.waitWorker(t, name, 4*time.Minute, nil)
		reply, replied := e.waitReply(done, 8*time.Minute)
		_, stillOpen := e.waitWorker(t, name, 5*time.Second, nil)
		detail := fmt.Sprintf("spawned=%v (%s) brain reported back=%v closed=%v reply=%q", spawned, worker.ID, replied, !stillOpen, oneLine(reply))
		if !spawned || !replied || stillOpen {
			return "", fmt.Errorf("%s", detail)
		}
		return detail, nil
	})
}

// healthVersion is the version the daemon's /health reports.
func (e *env) healthVersion() string {
	response, err := http.Get(e.url() + "/health")
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var health struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &health)
	return health.Version
}
