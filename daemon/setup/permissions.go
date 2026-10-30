package setup

import (
	"strings"

	"github.com/daoleno/mewla/daemon/work"
)

// commandForProvider returns the executor command setup writes for provider.
// Every agent runs with full authority: Codex and Claude get theirs from the
// launch path (the Brain host and every Worker add the bypass flag), so their
// commands stay plain; the others carry their own flags.
func commandForProvider(provider, id, existingCommand string) (command, kind string) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = work.InferWorkerProvider(existingCommand, id)
	}
	existingCommand = strings.TrimSpace(existingCommand)

	switch provider {
	case work.WorkerProviderCodex:
		return "codex", ""
	case work.WorkerProviderClaude:
		return "claude", ""
	case work.WorkerProviderCursor:
		return "cursor-agent --force --sandbox disabled", "cursor"
	case work.WorkerProviderGrok:
		return "grok --no-alt-screen --permission-mode bypassPermissions", ""
	case work.WorkerProviderPi:
		// Pi has no approval gate to bypass.
		return "pi", ""
	case work.WorkerProviderOpenCode:
		return "opencode " + work.OpenCodeAutoFlag, ""
	default:
		// Preserve custom commands.
		if existingCommand != "" {
			return existingCommand, ""
		}
		if id != "" {
			return id, ""
		}
		return "custom", ""
	}
}
