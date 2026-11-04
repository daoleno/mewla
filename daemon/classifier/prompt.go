package classifier

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// PromptKind names what a blocking prompt in an agent's terminal asks for.
type PromptKind string

const (
	// PromptPermission is a tool permission request, also one raised by a
	// sub-agent: a numbered menu under "Do you want to ...".
	PromptPermission PromptKind = "permission"
	// PromptPlan is plan approval.
	PromptPlan PromptKind = "plan"
	// PromptChoice is AskUserQuestion: one or more questions on tabs, each
	// single or multi select.
	PromptChoice PromptKind = "choice"
	// PromptStartup is a launch-time menu: trust this folder, accept a mode,
	// pick a theme or login.
	PromptStartup PromptKind = "startup"
	// PromptApproval is a Codex approval menu.
	PromptApproval PromptKind = "approval"
)

// Prompt is the one structured shape of a blocking agent prompt. The pane
// parser fills it, Brain receives it in the Worker's needs_input event, the
// app renders its card from it, and `mewla worker answer` names it by ID.
// Nothing downstream reads the screen again to learn what is asked.
type Prompt struct {
	// ID identifies this exact prompt: a different question, option set or
	// tab list is a different ID, the same prompt on a later poll keeps it.
	// An answer names the ID it was composed for and is refused when the
	// prompt on screen no longer has it.
	ID       string     `json:"id"`
	Kind     PromptKind `json:"kind"`
	Provider string     `json:"provider"`
	// Title is the prompt's own heading, e.g. "Bash command · from the
	// general-purpose agent" or "Waiting for plan approval".
	Title string `json:"title"`
	// Request is what is being asked about: the command and why it needs
	// approval, the plan, the folder.
	Request string `json:"request,omitempty"`
	// Options is the numbered menu of a permission, plan, startup or
	// approval prompt. A choice prompt carries its menus in Questions.
	Options []PromptOption `json:"options,omitempty"`
	// Questions is a choice prompt's questions in tab order. The pane shows
	// one at a time, so only the transcript can fill all of them.
	Questions []PromptQuestion `json:"questions,omitempty"`
	// CallID is the provider tool call behind a choice prompt, when known.
	CallID string `json:"call_id,omitempty"`
}

// PromptOption is one row of a numbered menu. Number is the digit the
// provider shows, and the key that picks it.
type PromptOption struct {
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Selected    bool   `json:"selected,omitempty"`
}

// PromptQuestion is one question of a choice prompt.
type PromptQuestion struct {
	Header      string         `json:"header,omitempty"`
	Question    string         `json:"question"`
	MultiSelect bool           `json:"multi_select,omitempty"`
	AllowsOther bool           `json:"allows_other,omitempty"`
	Answered    bool           `json:"answered,omitempty"`
	Options     []PromptOption `json:"options"`
}

// PromptID derives the stable identity of a prompt from what it shows.
func PromptID(kind PromptKind, parts ...string) string {
	hash := sha256.New()
	hash.Write([]byte(kind))
	for _, part := range parts {
		hash.Write([]byte{0})
		hash.Write([]byte(strings.Join(strings.Fields(part), " ")))
	}
	return hex.EncodeToString(hash.Sum(nil))[:12]
}

var (
	promptRowRe     = regexp.MustCompile(`^\s*(?:[❯›>]\s*)?(\d{1,2})[.)]\s+(\S.*)$`)
	promptCursorRe  = regexp.MustCompile(`^\s*[❯›>]\s*\d`)
	promptBoxEdgeRe = regexp.MustCompile(`^[\s│┃|]+`)
	promptRuleRe    = regexp.MustCompile(`^[\s─━╌╍┄┅┈┉-]{6,}$`)
	promptTabBarRe  = regexp.MustCompile(`([☐☒])\s+(.+?)(?:\s{2,}|$)`)
)

// parseNumberedMenu returns the numbered rows that end the given lines and
// the index of the first menu line. Indented lines under a row are its
// description. The menu must sit at the bottom of lines, footer already
// trimmed by the caller, and must start at "1.".
func parseNumberedMenu(lines []string) ([]PromptOption, int, bool) {
	var options []PromptOption
	var description []string
	footerLines := 0
	for index := len(lines) - 1; index >= 0; index-- {
		text := strings.TrimRight(lines[index], " \t")
		if strings.TrimSpace(text) == "" {
			continue
		}
		if match := promptRowRe.FindStringSubmatch(text); match != nil {
			number := atoiSmall(match[1])
			option := PromptOption{Number: number, Label: collapse(match[2]), Selected: promptCursorRe.MatchString(text)}
			if len(description) > 0 {
				option.Description = collapse(strings.Join(description, " "))
				description = nil
			}
			options = append([]PromptOption{option}, options...)
			if number == 1 {
				return options, index, true
			}
			continue
		}
		if strings.HasPrefix(text, "   ") {
			description = append([]string{strings.TrimSpace(text)}, description...)
			continue
		}
		if len(options) == 0 && len(description) == 0 && footerLines < 2 {
			// A footer under the menu, such as "ctrl-g to edit plan".
			footerLines++
			continue
		}
		return nil, 0, false
	}
	return nil, 0, false
}

