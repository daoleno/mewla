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
func menuRequest(lines []string, menuStart int) (title, request string) {
	var body []string
	for index := menuStart - 1; index >= 0 && len(body) < 40; index-- {
		text := strings.TrimSpace(promptBoxEdgeRe.ReplaceAllString(lines[index], ""))
		if promptRuleRe.MatchString(strings.TrimSpace(lines[index])) {
			if len(body) > 0 && !strings.Contains(lines[index], "╌") {
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
	for len(body) > 0 && body[0] == "" {
		body = body[1:]
	}
	if len(body) == 0 {
		return "", ""
	}
	// The first line of a Claude permission box is its heading ("Bash
	// command · from the general-purpose agent"), followed by a one-line
	// summary of the tool.
	if len(body) > 1 && body[1] != "" && !strings.Contains(body[0], "?") && len(body[0]) < 120 &&
		(strings.Contains(body[0], " · ") || strings.HasSuffix(body[0], "command") || strings.HasSuffix(body[0], "request")) {
		title = body[0]
		body = body[1:]
	}
	request = strings.TrimSpace(strings.Join(body, "\n"))
	request = trimRunes(request, 1800)
	return title, request
}

var (
	// A dialog Claude will refuse by itself says so, with a clock that ticks:
	// "Claude Code will automatically deny this request in about 28 seconds".
	autoDenyLineRe = regexp.MustCompile(`(?i)auto(?:matically)?[ -]?(?:deny|denied|denies|reject|dismiss|cancel)`)
	// An elapsed or remaining time in brackets: "Running… (12s)", "(1m 5s)".
	clockInBracketsRe = regexp.MustCompile(`\s*[(\[]\s*\d+(?:\.\d+)?\s*(?:ms|s|m|h)(?:\s*\d+\s*s)?\s*[)\]]`)
	// A spinner or progress glyph that leads a line and turns as time passes.
	spinnerRunes = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏✻✶✳✢✽·◐◓◑◒"
)

// withoutVolatileLines drops the lines of a prompt's text that only count time.
func withoutVolatileLines(text string) string {
	lines := strings.Split(text, "\n")
	kept := lines[:0:0]
	for _, line := range lines {
		if !autoDenyLineRe.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// stableText is what a prompt's identity may rest on: its words without the
// clocks and spinners that change while it stays open.
func stableText(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		line = strings.TrimLeft(line, " \t"+spinnerRunes)
		lines[index] = clockInBracketsRe.ReplaceAllString(line, "")
	}
	return strings.Join(lines, "\n")
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
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 {
		return Prompt{}, false
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
		return Prompt{}, false
	}
	body := lines
	if footer >= 0 {
		body = lines[:footer]
	}
	options, start, ok := parseNumberedMenu(body)
	if !ok || len(options) < 2 {
		return Prompt{}, false
	}
	title, request := menuRequest(body, start)
	request = withoutVolatileLines(request)
	labels := make([]string, 0, len(options)+2)
	labels = append(labels, stableText(request))
	for _, option := range options {
		labels = append(labels, stableText(option.Label))
	}
	return Prompt{
		ID:       PromptID(kind, labels...),
		Kind:     kind,
		Provider: provider,
		Title:    title,
		Request:  request,
		Options:  options,
	}, true
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
