package watcher

import (
	"strings"
	"time"
)

const piProjectTrustSessionKey = "__mewla_pi_trust_session__"

// advanceStartupTrustPromptOnce answers a startup trust picker that Mewla's own
// launch of a Worker could not answer on its command line: Pi's (session-only)
// and Cursor's. Codex's is not here. Delegated Codex launches carry their folder
// trust and show no screen, and a person's own Codex session is theirs to
// answer, not Mewla's.
func advanceStartupTrustPromptOnce(
	alreadyAdvanced bool,
	command string,
	content string,
	guard func() error,
	sendKey func(string) error,
) (bool, bool, bool) {
	if alreadyAdvanced {
		return true, false, true
	}
	key := ""
	switch {
	case isPiProjectTrustPrompt(command, content):
		key = piProjectTrustSessionKey
	case isCursorWorkspaceTrustPrompt(command, content):
		key = "a"
	default:
		return false, false, true
	}
	if guard != nil && guard() != nil {
		return false, false, false
	}
	if sendKey == nil || sendKey(key) != nil {
		return false, false, false
	}
	return true, true, true
}

// isPiProjectTrustPrompt recognizes Pi's startup project trust picker. The
// full option set and action footer are required so arbitrary pane text or a
// stale transcript cannot cause an input key sequence to be sent. A later Pi
// header/chrome means the picker has already been consumed and must not be
// revisited from scrollback.
func isPiProjectTrustPrompt(command, content string) bool {
	if !isPiCommand(command) {
		return false
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	titleMatches := piProjectTrustTitleRe.FindAllStringIndex(normalized, -1)
	if len(titleMatches) == 0 {
		return false
	}
	start := titleMatches[len(titleMatches)-1][0]
	current := normalized[start:]
	lower := strings.ToLower(current)
	for _, required := range []string{
		"this allows pi to load .pi settings and resources",
		"trust parent folder (",
		"trust (this session only)",
		"do not trust (this session only)",
		"navigate  enter select",
		"escape/ctrl+c cancel",
	} {
		if !strings.Contains(lower, required) {
			return false
		}
	}
	return !piVersionRe.MatchString(current) && !piChromeRe.MatchString(current)
}

func isWorkerInputReady(command, content string) bool {
	if workerCommandName(command) == "dsh" {
		return strings.Contains(content, "DSH ready.")
	}
	if !needsInputReadinessWait(command, content) {
		return true
	}
	explicitCodex := isCodexCommand(command)
	if explicitCodex {
		return isCodexStartupReady(content)
	}
	if isCursorAgentCommand(command) || strings.Contains(strings.ToLower(content), "cursor agent") {
		current := latestCursorPaneContent(content)
		return strings.Contains(strings.ToLower(current), "cursor agent") &&
			cursorInputReadyRe.MatchString(current)
	}
	if isClaudeCommand(command) {
		return isClaudeInputReady(content)
	}
	if isGrokCommand(command) || looksLikeGrokPane(content) {
		return isGrokInputReady(content)
	}
	if isPiCommand(command) {
		return isPiInputReady(content)
	}
	if isOpenCodeCommand(command) || looksLikeOpenCodePane(content) {
		return isOpenCodeInputReady(content)
	}
	return strings.TrimSpace(content) != ""
}

func isCodexStartupReady(content string) bool {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) > 80 {
		normalized = strings.Join(lines[len(lines)-80:], "\n")
	}
	lower := strings.ToLower(normalized)
	lastHeader := strings.LastIndex(lower, "openai codex")
	if lastHeader < 0 {
		lastHeader = strings.LastIndex(lower, ">_ codex")
	}
	lastFooter := -1
	if match := codexFooterReadyRe.FindAllStringIndex(normalized, -1); len(match) > 0 {
		lastFooter = match[len(match)-1][0]
	}
	if lastHeader < 0 && lastFooter < 0 {
		return false
	}
	composerIndexes := codexComposerReadyRe.FindAllStringIndex(normalized, -1)
	if len(composerIndexes) == 0 {
		return false
	}
	lastComposer := composerIndexes[len(composerIndexes)-1][0]
	epoch := lastHeader
	if epoch < 0 {
		epoch = lastFooter
		if lastComposer < epoch {
			epoch = lastComposer
		}
	}
	lower = strings.ToLower(normalized)
	for _, blocked := range []string{
		"update available",
		"update now",
		"skip until next version",
		"select a model",
		"choose a model",
		"loading model",
		"starting codex",
		"trust this folder",
		"do you trust the contents",
		"press enter to continue",
	} {
		if blockedAt := strings.LastIndex(lower, blocked); blockedAt >= epoch {
			return false
		}
	}
	return lastComposer >= lastHeader
}

