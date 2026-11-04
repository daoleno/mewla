package classifier

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Identity of a prompt that asks to run something.
//
// An answer is composed for a prompt id and sent only if the pane still shows
// that id, so the id has to tell any two requests apart. For Claude permission
// and Codex approval dialogs it is a hash of the dialog's text as displayed:
// every character of the lines from the dialog's first line down to its menu,
// with only positively identified provider UI removed (Claude's terminal
// countdown notice, the cursor caret and a trailing clock on a menu row).
//
// What the screen can and cannot prove, and what is done about it:
//   - It is the displayed text, not the program's bytes. A tab, or a line the
//     program trimmed or re-wrapped, cannot be told from the spaces it drew.
//     tmux joins lines the terminal wrapped (capture-pane -J), so a soft wrap
//     is not mistaken for a newline.
//   - Blanks at the end of a line are not evidence. tmux reports a cell that
//     was written with a space and a cell that an earlier, longer line left
//     erased alike (measured with tmux 3.6: a line overwritten in place and
//     erased to its end still reports the old extent as trailing spaces). They
//     are therefore left out of the id, and a dialog in which any line that is
//     followed by more request text ends in blanks is refused: inside a quoted
//     string or a heredoc those blanks can be the command's, and the screen
//     cannot say. Only the last line of a request may end in blanks.
//   - Whether the capture holds the whole dialog is a property of the capture,
//     not of the text. A heading-looking line proves nothing: a command can
//     contain one, and a clipped capture can begin at it. How full the pane's
//     scrollback is proves nothing either: tmux trims history in batches, so a
//     pane below its limit can have lost lines (measured: limit 20, 30 lines
//     printed, 19 held, first line gone). Two things do prove it. Claude's
//     dialog box begins at a rule line at the margin, which a command's own
//     lines (drawn indented) do not produce. Codex's dialog has no such line,
//     so a Codex id needs the pane's origin marker (CaptureInfo.Origin) as the
//     capture's first line: tmux evicts from the top, so the pane's first
//     output still being the first line means nothing was evicted. A copy of
//     the marker elsewhere (in a command) is not looked at; forging it would
//     take the pane's private nonce and an eviction that leaves that copy as
//     the first line.
//   - Blank lines inside the request cannot be counted exactly either: whether
//     a renderer paints a blank line's cells or leaves them default, an empty
//     line and a line of spaces look alike. Inside the request (after the
//     command's first line, up to its last non-blank line) any blank line
//     refuses the dialog; the provider's own separators are outside that
//     region by position.
//   - A capture not taken with -J (CaptureInfo.Exact) never yields an id.
//
// Whenever one of these cannot be established the dialog is still reported as
// blocked, with no prompt and so no id to answer.
//
//	provider  dialog starts at                       answerable id only when
//	claude    the solid rule line above the box       Exact capture, rule found, request has no blank line or trailing blanks
//	codex     the topmost heading below the marker    Exact capture, origin marker above the heading, request has no blank line or trailing blanks
//
// Known collision, not detectable from cells: a renderer that never paints
// trailing blanks shows 'alpha ' and 'alpha' alike, so those two commands share
// an id. Only blanks that are painted can be refused.
//
// The presentation (Request, Title) is cut and tidied separately; it never
// feeds the id.

// PromptIdentity hashes parts exactly, each with its length, so neither
// whitespace nor the split between parts can make two requests agree.
func PromptIdentity(kind PromptKind, parts ...string) string {
	hash := sha256.New()
	hash.Write([]byte("exact\x00" + string(kind)))
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(strconv.Itoa(len(part))))
		hash.Write([]byte{':'})
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))[:12]
}

var (
	// claudeBoxRuleRe is the solid rule that opens a Claude dialog's box: box
	// drawing characters only, at the left margin. Lines inside the box are
	// indented, and its inner separators are dashed (╌), so neither is it.
	claudeBoxRuleRe = regexp.MustCompile(`^ ?[─━]{6,}\s*$`)
	// codexHeadingRe is the sentence a Codex dialog opens with.
	codexHeadingRe = regexp.MustCompile(`(?i)^\s*(?:would you like to\b|do you trust\b|do you want to\b|allow (?:the\b.*\bmcp\b|codex\b)|update available\b)`)
	menuCaretRe    = regexp.MustCompile(`^(\s*)[❯›>](\s*)(\d{1,2}[.)])`)
)

