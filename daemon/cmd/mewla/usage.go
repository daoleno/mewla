package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/remote"
	"github.com/daoleno/mewla/daemon/usage"
)

// usageNoticeLine is printed once, the first time Mewla starts in a terminal.
const usageNoticeLine = "Mewla sends anonymous usage counts (never your content). Turn them off: mewla telemetry off"

// The first flush waits so a person who reads the notice can turn counts off
// before anything leaves.
const (
	usageFirstFlush = 10 * time.Minute
	usageFlushEvery = time.Hour
)

// usageSender is the running daemon's sender (nil in CLI processes).
var usageSender *usage.Sender

// startUsage prepares this computer's counts and sends them in the
// background. A person at a terminal sees the notice once.
func startUsage(ctx context.Context, authManager *auth.Manager, interactive bool, stderr io.Writer) {
	stateDir := authManager.StorageDir()
	if interactive && usage.OffReason(stateDir) == "" {
		if settings := usage.ReadSettings(stateDir); !settings.NoticeShown {
			fmt.Fprintln(stderr, usageNoticeLine)
			settings.NoticeShown = true
			_ = usage.WriteSettings(stateDir, settings)
		}
	}
	sender := &usage.Sender{
		StateDir: stateDir,
		Version:  Version,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Via:      usageVia(),
		First:    usageFirstFlush,
		Every:    usageFlushEvery,
		Daily: func() usage.Daily {
			_, signedIn, _ := remote.LoadRemote(stateDir)
			devices := 0
			for _, device := range authManager.ListDevices() {
				if device.Kind != auth.DeviceKindComputer {
					devices++
				}
			}
			return usage.Daily{
				OS: runtime.GOOS, Arch: runtime.GOARCH, Version: Version,
				SignedIn: signedIn, Devices: devices, Agents: agentsOnPath(),
				Features: usageFeatures(signedIn),
			}
		},
	}
	if os.Getenv(usage.EndpointEnv) != "" {
		// A proof run points counts at its own receiver and wants them now.
		sender.First, sender.Every = 15*time.Second, time.Minute
	}
	sender.Start()
	usageSender = sender
	go sender.Run(ctx)
}

// usageVia is how this binary was installed, from where it runs.
func usageVia() string {
	exe, err := os.Executable()
	if err != nil {
		return "other"
	}
	exe = filepath.ToSlash(exe)
	switch {
	case releaseBuild != "1":
		return "source"
	case strings.Contains(exe, ".app/Contents/"):
		return "mac_app"
	case strings.Contains(exe, "/.local/bin/"), strings.Contains(strings.ToLower(exe), "/appdata/local/"):
		return "script"
	default:
		return "other"
	}
}

// agentCommands are the agent CLIs Mewla runs, by kind.
var agentCommands = map[string][]string{
	"claude":      {"claude"},
	"codex":       {"codex"},
	"opencode":    {"opencode"},
	"gemini":      {"gemini"},
	"grok":        {"grok"},
	"cursor":      {"cursor-agent"},
	"pi":          {"pi"},
	"antigravity": {"agy", "antigravity"},
}

// agentsOnPath lists the agent kinds installed on this computer.
func agentsOnPath() []string {
	var kinds []string
	for kind, commands := range agentCommands {
		for _, command := range commands {
			if _, err := exec.LookPath(command); err == nil {
				kinds = append(kinds, kind)
				break
			}
		}
	}
	return kinds
}

func usageFeatures(signedIn bool) []string {
	var features []string
	if signedIn {
		features = append(features, "relay")
	}
	return features
}

// usageBrainTurn counts the first Brain reply this install ever got, and
// Brain turns that failed for good, by error class only.
func usageBrainTurn(host func() (brain.HostSession, error), e agentevents.Event) {
	if usageSender == nil || e.AgentID != "" {
		return
	}
	session, err := host()
	if err != nil || session.ID == "" || session.ID != e.WorkerID {
		return
	}
	agent := usageAgentKind(session.ExecutorID)
	switch e.Status {
	case agentevents.TurnCompleted:
		usageSender.FirstReply(agent)
	case agentevents.TurnFailed:
		if e.Error == nil || e.Error.WillRetry {
			return
		}
		recordUsage(usageSender.StateDir, "reply_failed", map[string]string{"class": usageReplyFailure(e.Error.Class), "agent": agent})
	}
}

