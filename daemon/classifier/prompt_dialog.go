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
//     printed, 19 held, first line gone). What does prove it:
//     Claude: the box begins at a solid rule line at the margin followed by
//     the tool's header line ("Bash command"). A command's own rows are drawn
//     after the header and a `╌` separator (multi-line ones with a " │ " gutter),
//     so a rule-looking row there is followed by a separator or more request,
//     not by a header. A request taller than the pane is not cut by Claude: the
//     box grows and its head, rule included, scrolls off the alternate screen,
//     which has no scrollback behind it (measured on Claude 2.1.318 at 40 and
//     24 rows). That dialog has no start on screen and gets no id
//     (ActivitySignal.Unidentified says why); its exact request is the
//     PermissionRequest hook's, which Brain answers with `worker approve`.
//     Codex on its alternate screen (CaptureInfo.Alternate): the capture is the
//     TUI's own screen, with no scrollback behind it to evict, and Codex cuts a
//     long request to fit ("[… 63 lines] ^a view all", refused here) instead of
//     scrolling its head away. Measured on Codex 0.166.0 at 40 and 14 rows;
//     other versions are not covered by that observation.
//     Codex on the main screen (inline mode): the pane's origin marker
//     (CaptureInfo.Origin) as the capture's first line; tmux evicts from the
//     top, so the pane's first output still being first means nothing was
//     evicted. A copy of the marker elsewhere is not looked at.
//   - Blanks. Codex pads its grid (measured: the heading, menu rows and blank
//     rows carry painted trailing blanks of arbitrary width), so blanks outside
//     the request are provider UI and normalized, and two commands that differ
//     only in blanks the renderer pads share an id when they differ nowhere
//     else. Inside the request (Codex: from the "$ " line, or for a dialog
//     without one the body after the heading, to the last non-blank line;
//     Claude: the run of box lines indented three or more spaces) a blank row,
//     or blanks painted after text that more request text follows, refuse the
//     dialog: cells cannot say whether they were written or left by an earlier
//     frame. Claude is measured (2.1.318): it paints trailing blanks of
//     arbitrary width after rows of its own box, including request rows (9
//     blanks after a command line that has none; a whole row of blanks for an
//     empty command line, which keeps its " │ " gutter). They are not evidence
//     either way, so for Claude they are left out of the id and neither a
//     trailing blank nor a blank row refuses the dialog.
//   - A capture not taken with -J (CaptureInfo.Exact) never yields an id.
//
// Whenever one of these cannot be established the dialog is still reported as
// blocked, with no prompt and so no id to answer.
//
//	provider  dialog starts at                       answerable id only when
//	claude    the solid rule line above the box       Exact capture, rule followed by the tool header found
//	codex     the topmost heading sentence            Exact capture that is the alternate screen or starts at the origin marker, heading found, no truncation marker, request has no blank row or painted trailing blanks
//
// Known collision, not detectable from cells: a renderer that never paints
// trailing blanks shows 'a ' and 'a' alike, so those two commands share an id
// (for Claude this holds for every line of the request, and a request line of
// only blanks equals an empty one);
// and the part of a request Codex cut from the screen is not in the id (hence
// the refusal).
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
	// drawing characters only, at column 0. Every row Claude draws inside the
	// box (header, tip, description, request, warning) starts with a space, and
	// so does a line Claude wraps from a long command, so a wrapped run of
	// dashes is never taken for the rule. Lines inside the box are
	// indented, and its inner separators are dashed (╌), so neither is it.
	claudeBoxRuleRe = regexp.MustCompile(`^[─━]{6,}\s*$`)
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
// starts on (`$ `) to the last non-blank line; for a dialog with no command
// line (a network or MCP question) from the first non-blank line after the
// heading, so the heading and the blank row after it stay provider UI.
func codexRequestRegion(dialog []string) (from, to int, ok bool) {
	last := lastNonBlank(dialog)
	if last < 0 {
		return 0, 0, false
	}
	for index, line := range dialog {
		if codexCommandLineRe.MatchString(line) {
			return index, last + 1, true
		}
	}
	for index := 1; index <= last; index++ {
		if !isBlank(dialog[index]) {
			return index, last + 1, true
		}
	}
	return 0, 0, false
}

// codexTruncatedRe is Codex cutting a long request to fit the screen:
// "[… 63 lines] ^a view all". The screen then never holds the whole request.
var codexTruncatedRe = regexp.MustCompile(`\[(?:…|\.{2,3})\s*\d+ lines?\]|\bview all\b`)

