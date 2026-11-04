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
	pane := latestProviderPaneWindow(in.PaneContent, "openai codex")
	approvalPane := codexApprovalPane(pane)
	wholeApprovalPane := codexApprovalPane(strings.ReplaceAll(in.PaneContent, "\r\n", "\n"))
	if codexApprovalPromptRe.MatchString(approvalPane) {
		signal := ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Codex approval",
			Source:   "codex_pane_blocked",
			Provider: a.Name(),
		}
		if prompt, ok := ParseMenuPromptIn(a.Name(), PromptApproval, approvalPane, wholeApprovalPane, "Press enter to confirm", "enter to confirm", "Enter to continue"); ok {
			prompt.Title = signal.Summary
			signal.Prompt = &prompt
		} else if prompt, ok := ParseMenuPromptIn(a.Name(), PromptApproval, approvalPane, wholeApprovalPane); ok {
			prompt.Title = signal.Summary
			signal.Prompt = &prompt
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

// codexStatusLineRe is the whole line Codex paints for a running turn, with
// its clock: "• Working (32s • esc to interrupt)".
var codexStatusLineRe = regexp.MustCompile(`(?i)^\s*[•·]?\s*Working\s*\(\s*(?:\d+\s*h\s*)?(?:\d+\s*m\s*)?\d+(?:\.\d+)?\s*[hms]\s*[•·]\s*esc\s+to\s+interrupt\s*\)\s*$`)

// codexApprovalPane is what Codex painted below its last composer, without
// the turn's status line: its clock counts up while a command waits for
// approval, and it must not become part of the prompt's identity. Only the
// status line ahead of the dialog's command (the first "$ " line) is dropped; a
// line of that shape inside the command is the command's.
func codexApprovalPane(pane string) string {
	lines := linesAfterLastCodexComposer(strings.Split(pane, "\n"))
	command := len(lines)
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "$ ") {
			command = index
			break
		}
	}
	kept := make([]string, 0, len(lines))
	for index, line := range lines {
		if index < command && codexStatusLineRe.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
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
