//go:build linux || darwin

package compatsweep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAgentInstallStyles puts the agent where each installer puts it, while
// the daemon runs with a service's PATH (a systemd user unit, a desktop entry,
// launchd): /usr/bin:/bin only. The person's shell finds the agent through its
// startup files; Mewla must too, for Brain and for Workers.
func TestAgentInstallStyles(t *testing.T) {
	type style struct {
		name string
		// dir is where the installer puts the agent, under HOME.
		dir string
		// rc is the startup file line the installer adds ("" adds none).
		rc string
	}
	styles := []style{
		{name: "native-local-bin", dir: ".local/bin"},
		{name: "npm-global-prefix", dir: ".npm-global/bin", rc: `export PATH="$HOME/.npm-global/bin:$PATH"`},
		{name: "nvm", dir: ".nvm/versions/node/v22.11.0/bin", rc: `export NVM_DIR="$HOME/.nvm"; export PATH="$NVM_DIR/versions/node/v22.11.0/bin:$PATH"`},
		{name: "fnm", dir: ".local/share/fnm/node-versions/v22.11.0/installation/bin", rc: `export PATH="$HOME/.local/share/fnm/node-versions/v22.11.0/installation/bin:$PATH"`},
		{name: "volta", dir: ".volta/bin", rc: `export VOLTA_HOME="$HOME/.volta"; export PATH="$VOLTA_HOME/bin:$PATH"`},
		{name: "bun-global", dir: ".bun/bin", rc: `export BUN_INSTALL="$HOME/.bun"; export PATH="$BUN_INSTALL/bin:$PATH"`},
		{name: "login-shell-only", dir: "tools/agents/bin", rc: `export PATH="$HOME/tools/agents/bin:$PATH"`},
	}
	for _, s := range styles {
		t.Run(s.name, func(t *testing.T) {
			e := newEnv(t, envOptions{
				executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
				hostExecutor: "claude",
				env:          []string{"SHELL=/bin/bash"},
			})
			out := filepath.Join(e.dir, "launches")
			if err := os.MkdirAll(out, 0o700); err != nil {
				t.Fatal(err)
			}
			recordingAgent(t, filepath.Join(e.home, s.dir, "claude"), out)
			if s.rc != "" {
				for _, rc := range []string{".profile", ".bashrc", ".zshrc"} {
					writeFile(t, filepath.Join(e.home, rc), s.rc+"\n", 0o600)
				}
			}
			e.start()
			// Brain's host starts on it.
			brain := waitBrainLaunch(out, 60*time.Second)
			// And a Worker spawns on it.
			worker := filepath.Join(e.home, "project")
			_ = os.MkdirAll(worker, 0o700)
			response, err := e.cli(60*time.Second, "worker", "spawn", "@state", "-name", "Sweep install", "-executor", "claude",
				"-cwd", worker, "-prompt", "install check", "-profile", "quick")
			_, workerLaunched := waitLaunch(out, 30*time.Second)
			ok := brain && err == nil && workerLaunched
			detail := "Brain host and a Worker started on " + s.dir + "/claude"
			if !ok {
				problem := ""
				if line, found := e.waitLog(`brain host problem: `, time.Second); found {
					problem = line
				}
				detail = "brain launched=" + boolWord(brain) + "; worker spawn=" + oneLine(response) + "; " + problem
			}
			record(t, "install-claude-"+s.name, map[bool]string{true: "pass", false: "fail"}[ok], detail)
			if !ok {
				t.Fatal(detail)
			}
		})
	}
}

// waitBrainLaunch waits for the recording agent to be started as Brain's host.
func waitBrainLaunch(out string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, got := range launches(out) {
			if strings.Contains(strings.Join(got.Argv, " "), "--add-dir") {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func boolWord(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// TestAgentMissingOrOld: no agent at all, and one too old to run. Both are
// said plainly to the client (host problem) and to the spawning caller.
func TestAgentMissingOrOld(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		e := newEnv(t, envOptions{
			executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
			hostExecutor: "claude",
		})
		e.start()
		c := e.connect(e.pair())
		problem, ok := hostProblem(c, 60*time.Second, func(p map[string]any) bool { return str(p["code"]) == "not_installed" })
		worker := filepath.Join(e.home, "project")
		_ = os.MkdirAll(worker, 0o700)
		response, err := e.cli(60*time.Second, "worker", "spawn", "@state", "-name", "Sweep missing", "-executor", "claude",
			"-cwd", worker, "-prompt", "x", "-profile", "quick")
		pass := ok && err != nil && strings.Contains(response, "claude")
		record(t, "install-claude-missing", map[bool]string{true: "pass", false: "fail"}[pass],
			"host problem="+str(problem["detail"])+"; spawn="+oneLine(response))
		if !pass {
			t.Fatalf("problem=%v spawn err=%v %s", problem, err, response)
		}
	})
	t.Run("old-version", func(t *testing.T) {
		// An old release that predates an option Mewla passes refuses it and
		// exits: the client sees that, in its words.
		e := newEnv(t, envOptions{
			executors:    "[[executors]]\nname = \"claude\"\ncommand = \"claude\"\n",
			hostExecutor: "claude",
		})
		writeFile(t, filepath.Join(e.tools, "claude"), "#!/bin/sh\n[ \"$1\" = --version ] && { echo '0.2.9 (Claude Code)'; exit 0; }\n"+
			"for a in \"$@\"; do [ \"$a\" = --settings ] && { echo \"error: unknown option '--settings'\" >&2; exit 1; }; done\nexec sleep 600\n", 0o755)
		e.start()
		c := e.connect(e.pair())
		problem, ok := hostProblem(c, 90*time.Second, func(p map[string]any) bool {
			return str(p["code"]) == "start_failed" && strings.Contains(str(p["detail"]), "unknown option '--settings'")
		})
		detail := "host problem=" + str(problem["detail"])
		if !ok {
			detail = "no start_failed problem naming the refused option:\n" + e.logTail(10)
		}
		record(t, "install-claude-old-version", map[bool]string{true: "pass", false: "fail"}[ok], detail)
		if !ok {
			t.Fatal(detail)
		}
	})
}