func isGrokInputReady(content string) bool {
	current := latestGrokPaneContent(content)
	if strings.TrimSpace(current) == "" {
		return false
	}
	return grokChromeReadyRe.MatchString(current) && grokPromptReadyRe.MatchString(current)
}

func looksLikeGrokPane(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "grok") &&
		(strings.Contains(lower, "always-approve") ||
			strings.Contains(lower, "xai") ||
			strings.Contains(lower, "enter:send") ||
			strings.Contains(lower, "shift+tab:mode") ||
			grokChromeReadyRe.MatchString(content))
}

func latestGrokPaneContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	if idx := strings.LastIndex(lower, "grok"); idx >= 0 {
		// Prefer the latest chrome block; keep preceding context so the composer
		// prompt above the Grok footer is still visible for readiness checks.
		start := idx - 400
		if start < 0 {
			start = 0
		}
		return normalized[start:]
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 60 {
		return strings.Join(lines[len(lines)-60:], "\n")
	}
	return normalized
}

func isCursorWorkspaceTrustPrompt(command, content string) bool {
	if !isCursorAgentCommand(command) && !strings.Contains(strings.ToLower(content), "cursor agent") {
		return false
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	return cursorWorkspaceTrustRe.MatchString(normalized) &&
		strings.Contains(lower, "trust this workspace")
}

func needsInputReadinessWait(command, content string) bool {
	if workerCommandName(command) == "dsh" {
		return true
	}
	lowerContent := strings.ToLower(content)
	return isCodexCommand(command) ||
		isCursorAgentCommand(command) ||
		isClaudeCommand(command) ||
		isGrokCommand(command) ||
		isPiCommand(command) ||
		isOpenCodeCommand(command) ||
		strings.Contains(lowerContent, "openai codex") ||
		strings.Contains(lowerContent, "cursor agent") ||
		looksLikeGrokPane(content) ||
		looksLikeOpenCodePane(content)
}

func isPiInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if !piVersionRe.MatchString(content) && !piChromeRe.MatchString(content) {
		return false
	}
	if !piFooterRe.MatchString(content) {
		return false
	}
	// Evaluate overlays only in the latest Pi UI epoch. Older transcript and
	// startup scrollback may contain these words after the provider is idle.
	if headers := piVersionRe.FindAllStringIndex(content, -1); len(headers) > 0 {
		current := content[headers[len(headers)-1][0]:]
		if piBlockedOverlayRe.MatchString(current) {
			return false
		}
	}
	borders := piEditorBorderRe.FindAllStringIndex(content, -1)
	if len(borders) < 2 {
		return false
	}
	// Empty editor: two horizontal rules with only blank/whitespace between them.
	between := content[borders[len(borders)-2][1]:borders[len(borders)-1][0]]
	if strings.TrimSpace(between) != "" {
		return false
	}
	return true
}

func isOpenCodeInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	if openCodeBlockedOverlayRe.MatchString(content) {
		return false
	}
	return (openCodeComposerPlaceholderRe.MatchString(content) || openCodeComposerEmpty(content)) &&
		openCodeAgentLineRe.MatchString(content) &&
		(openCodeVersionFooterRe.MatchString(content) || openCodeIdleFooterRe.MatchString(content))
}