// interiorTrailingBlanks reports whether a non-blank line in dialog[from:to]
// that is followed by more request text ends in blanks (see the identity notes
// above).
func interiorTrailingBlanks(dialog []string, from, to int) bool {
	last := lastNonBlank(dialog[:to])
	for index := from; index < last; index++ {
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
	if provider != "claude" {
		// Codex draws on a cell grid that it pads (measured on 0.166.0: the
		// heading, menu rows and separators all carry painted trailing blanks
		// of arbitrary width), so blanks outside the request are provider UI
		// and normalized; inside it they are held against the dialog.
		from, to, region := codexRequestRegion(dialog)
		for _, line := range dialog {
			if codexTruncatedRe.MatchString(line) {
				return Prompt{}, false
			}
		}
		if region && (interiorTrailingBlanks(dialog, from, to) || blankInsideRequest(dialog, from, to)) {
			return Prompt{}, false
		}
	}
	rawRows := make([]string, len(dialog))
	for index, line := range dialog {
		rawRows[index] = strings.TrimRight(line, " \t\r")
	}
	title, request, _, _ := readMenuRequest(dialog, len(dialog), false)
	request = withoutSeparatorRows(request)
	return Prompt{
		ID:       PromptIdentity(kind, strings.Join(rawRows, "\n"), menuIdentityRows(menu)),
		Kind:     kind,
		Provider: provider,
		Title:    title,
		Request:  request,
		Options:  options,
	}, true
}

// claudeUnidentifiedStart is why a Claude dialog has no id when no rule line
// proves where it begins.
const claudeUnidentifiedStart = "the screen does not show where the dialog starts (a request taller than the pane scrolls its head away, and the capture has no history behind it)"

// claudeDialogStart is the index, in body, of the first line of the permission
// dialog whose menu starts at menuStart, or -1. The dialog opens with a solid
// rule at the margin followed by the tool's header line ("Bash command"). The
// header is what tells that rule from a line of dashes inside the request: in
// the measured layout a request's own rows are drawn after the header and the
// first separator, and a rule-looking row there is followed by a separator or
// more request, not by a header.
func claudeDialogStart(body []string, menuStart int) int {
	for index := menuStart - 1; index >= 0; index-- {
		if !claudeBoxRuleRe.MatchString(body[index]) {
			continue
		}
		if index+1 < menuStart && claudeHeaderRe.MatchString(body[index+1]) {
			return index + 1
		}
	}
	return -1
}

// claudeHeaderRe is the tool header under the rule: text, not box drawing.
var claudeHeaderRe = regexp.MustCompile(`^ \S*[\p{L}\p{N}]`)

var separatorRowRe = regexp.MustCompile(`^[╌─━]{6,}$`)

// withoutSeparatorRows drops the provider's own separator rows from the
// request as Brain reads it. The id does not rest on this text.
func withoutSeparatorRows(request string) string {
	lines := strings.Split(request, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !separatorRowRe.MatchString(strings.TrimSpace(line)) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// ParseClaudePermissionPrompt reads the permission dialog open at the bottom of
// a Claude pane. ok is false when it is not one, or when no rule line above it
// proves where its box begins; reason then says why no id came of a dialog
// that is open.
func ParseClaudePermissionPrompt(pane string) (prompt Prompt, reason string, ok bool) {
	body, options, menuStart, found := locateMenu(pane, []string{"Esc to cancel"})
	if !found {
		return Prompt{}, "", false
	}
	start := claudeDialogStart(body, menuStart)
	if start < 0 {
		return Prompt{}, claudeUnidentifiedStart, false
	}
	dialog := dropTerminalNotice(body[start:menuStart])
	prompt, ok = dialogPrompt("claude", PromptPermission, dialog, body[menuStart:], options)
	return prompt, "", ok
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
// bottom of a Codex pane and identifies it. The dialog starts at the topmost
// heading sentence, and the capture has to prove that sentence is the real
// start (a heading-like line inside a command would otherwise do):
//   - on the alternate screen (capture.Alternate) the capture is the TUI's own
//     screen with no scrollback to evict, and the TUI cuts a long request to fit
//     (codexTruncatedRe) instead of scrolling its head away (observed on Codex
//     0.166.0 at 40 and 14 rows);
//   - on the main screen the pane's origin marker has to be the capture's first
//     line, and the heading is searched below it.
//
// ok is false when neither holds, so no id comes of a capture that may have
// lost the dialog's start.
func ParseCodexApprovalPrompt(pane string, capture CaptureInfo) (Prompt, bool) {
	body, options, menuStart, ok := locateCodexMenu(pane)
	if !ok {
		return Prompt{}, false
	}
	from := 0
	if !capture.Alternate {
		origin := strings.TrimRight(capture.Origin, " \t\r")
		// The marker is the pane's first output, so in a capture that still
		// holds everything it is the first line. A copy anywhere else (say
		// inside a command) proves nothing and is not looked for.
		if origin == "" || len(body) == 0 || strings.TrimRight(body[0], " \t\r") != origin || menuStart <= 0 {
			return Prompt{}, false
		}
		from = 1
	}
	start := topmostCodexHeading(body, from, menuStart)
	if start < 0 {
		return Prompt{}, false
	}
	return dialogPrompt("codex", PromptApproval, body[start:menuStart], body[menuStart:], options)
}