func atoiSmall(text string) int {
	value := 0
	for _, r := range text {
		value = value*10 + int(r-'0')
	}
	return value
}

func collapse(text string) string { return strings.Join(strings.Fields(text), " ") }

// menuRequest reads the heading and body above a menu: back to the previous
// rule line, box edges stripped. The line that asks ("Do you want to
// proceed?") is the question; the lines above it are the request.
func menuRequest(lines []string, menuStart int) (title, request, full string, meta menuMeta) {
	var body []string
	index := menuStart - 1
	for ; index >= 0 && len(body) < 40; index-- {
		text := strings.TrimSpace(promptBoxEdgeRe.ReplaceAllString(lines[index], ""))
		if promptRuleRe.MatchString(strings.TrimSpace(lines[index])) {
			if len(body) > 0 && !strings.Contains(lines[index], "╌") {
				meta.ruled = true
				break
			}
			continue
		}
		if text == "" {
			if len(body) > 0 {
				body = append([]string{""}, body...)
			}
			continue
		}
		body = append([]string{text}, body...)
	}
	// Forty lines read and no rule above them: the request goes on above what
	// was read, so what was read is not the whole of it.
	meta.capped = !meta.ruled && index >= 0
	for len(body) > 0 && body[0] == "" {
		body = body[1:]
	}
	if len(body) == 0 {
		return "", "", "", meta
	}
	// The first line of a Claude permission box is its heading ("Bash
	// command · from the general-purpose agent"), followed by a one-line
	// summary of the tool.
	if len(body) > 1 && body[1] != "" && !strings.Contains(body[0], "?") && len(body[0]) < 120 &&
		(strings.Contains(body[0], " · ") || strings.HasSuffix(body[0], "command") || strings.HasSuffix(body[0], "request")) {
		title = body[0]
		body = body[1:]
	}
	full = strings.TrimSpace(strings.Join(body, "\n"))
	return title, trimRunes(full, 1800), full, meta
}

// menuMeta says how far a prompt's request could be read.
type menuMeta struct {
	// ruled: the request is bounded above by the rule line that opens its box.
	ruled bool
	// capped: the read stopped at its line limit inside the request.
	capped bool
}

var (
	// The notice a dialog carries when Claude will refuse it by itself, with a
	// clock that ticks: "Claude Code will automatically deny this request in
	// about 28 seconds", "... automatically block this request in 2:00, to
	// avoid holding up an unattended session". The verb has varied between
	// releases, so the notice is recognized by its whole shape: it opens a
	// paragraph with the provider's name, says it will act on this request,
	// and counts down. A line of a command that merely says "automatically
	// deny" or "will automatically retry the request" is not this notice.
	autoNoticeStartRe = regexp.MustCompile(`(?i)^(?:⏱\s*)?(?:claude\s+code|this\s+agent)\b`)
	autoNoticeRe      = regexp.MustCompile(`(?i)^(?:⏱\s*)?(?:claude\s+code|this\s+agent)\s+will\s+auto(?:matically)?[\s-]*\w+\s+(?:this|the)\s+request\s+in\s+(?:about\s+|under\s+|less\s+than\s+)?(?:\d+:\d{2}|(?:\d+(?:\.\d+)?|an?|one)\s*(?:seconds?|secs?|minutes?|mins?|s|m))\b`)
	// What ends a notice's paragraph when no blank line does: the question
	// under it or a menu row.
	autoNoticeEndRe = regexp.MustCompile(`(?i)^(?:do you want\b|would you like\b|esc to cancel)`)
	// A remaining or elapsed time in brackets that ends a menu row: "Yes (28s)",
	// "Yes (1m 5s)". Only a row's own trailing clock is provider UI; a clock
	// anywhere else in a prompt may be part of the command being approved.
	trailingClockRe = regexp.MustCompile(`\s*[(\[]\s*\d+(?:\.\d+)?\s*(?:ms|s|m|h)(?:\s*\d+\s*s)?\s*[)\]]\s*$`)
)

// autoNoticeMaxLines bounds a notice paragraph: even a very narrow pane wraps
// it in a handful of lines.
const autoNoticeMaxLines = 7

// autoNoticeSpans finds the provider's countdown notices in lines: each span
// is [first, end) of consecutive lines, wrapped however the pane is wide.
func autoNoticeSpans(lines []string) [][2]int {
	var spans [][2]int
	for index := 0; index < len(lines); {
		first := strings.TrimSpace(promptBoxEdgeRe.ReplaceAllString(lines[index], ""))
		if !autoNoticeStartRe.MatchString(first) {
			index++
			continue
		}
		parts := []string{first}
		end := index + 1
		for ; end < len(lines) && end-index < autoNoticeMaxLines; end++ {
			next := strings.TrimSpace(promptBoxEdgeRe.ReplaceAllString(lines[end], ""))
			if next == "" || autoNoticeEndRe.MatchString(next) || promptRowRe.MatchString(lines[end]) {
				break
			}
			parts = append(parts, next)
		}
		if autoNoticeRe.MatchString(strings.Join(parts, " ")) {
			spans = append(spans, [2]int{index, end})
			index = end
			continue
		}
		index++
	}
	return spans
}