// menuIdentityRows are the menu's lines as displayed, minus the moving cursor,
// a row's own countdown and blanks at a row's end.
func menuIdentityRows(rows []string) string {
	out := make([]string, len(rows))
	for index, row := range rows {
		row = strings.TrimRight(row, " \t\r")
		row = menuCaretRe.ReplaceAllString(row, "$1 $2$3")
		out[index] = trailingClockRe.ReplaceAllString(row, "")
	}
	return strings.Join(out, "\n")
}

// dropTerminalNotice removes the provider's countdown notice, and the blank
// lines after it, when it is the last paragraph before the question.
func dropTerminalNotice(lines []string) []string {
	spans := autoNoticeSpans(lines)
	if len(spans) == 0 {
		return lines
	}
	span := spans[len(spans)-1]
	following := 0
	for _, line := range lines[span[1]:] {
		if strings.TrimSpace(line) != "" {
			following++
		}
	}
	if following > 1 {
		return lines
	}
	end := span[1]
	for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
		end++
	}
	return append(append([]string{}, lines[:span[0]]...), lines[end:]...)
}

var (
	// codexCommandLineRe is the line a Codex request's command starts on.
	codexCommandLineRe = regexp.MustCompile(`^\s*\$ `)
)

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

func lastNonBlank(lines []string) int {
	last := -1
	for index, line := range lines {
		if !isBlank(line) {
			last = index
		}
	}
	return last
}

// blankInsideRequest reports whether a blank line, empty or of spaces, sits
// inside the request region [from, to) of the dialog, after trimming the blank
// lines that end the region.
func blankInsideRequest(dialog []string, from, to int) bool {
	for to > from && isBlank(dialog[to-1]) {
		to--
	}
	for index := from; index < to; index++ {
		if isBlank(dialog[index]) {
			return true
		}
	}
	return false
}

// codexRequestRegion is where Codex's request lies: from the line its command
// starts on to the last non-blank line. ok is false for a dialog with no
// command line (a trust or MCP question), which has no region to check.
func codexRequestRegion(dialog []string) (from, to int, ok bool) {
	for index, line := range dialog {
		if codexCommandLineRe.MatchString(line) {
			return index, lastNonBlank(dialog) + 1, true
		}
	}
	return 0, 0, false
}

// claudeRequestRegion is where Claude's request lies: the run of lines drawn
// indented three or more spaces (the box content) that starts at the first of
// them. Claude's own lines (header, warnings, the question) are indented one.
func claudeRequestRegion(dialog []string) (from, to int, ok bool) {
	from = -1
	for index, line := range dialog {
		if !isBlank(line) && len(line)-len(strings.TrimLeft(line, " ")) >= 3 {
			from = index
			break
		}
	}
	if from < 0 {
		return 0, 0, false
	}
	to = from
	for index := from; index < len(dialog); index++ {
		line := dialog[index]
		if isBlank(line) {
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " ")) < 3 {
			break
		}
		to = index + 1
	}
	return from, to, true
}

// interiorTrailingBlanks reports whether a line followed by more text in the
// dialog ends in blanks (see the identity notes above).
func interiorTrailingBlanks(dialog []string) bool {
	last := lastNonBlank(dialog)
	for index := 0; index < last; index++ {
		line := strings.TrimSuffix(dialog[index], "\r")
		if !isBlank(line) && strings.TrimRight(line, " \t") != line {
			return true
		}
	}
	return false
}

