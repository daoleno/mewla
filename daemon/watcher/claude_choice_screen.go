package watcher

import (
	"regexp"
	"strings"
	"unicode"
)

// Claude Code AskUserQuestion TUI (verified against 2.1.296, wide and narrow
// panes). The pane is plain text after capture-pane -p:
//
//	←  ☐ Fruit  ☒ Colors  ✔ Submit  →      tab bar (single question: " ☐ Size")
//	Which colors?                          question text, wrapped by Ink
//	❯ 1. [✔] Red                           numbered rows; [ ]/[✔] only for multi-select
//	         warm                          description or wrapped label
//	  4. [✔] typed other text              "Type something" row holds the Other text
//	     Submit                            multi-select only
//	────────                               separator, then "N. Chat about this"
//	Enter to select · … · Esc to cancel    footer (may wrap)
//
// Multi-question and multi-select prompts end on a review screen:
//
//	Review your answers
//	 ● Which colors?
//	   → Red, Blue
//	Ready to submit your answers?
//	❯ 1. Submit answers
//	  2. Cancel
//
// The preview layout draws a box to the right of each row and has no
// "Type something" row.

type claudeChoiceScreenKind int

const (
	claudeChoiceScreenNone claudeChoiceScreenKind = iota
	claudeChoiceScreenQuestion
	claudeChoiceScreenReview
)

type claudeChoiceRow struct {
	Number  int
	Text    string // first line, whitespace-collapsed
	Full    string // first line plus continuation lines, whitespace-collapsed
	Checked bool
	HasBox  bool // multi-select checkbox present
	Cursor  bool
}

type claudeChoiceReviewItem struct {
	Question string
	Answer   string
}

type claudeChoiceScreen struct {
	Kind         claudeChoiceScreenKind
	Tabs         []string // tab headers, in order, without the Submit tab
	TabsAnswered []bool
	Question     string // whitespace-free question text
	Rows         []claudeChoiceRow
	SubmitRow    bool
	SubmitCursor bool
	Review       []claudeChoiceReviewItem
	ReviewRows   []claudeChoiceRow
}

var (
	claudeChoiceRowRe       = regexp.MustCompile(`^\s*(❯)?\s*(\d+)\.\s(.*)$`)
	claudeChoiceCheckboxRe  = regexp.MustCompile(`^\[([ ✔])\]\s?(.*)$`)
	claudeChoiceBoxRe       = regexp.MustCompile(`\s{2,}[│┌└├┐┘].*$`)
	claudeChoiceSubmitRowRe = regexp.MustCompile(`^\s*(❯)?\s*Submit\s*$`)
	claudeChoiceTabRe       = regexp.MustCompile(`([☐☒])\s+(.+?)(?:\s{2,}|$)`)
)

func isClaudeChoiceSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, r := range trimmed {
		if r != '─' {
			return false
		}
	}
	return true
}

func isClaudeChoiceTabBar(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "←") || strings.HasPrefix(trimmed, "☐") || strings.HasPrefix(trimmed, "☒")
}

// parseClaudeChoiceScreen reads the AskUserQuestion prompt from the visible
// pane. It returns Kind none for anything it cannot prove.
func parseClaudeChoiceScreen(pane string) claudeChoiceScreen {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	footer, review := -1, -1
	for index := len(lines) - 1; index >= 0; index-- {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, "Enter to select") {
			footer = index
			break
		}
		if trimmed == "Ready to submit your answers?" {
			review = index
			break
		}
	}
	if footer < 0 && review < 0 {
		return claudeChoiceScreen{}
	}
	end := footer
	if end < 0 {
		end = review
	}
	tabBar := -1
	for index := end - 1; index >= 0; index-- {
		if isClaudeChoiceTabBar(lines[index]) {
			tabBar = index
			break
		}
	}
	if tabBar < 0 {
		return claudeChoiceScreen{}
	}
	screen := claudeChoiceScreen{}
	for _, match := range claudeChoiceTabRe.FindAllStringSubmatch(strings.Trim(strings.TrimSpace(lines[tabBar]), "←→ "), -1) {
		header := strings.TrimSpace(match[2])
		screen.Tabs = append(screen.Tabs, header)
		screen.TabsAnswered = append(screen.TabsAnswered, match[1] == "☒")
	}
	if len(screen.Tabs) == 0 {
		return claudeChoiceScreen{}
	}
	if review >= 0 {
		return parseClaudeChoiceReview(screen, lines[tabBar+1:], lines[review+1:])
	}
	return parseClaudeChoiceQuestion(screen, lines[tabBar+1:footer])
}