// countSessionStarts counts each Session launch by agent kind.
func countSessionStarts(stateDir string, adapt func(executorID, command string) (agentevents.Launch, error)) func(executorID, command string) (agentevents.Launch, error) {
	return func(executorID, command string) (agentevents.Launch, error) {
		launch, err := adapt(executorID, command)
		if err == nil {
			recordUsage(stateDir, "session_started", map[string]string{"agent": usageAgentKind(executorID)})
		}
		return launch, err
	}
}

// usageAgentKind maps an executor id ("claude", "codex-mini") to its kind.
func usageAgentKind(executorID string) string {
	id := strings.ToLower(executorID)
	for _, kind := range usage.AgentKinds {
		if strings.HasPrefix(id, kind) {
			return kind
		}
	}
	if strings.HasPrefix(id, "agy") {
		return "antigravity"
	}
	return "other"
}

func usageReplyFailure(class agentevents.ErrorClass) string {
	switch class {
	case agentevents.ErrorAuth:
		return "provider_auth"
	case agentevents.ErrorBilling, agentevents.ErrorRateLimit:
		return "quota"
	case agentevents.ErrorOverloaded, agentevents.ErrorServer, agentevents.ErrorInvalidRequest, agentevents.ErrorContext:
		return "model"
	case agentevents.ErrorNetwork:
		return "timeout"
	default:
		return "other"
	}
}

// recordUsage records one event from any Mewla process on this computer.
func recordUsage(stateDir, name string, props map[string]string) {
	usage.Record(stateDir, name, props)
}

// runTelemetryCommand is `mewla telemetry on|off|status|show|reset`.
func runTelemetryCommand(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mewla telemetry", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stateDirFlag := flags.String("state-dir", "", "state directory")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla telemetry <status|on|off|show|reset> [-state-dir DIR]")
		fmt.Fprintln(stderr, "Anonymous usage counts: never content, prompts, paths, file names or addresses.")
		fmt.Fprintln(stderr, "MEWLA_TELEMETRY=0 or DO_NOT_TRACK=1 also turns them off. What is sent: docs/security-and-privacy.md#usage-counts")
	}
	command := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	stateDir, err := auth.ResolveStorageDir(*stateDirFlag)
	if err != nil {
		return err
	}
	switch command {
	case "status":
		fmt.Fprintln(stdout, usageStatusLine(stateDir))
	case "on":
		if err := usage.SetEnabled(stateDir, true); err != nil {
			return err
		}
		fmt.Fprintln(stdout, usageStatusLine(stateDir))
	case "off":
		if err := usage.SetEnabled(stateDir, false); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Usage counts are off for this computer and its phones. Nothing waiting was sent.")
	case "show":
		queued := usage.Queued(stateDir)
		if len(queued) == 0 {
			fmt.Fprintln(stdout, "Nothing waits to be sent.")
			return nil
		}
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(queued)
	case "reset":
		if _, err := usage.ResetID(stateDir); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "This computer has a new random install id.")
	default:
		flags.Usage()
		return errors.New("unknown telemetry command: " + command)
	}
	return nil
}

func usageStatusLine(stateDir string) string {
	switch usage.OffReason(stateDir) {
	case "":
		return "Usage counts are on (anonymous, never content). Turn off: mewla telemetry off"
	case usage.OffBySetting:
		return "Usage counts are off. Turn on: mewla telemetry on"
	case usage.OffByEnv:
		return "Usage counts are off by MEWLA_TELEMETRY or DO_NOT_TRACK in this environment."
	case usage.OffInCI:
		return "Usage counts are off: CI is set."
	default:
		return "Usage counts are off: this is a development build."
	}
}
