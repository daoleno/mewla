package classifier

import (
	"regexp"
	"strings"
)

var (
	codexChromeRe    = regexp.MustCompile(`(?i)\bopenai\s+codex\b`)
	codexWorkingRe   = regexp.MustCompile(`(?im)\bworking\b`)
	codexInterruptRe = regexp.MustCompile(`(?i)esc\s+to\s+interrupt`)
	// Interactive approval phrasing only — exclude static mode chrome like "always-approve".
	codexApprovalPromptRe = regexp.MustCompile(`(?i)(trust this folder|enter continue.*esc quit|press enter to (continue|confirm)|would you like to run|waiting for approval|do you want to|please\s+approve|approve\s+or\s+reject|allow .+ to)`)
)

// CodexActivityAdapter detects only visible approval and Working/interrupt pane
// evidence. Canonical provider transcripts remain owned by daemon/work.
type CodexActivityAdapter struct{}

func NewCodexActivityAdapter() *CodexActivityAdapter {
	return &CodexActivityAdapter{}
}

func (a *CodexActivityAdapter) Name() string { return "codex" }

func (a *CodexActivityAdapter) Match(in ActivityInput) bool {
	base := commandBaseName(in.Worker.Command)
	if base == "codex" || strings.Contains(base, "codex") {
		return true
	}
	return codexChromeRe.MatchString(in.PaneContent)
}

func (a *CodexActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	pane := latestCodexPaneWindow(in.PaneContent)
	approvalPane := strings.Join(linesAfterLastCodexComposer(strings.Split(pane, "\n")), "\n")
	// A dialog proves itself by structure: heading sentence, numbered menu and
	// its footer at the bottom of the capture. That holds whatever a banner-like
	// or composer-like line inside its command did to the window above.
	whole := strings.ReplaceAll(in.PaneContent, "\r\n", "\n")
	if codexApprovalPromptRe.MatchString(approvalPane) || CodexApprovalDialogOpen(whole) {
		signal := ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Codex approval",
			Source:   "codex_pane_blocked",
			Provider: a.Name(),
		}
		// The identity rests on the whole capture, not on the window or the
		// text below the composer: a line in the command can look like either.
		// An answerable id needs the dialog's first lines to be provably in
		// the capture: the pane's origin marker above its heading says so.
		if in.Capture != nil && in.Capture.Exact {
			if prompt, ok := ParseCodexApprovalPrompt(whole, in.Capture.Origin); ok {
				prompt.Title = signal.Summary
				signal.Prompt = &prompt
			}
		}
		return signal
	}

	if codexWorkingRe.MatchString(pane) && codexInterruptRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateRunning,
			Summary:  "Codex working",
			Source:   "codex_pane_working",
			Provider: a.Name(),
		}
	}

	return ActivitySignal{State: StateUnknown, Source: "codex_idle", Provider: a.Name()}
}

// isCodexComposerLine reports whether line is Codex's input box rather than a
// menu row. The composer paints "›" and then either nothing, its placeholder
// ("Ask Codex to do anything", "Write tests for @filename") or the person's
// unsent draft; an approval, trust or picker menu paints "›" before a numbered
// option ("› 1. Yes, proceed (y)"). A modal replaces the composer, so a visible
// composer means nothing above it is waiting for an answer.
func isCodexComposerLine(line string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "›")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest == "" || rest[0] < '0' || rest[0] > '9'
}

// linesAfterLastCodexComposer keeps only what Codex painted below its last
// composer: everything above is scrollback (tool output, quoted help text),
// which can contain approval wording without anything being asked.
func linesAfterLastCodexComposer(lines []string) []string {
	last := -1
	for index, line := range lines {
		if isCodexComposerLine(line) {
			last = index
		}
	}
	if last < 0 {
		return lines
	}
	return lines[last+1:]
}

func isCodexPane(command string, lines []string) bool {
	if strings.Contains(commandBaseName(command), "codex") {
		return true
	}
	for _, line := range lines {
		if codexChromeRe.MatchString(line) {
			return true
		}
	}
	return false
}

// codexBannerRe is Codex's own banner line: its name alone, with a version or
// in the welcome box ("│ >_ OpenAI Codex (v0.46.0) │"). A mention inside a
// command or tool output is not one, and taking it for one would start the pane
// window inside an open dialog.
var codexBannerRe = regexp.MustCompile(`(?im)^[\s│|╭]*(?:>_\s*)?openai codex\b(?:\s*\(v?[^)\n]*\))?[\s│|╮]*$`)

// latestCodexPaneWindow is the pane from Codex's latest banner on, or its last
// 80 lines when the banner has scrolled out.
func latestCodexPaneWindow(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if matches := codexBannerRe.FindAllStringIndex(normalized, -1); len(matches) > 0 {
		return normalized[matches[len(matches)-1][0]:]
	}
	return latestProviderPaneWindow(normalized, "")
}

func latestProviderPaneWindow(content, marker string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	marker = strings.ToLower(strings.TrimSpace(marker))
	if marker != "" {
		if idx := strings.LastIndex(lower, marker); idx >= 0 {
			return normalized[idx:]
		}
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 80 {
		return strings.Join(lines[len(lines)-80:], "\n")
	}
	return normalized
}