func parseClaudeChoiceQuestion(screen claudeChoiceScreen, body []string) claudeChoiceScreen {
	var question strings.Builder
	index := 0
	for ; index < len(body); index++ {
		if claudeChoiceRowRe.MatchString(body[index]) {
			break
		}
		question.WriteString(body[index])
	}
	screen.Question = claudeChoiceCompact(question.String())
	if screen.Question == "" {
		return claudeChoiceScreen{}
	}
	for ; index < len(body); index++ {
		line := body[index]
		if isClaudeChoiceSeparator(line) {
			break
		}
		if match := claudeChoiceSubmitRowRe.FindStringSubmatch(line); match != nil {
			screen.SubmitRow = true
			screen.SubmitCursor = match[1] != ""
			continue
		}
		match := claudeChoiceRowRe.FindStringSubmatch(line)
		if match == nil {
			if len(screen.Rows) > 0 && strings.TrimSpace(line) != "" {
				row := &screen.Rows[len(screen.Rows)-1]
				row.Full = claudeChoiceCollapse(row.Full + " " + claudeChoiceBoxRe.ReplaceAllString(line, ""))
			}
			continue
		}
		text := claudeChoiceBoxRe.ReplaceAllString(match[3], "")
		row := claudeChoiceRow{Number: claudeChoiceAtoi(match[2]), Cursor: match[1] != ""}
		if box := claudeChoiceCheckboxRe.FindStringSubmatch(text); box != nil {
			row.HasBox = true
			row.Checked = box[1] == "✔"
			text = box[2]
		}
		row.Text = claudeChoiceCollapse(text)
		row.Full = row.Text
		if row.Number != len(screen.Rows)+1 {
			return claudeChoiceScreen{}
		}
		screen.Rows = append(screen.Rows, row)
	}
	if len(screen.Rows) == 0 {
		return claudeChoiceScreen{}
	}
	screen.Kind = claudeChoiceScreenQuestion
	return screen
}

func parseClaudeChoiceReview(screen claudeChoiceScreen, body, tail []string) claudeChoiceScreen {
	start := -1
	for index, line := range body {
		if strings.TrimSpace(line) == "Review your answers" {
			start = index
			break
		}
	}
	if start < 0 {
		return claudeChoiceScreen{}
	}
	var current *claudeChoiceReviewItem
	inAnswer := false
	for _, line := range body[start+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Ready to submit your answers?" {
			break
		}
		switch {
		case strings.HasPrefix(trimmed, "● "):
			screen.Review = append(screen.Review, claudeChoiceReviewItem{Question: strings.TrimPrefix(trimmed, "● ")})
			current = &screen.Review[len(screen.Review)-1]
			inAnswer = false
		case strings.HasPrefix(trimmed, "→ ") && current != nil:
			current.Answer = strings.TrimPrefix(trimmed, "→ ")
			inAnswer = true
		case trimmed != "" && current != nil:
			// Ink wraps long lines with a hanging indent; join them without
			// inventing spaces so CJK text stays contiguous.
			if inAnswer {
				current.Answer += trimmed
			} else {
				current.Question += trimmed
			}
		}
	}
	for _, line := range tail {
		if isClaudeChoiceSeparator(line) {
			break
		}
		match := claudeChoiceRowRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		screen.ReviewRows = append(screen.ReviewRows, claudeChoiceRow{
			Number: claudeChoiceAtoi(match[2]),
			Text:   claudeChoiceCollapse(match[3]),
			Cursor: match[1] != "",
		})
	}
	if len(screen.Review) == 0 || len(screen.ReviewRows) == 0 {
		return claudeChoiceScreen{}
	}
	screen.Kind = claudeChoiceScreenReview
	return screen
}

func (s claudeChoiceScreen) cursorRow() int {
	for _, row := range s.Rows {
		if row.Cursor {
			return row.Number
		}
	}
	return 0
}

func (s claudeChoiceScreen) row(number int) (claudeChoiceRow, bool) {
	if number < 1 || number > len(s.Rows) {
		return claudeChoiceRow{}, false
	}
	return s.Rows[number-1], true
}

// claudeChoiceCompact removes all whitespace: Ink re-wraps text at any width,
// and CJK text wraps without a space, so only the non-space runes are stable.
func claudeChoiceCompact(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func claudeChoiceCollapse(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func claudeChoiceAtoi(value string) int {
	number := 0
	for _, r := range value {
		number = number*10 + int(r-'0')
	}
	return number
}
