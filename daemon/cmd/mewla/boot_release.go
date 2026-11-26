package main

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/daoleno/mewla/daemon/modelprofiles"
)

// bootReleaseAgentConfigs gives Codex and Claude Code their own settings back
// when Mewla is uninstalled: Codex's model_provider and Claude Code's env as
// they were before Mewla's gateway, so neither is left pointing at a gateway
// that no longer runs. Values another tool or the person set since stay.
//
// Only an uninstall does this. A daemon that stops or restarts keeps the
// projection (it comes back on the same address); choosing Official login in
// Model Providers gives the settings back while Mewla runs. A state directory
// still owned by another daemon is left to that daemon.
func bootReleaseAgentConfigs(stateDir string, out io.Writer) {
	if stateDir == "" {
		return
	}
	gatewayDir := filepath.Join(stateDir, "codex-gateway")
	if _, err := os.Stat(gatewayDir); err != nil {
		return
	}
	codexConfig, claudeSettings := machineConfigPaths()
	if codexConfig != "" {
		if _, err := modelprofiles.NewTakeover(codexConfig, gatewayDir, nil).Disable(); err != nil {
			fmt.Fprintf(out, "Note: Codex's settings (%s) were not given back: %v\n", codexConfig, err)
		}
	}
	if claudeSettings != "" {
		if err := modelprofiles.NewClaudeTakeover(claudeSettings, gatewayDir).Release(); err != nil {
			fmt.Fprintf(out, "Note: Claude Code's settings (%s) were not given back: %v\n", claudeSettings, err)
		}
	}
}

var bootTaskStateDirArg = regexp.MustCompile(`-state-dir\s+(?:"([^"]*)"|(\S+))`)

// bootTaskStateDir reads the state directory from the task's daemon arguments.
func bootTaskStateDir(taskXML string) string {
	m := bootTaskStateDirArg.FindStringSubmatch(html.UnescapeString(taskXML))
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}