// withoutVolatileLines drops the provider's countdown notice from a prompt's
// text. It is a paragraph: in a narrow pane it wraps, and the wrapped tail
// ("to avoid holding up an unattended session") carries no keyword, so a
// per-line filter leaves a fragment whose shape changes as the clock does.
//
// Only the notice the provider paints where it paints it is dropped: the last
// paragraph of the request, ahead of the question. The same words inside a
// command or heredoc are payload and stay, as does any earlier paragraph.
func withoutVolatileLines(text string) string {
	lines := strings.Split(text, "\n")
	spans := autoNoticeSpans(lines)
	if len(spans) == 0 {
		return text
	}
	span := spans[len(spans)-1]
	following := 0
	for _, line := range lines[span[1]:] {
		if strings.TrimSpace(line) != "" {
			following++
		}
	}
	if following > 1 {
		return text
	}
	kept := append(append([]string{}, lines[:span[0]]...), lines[span[1]:]...)
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// stableLabel is a menu row as its identity may rest on it: without the
// clock the provider counts on the row itself ("Yes (28s)").
func stableLabel(label string) string {
	return trailingClockRe.ReplaceAllString(label, "")
}

func trimRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return "…" + string(runes[len(runes)-limit:])
}

// ParseMenuPrompt reads a blocking numbered-menu prompt (permission, plan,
// startup, approval) from the bottom of a provider's pane. footer says which
// bottom line proves the prompt is open now; it returns false for anything
// else, so prose in the conversation above never becomes a prompt.
func ParseMenuPrompt(provider string, kind PromptKind, pane string, footerPrefixes ...string) (Prompt, bool) {
	prompt, _, ok := parseMenuPrompt(provider, kind, pane, footerPrefixes...)
	return prompt, ok
}

// ParseBoxedMenuPrompt reads a prompt whose request is a box a rule line opens
// (Claude's permission dialog) from window, a pane cut down to the provider's
// latest banner, and holds it to the whole pane. The request must be bounded
// above by its rule line: if the read ran out of lines first, or the cut fell
// inside the request (a command line that looks like the banner) and the whole
// pane does not bound it, no prompt is returned. An answerable identity never
// rests on a truncated request; the dialog is still reported as blocked.
func ParseBoxedMenuPrompt(provider string, kind PromptKind, window, whole string, footerPrefixes ...string) (Prompt, bool) {
	prompt, meta, ok := parseMenuPrompt(provider, kind, window, footerPrefixes...)
	if !ok || meta.capped {
		return Prompt{}, false
	}
	if !meta.ruled && window != whole {
		// The read reached the first line of the window: the request may go on above it.
		full, fullMeta, fullOK := parseMenuPrompt(provider, kind, whole, footerPrefixes...)
		switch {
		case fullOK && full.ID == prompt.ID:
		case fullOK && fullMeta.ruled && !fullMeta.capped:
			return full, true
		default:
			return Prompt{}, false
		}
	}
	return prompt, true
}

func parseMenuPrompt(provider string, kind PromptKind, pane string, footerPrefixes ...string) (Prompt, menuMeta, bool) {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 {
		return Prompt{}, menuMeta{}, false
	}
	lines = lines[:last+1]
	// The footer is the bottom line, or the bottom two when it wrapped.
	footer := -1
	for index := len(lines) - 1; index >= 0 && index >= len(lines)-2; index-- {
		trimmed := strings.TrimSpace(lines[index])
		for _, prefix := range footerPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				footer = index
			}
		}
		if footer >= 0 {
			break
		}
	}
	if len(footerPrefixes) > 0 && footer < 0 {
		return Prompt{}, menuMeta{}, false
	}
	body := lines
	if footer >= 0 {
		body = lines[:footer]
	}
	options, start, ok := parseNumberedMenu(body)
	if !ok || len(options) < 2 {
		return Prompt{}, menuMeta{}, false
	}
	title, request, full, meta := menuRequest(body, start)
	// The identity rests on the whole request, not on the part shown.
	request = withoutVolatileLines(request)
	full = withoutVolatileLines(full)
	labels := make([]string, 0, len(options)+2)
	labels = append(labels, full)
	for _, option := range options {
		labels = append(labels, stableLabel(option.Label))
	}
	return Prompt{
		ID:       PromptID(kind, labels...),
		Kind:     kind,
		Provider: provider,
		Title:    title,
		Request:  request,
		Options:  options,
	}, meta, true
}

// ParseChoiceTabs reads the tab headers of an AskUserQuestion prompt, in
// order, without the Submit tab.
func ParseChoiceTabs(line string) (headers []string, answered []bool) {
	for _, match := range promptTabBarRe.FindAllStringSubmatch(strings.Trim(strings.TrimSpace(line), "←→ "), -1) {
		headers = append(headers, strings.TrimSpace(match[2]))
		answered = append(answered, match[1] == "☒")
	}
	return headers, answered
}