// dialogPrompt builds the prompt of the dialog lines, whose menu follows. ok is
// false when the displayed text cannot support an exact identity.
func dialogPrompt(provider string, kind PromptKind, dialog, menu []string, options []PromptOption) (Prompt, bool) {
	if interiorTrailingBlanks(dialog) {
		return Prompt{}, false
	}
	var from, to int
	var region bool
	if provider == "claude" {
		from, to, region = claudeRequestRegion(dialog)
	} else {
		from, to, region = codexRequestRegion(dialog)
	}
	if region && blankInsideRequest(dialog, from, to) {
		return Prompt{}, false
	}
	rawRows := make([]string, len(dialog))
	for index, line := range dialog {
		rawRows[index] = strings.TrimRight(line, " \t\r")
	}
	title, request, _, _ := readMenuRequest(dialog, len(dialog), false)
	return Prompt{
		ID:       PromptIdentity(kind, strings.Join(rawRows, "\n"), menuIdentityRows(menu)),
		Kind:     kind,
		Provider: provider,
		Title:    title,
		Request:  request,
		Options:  options,
	}, true
}

// ParseClaudePermissionPrompt reads the permission dialog open at the bottom of
// a Claude pane. ok is false when it is not one, or when no rule line above it
// proves where its box begins.
func ParseClaudePermissionPrompt(pane string) (Prompt, bool) {
	body, options, menuStart, ok := locateMenu(pane, []string{"Esc to cancel"})
	if !ok {
		return Prompt{}, false
	}
	start := -1
	for index := menuStart - 1; index >= 0; index-- {
		if claudeBoxRuleRe.MatchString(body[index]) {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return Prompt{}, false
	}
	dialog := dropTerminalNotice(body[start:menuStart])
	return dialogPrompt("claude", PromptPermission, dialog, body[menuStart:], options)
}

// locateCodexMenu finds the numbered menu and footer at the bottom of a Codex
// pane.
func locateCodexMenu(pane string) (body []string, options []PromptOption, menuStart int, ok bool) {
	body, options, menuStart, ok = locateMenu(pane, []string{"Press enter to confirm", "enter to confirm", "Enter to continue"})
	if !ok {
		body, options, menuStart, ok = locateMenu(pane, nil)
	}
	return body, options, menuStart, ok
}

// topmostCodexHeading is the first heading sentence in body[from:to], or -1.
func topmostCodexHeading(body []string, from, to int) int {
	for index := from; index < to; index++ {
		if codexHeadingRe.MatchString(promptBoxEdgeRe.ReplaceAllString(body[index], " ")) {
			return index
		}
	}
	return -1
}

// CodexApprovalDialogOpen reports whether a heading sentence and a numbered
// menu with its footer stand at the bottom of the pane. It says a dialog is
// open; it does not identify one (ParseCodexApprovalPrompt does).
func CodexApprovalDialogOpen(pane string) bool {
	body, _, menuStart, ok := locateCodexMenu(pane)
	return ok && topmostCodexHeading(body, 0, menuStart) >= 0
}

// ParseCodexApprovalPrompt reads the approval or trust dialog open at the
// bottom of a Codex pane and identifies it. origin is the pane's marker line
// (CaptureInfo.Origin): the dialog starts at the topmost heading sentence
// below it, which proves the capture still holds every line from the marker
// on. A heading-like line inside the command sits below the real heading, so
// it can only lengthen what is hashed. ok is false unless the capture's first
// line is the marker, so no id comes of a capture that may have lost the
// dialog's start.
func ParseCodexApprovalPrompt(pane, origin string) (Prompt, bool) {
	origin = strings.TrimRight(origin, " \t\r")
	if origin == "" {
		return Prompt{}, false
	}
	body, options, menuStart, ok := locateCodexMenu(pane)
	if !ok {
		return Prompt{}, false
	}
	// The marker is the pane's first output, so in a capture that still holds
	// everything it is the first line. A copy anywhere else (say inside a
	// command) proves nothing and is not looked for.
	marker := 0
	if len(body) == 0 || strings.TrimRight(body[0], " \t\r") != origin || marker >= menuStart {
		return Prompt{}, false
	}
	start := topmostCodexHeading(body, marker+1, menuStart)
	if start < 0 {
		return Prompt{}, false
	}
	return dialogPrompt("codex", PromptApproval, body[start:menuStart], body[menuStart:], options)
}
