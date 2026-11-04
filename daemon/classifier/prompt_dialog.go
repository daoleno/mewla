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
// spaces at line ends included, with only positively identified provider UI
// removed (Claude's terminal countdown notice, the cursor caret and a trailing
// clock on a menu row).
//
// What the screen can and cannot prove:
//   - It is the displayed text, not the program's bytes. tmux keeps the spaces
//     a program wrote at the end of a line and, with -J, joins lines the
//     terminal wrapped; it cannot show a tab, or a line the program itself
//     trimmed or re-wrapped. Two commands that render identically get one id.
//   - Whether the capture holds the whole dialog is a property of the capture,
//     not of the text. A heading-looking line proves nothing: a command can
//     contain one, and a clipped capture can begin at it. Claude's dialog box
//     begins at a rule line at the margin, which a command's own lines (drawn
//     indented) do not produce. Codex's dialog has no such line, so a Codex id
//     is issued only when the capture starts at the pane's first line with
//     nothing evicted (CaptureInfo.Complete); otherwise the dialog is reported
//     blocked with no prompt, and so no id to answer.
//   - A capture not taken with -J (CaptureInfo.Exact) never yields an id.
//
//	provider  dialog starts at                       answerable id only when
//	claude    the solid rule line above the box       Exact capture, rule line found
//	codex     the topmost heading sentence            Exact and Complete capture, heading found
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

// menuIdentityRows are the menu's lines as displayed, minus the moving cursor
// and a row's own countdown.
func menuIdentityRows(rows []string) string {
	out := make([]string, len(rows))
	for index, row := range rows {
		row = strings.TrimSuffix(row, "\r")
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

// dialogPrompt builds the prompt of the dialog lines[start:menuStart].
func dialogPrompt(provider string, kind PromptKind, dialog, menu []string, options []PromptOption) Prompt {
	rawRows := make([]string, len(dialog))
	for index, line := range dialog {
		rawRows[index] = strings.TrimSuffix(line, "\r")
	}
	title, request, _, _ := readMenuRequest(dialog, len(dialog), false)
	return Prompt{
		ID:       PromptIdentity(kind, strings.Join(rawRows, "\n"), menuIdentityRows(menu)),
		Kind:     kind,
		Provider: provider,
		Title:    title,
		Request:  request,
		Options:  options,
	}
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
	return dialogPrompt("claude", PromptPermission, dialog, body[menuStart:], options), true
}

// ParseCodexApprovalPrompt reads the approval or trust dialog open at the
// bottom of a Codex pane. The dialog starts at the topmost heading sentence
// above its menu, so a heading-like line inside the command, which sits below
// the real one, can only lengthen what is hashed. ok is false when no heading
// is found.
func ParseCodexApprovalPrompt(pane string) (Prompt, bool) {
	body, options, menuStart, ok := locateMenu(pane, []string{"Press enter to confirm", "enter to confirm", "Enter to continue"})
	if !ok {
		body, options, menuStart, ok = locateMenu(pane, nil)
		if !ok {
			return Prompt{}, false
		}
	}
	start := -1
	for index := 0; index < menuStart; index++ {
		if codexHeadingRe.MatchString(promptBoxEdgeRe.ReplaceAllString(body[index], " ")) {
			start = index
			break
		}
	}
	if start < 0 {
		return Prompt{}, false
	}
	return dialogPrompt("codex", PromptApproval, body[start:menuStart], body[menuStart:], options), true
}
