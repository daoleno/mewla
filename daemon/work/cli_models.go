package work

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/daoleno/mewla/daemon/procspawn"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// CLIModel is one model an agent's own `models` command lists.
type CLIModel struct {
	ID      string
	Name    string
	Default bool
	Current bool
}

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// ReadCLIModels runs the `models` command of the agent a launch command runs
// (Cursor Agent, Grok) and reads its list. A refusal is the agent's own words.
func ReadCLIModels(ctx context.Context, provider, command, cwd string) ([]CLIModel, error) {
	program := launchProgram(command)
	if program == "" {
		return nil, fmt.Errorf("no program in the Session's command")
	}
	cmd := procspawn.CommandContext(ctx, provider+" models", program, "models")
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var models []CLIModel
	switch provider {
	case WorkerProviderCursor:
		models = ParseCursorModels(stdout.String())
	case WorkerProviderGrok:
		// Signed out, Grok still lists its models; none of them can run.
		if strings.Contains(stdout.String()+stderr.String(), "You are not authenticated") {
			return nil, fmt.Errorf("%s: not signed in", AgentProductName(provider, command))
		}
		models = ParseGrokModels(stdout.String())
	}
	if len(models) > 0 {
		return models, nil
	}
	reason := strings.TrimSpace(ansiEscape.ReplaceAllString(stderr.String()+"\n"+stdout.String(), ""))
	if reason == "" && runErr != nil {
		reason = runErr.Error()
	}
	if reason == "" {
		reason = "it listed no models"
	}
	return nil, fmt.Errorf("%s: %s", AgentProductName(provider, command), modelsReasonLine(reason))
}

var cursorModelLine = regexp.MustCompile(`^(\S+)(?: - (.*?))?(?: \(((?:current|default)(?:, (?:current|default))?)\))?$`)

// ParseCursorModels reads `cursor-agent models`: after "Available models",
// one "<id> - <name> (current, default)" line per model, then a tip.
func ParseCursorModels(out string) []CLIModel {
	var models []CLIModel
	listing := false
	for _, raw := range strings.Split(ansiEscape.ReplaceAllString(out, ""), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "Available models":
			listing = true
			continue
		case !listing || line == "":
			continue
		case strings.HasPrefix(line, "Tip:"):
			return models
		}
		match := cursorModelLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		models = append(models, CLIModel{ID: match[1], Name: match[2], Current: strings.Contains(match[3], "current"), Default: strings.Contains(match[3], "default")})
	}
	return models
}

var grokModelLine = regexp.MustCompile(`^[*-]\s+(\S+)(?:\s+\((default)\))?$`)

// ParseGrokModels reads `grok models`: "Available models:" then one
// "* <id> (default)" or "- <id>" line per model.
func ParseGrokModels(out string) []CLIModel {
	var models []CLIModel
	listing := false
	for _, raw := range strings.Split(ansiEscape.ReplaceAllString(out, ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "Available models:" {
			listing = true
			continue
		}
		if !listing || line == "" {
			continue
		}
		if match := grokModelLine.FindStringSubmatch(line); match != nil {
			models = append(models, CLIModel{ID: match[1], Default: match[2] != ""})
		}
	}
	return models
}

// LaunchModelFlag reads --model (or -m) off a launch command.
func LaunchModelFlag(command string) string {
	fields, ok := shellquote.Fields(command)
	if !ok {
		fields = strings.Fields(command)
	}
	for i := 0; i < len(fields); i++ {
		key, value, inline := strings.Cut(fields[i], "=")
		if key != "--model" && key != "-m" {
			continue
		}
		if inline {
			return value
		}
		if i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func modelsReasonLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}