// openCodeComposerEmpty reports OpenCode 2's composer box with no draft. The
// "Ask anything…" placeholder appears only in an empty session; a session
// opened with --session shows blank "┃" rows above the agent line instead.
func openCodeComposerEmpty(content string) bool {
	lines := strings.Split(content, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(line, "┃") || !openCodeAgentLineRe.MatchString(line) {
			continue
		}
		blank := 0
		for above := index - 1; above >= 0; above-- {
			row := strings.TrimSpace(lines[above])
			if !strings.HasPrefix(row, "┃") {
				break
			}
			if strings.TrimSpace(strings.TrimPrefix(row, "┃")) != "" {
				return false
			}
			blank++
		}
		return blank > 0
	}
	return false
}

func looksLikeOpenCodePane(content string) bool {
	lower := strings.ToLower(content)
	return openCodeComposerPlaceholderRe.MatchString(content) ||
		(strings.Contains(lower, "tab agents") && strings.Contains(lower, "ctrl+p commands"))
}

func isClaudeInputReady(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	// Ready evidence is the pair of an empty composer line and the
	// permission/mode footer below it. Composer: prompt glyph with only
	// spaces/tabs/NBSP (plus the fixed suggestion placeholder). Footer: mode
	// indication (auto mode, bypass permissions or manual mode).
	//
	// The "Claude Code vX.Y.Z" banner is not required: `claude --resume`
	// replays the transcript, which scrolls the banner out of the pane while
	// the TUI is fully usable (captured live 2026-10-24 on a resumed 432 MB
	// transcript: transcript, empty composer and auto-mode footer, no header).
	composers := claudeComposerRe.FindAllStringIndex(content, -1)
	footers := claudeModeFooterRe.FindAllStringIndex(content, -1)
	if len(composers) == 0 || len(footers) == 0 {
		return false
	}
	composer := composers[len(composers)-1]
	if footers[len(footers)-1][0] < composer[1] {
		return false
	}
	// Evaluate overlays in the current UI epoch. Claude keeps startup text in
	// scrollback after the TUI becomes usable; treating words such as
	// "loading" from that old epoch as live state causes a ready Worker to
	// time out and the shared handoff to report input-not-ready. A header, when
	// the pane still shows one, bounds the epoch earlier; otherwise the last
	// composer is the anchor.
	start := composer[0]
	if headers := claudeHeaderRe.FindAllStringIndex(content, -1); len(headers) > 0 {
		start = max(headers[len(headers)-1][0], composer[0]-96)
	}
	return !claudeBlockedOverlayRe.MatchString(content[start:])
}

func tmuxSubmitDelay(command string) time.Duration {
	if isCodexCommand(command) {
		// Codex turns large bracketed pastes into an asynchronous composer
		// attachment. Enter before the attachment is committed is ignored and
		// leaves a visible "[Pasted Content ...]" draft without a provider turn.
		return 2 * time.Second
	}
	if isCursorAgentCommand(command) {
		// Large pastes become a composer attachment asynchronously. Enter sent
		// before that attachment is ready is ignored and leaves bytes sitting
		// in the composer.
		return 2 * time.Second
	}
	if isGrokCommand(command) {
		// Large spawn briefs need a settle window before Enter or Grok keeps the draft unsent.
		return 300 * time.Millisecond
	}
	if isClaudeCommand(command) {
		// Claude needs settle time for initial brief to be fully pasted before submit.
		return 250 * time.Millisecond
	}
	return 120 * time.Millisecond
}

func tmuxPrepareDelay(command string) time.Duration {
	if isCursorAgentCommand(command) {
		// Cursor applies composer edits asynchronously. Give its clear action
		// one render boundary before atomically pasting the new payload.
		return 400 * time.Millisecond
	}
	return 0
}

func inputReadyTimeout(command string) time.Duration {
	if isCodexCommand(command) {
		return codexInputStartupStallTimeout
	}
	if isCursorAgentCommand(command) {
		return cursorInputReadyTimeout
	}
	if isClaudeCommand(command) {
		return claudeInputReadyTimeout
	}
	if isGrokCommand(command) {
		return grokInputReadyTimeout
	}
	if isPiCommand(command) {
		return piInputReadyTimeout
	}
	if isOpenCodeCommand(command) {
		return openCodeInputReadyTimeout
	}
	return initialInputReadyTimeout
}
