package classifier

import (
	"regexp"
	"strings"
	"unicode"
)

// Setup screens: what Claude Code and Codex show before their first
// conversation, on a computer where they never ran or are not signed in. Each
// waits on the person, and nobody sits at a Session's terminal, so each is a
// prompt with its real choices, answerable from Chat. Their keys differ from a
// menu's digit: an option may carry the Key it is answered with (Enter, Esc),
// and a sign-in screen takes text (the code its sign-in page shows) or only
// shows a link and a code to enter elsewhere.

const (
	claudeThemeHeading  = "Choose the text style"
	claudeLoginHeading  = "Select login method:"
	claudePasteCodeLine = "Paste code here if prompted"
	claudeRetryLine     = "Press Enter to retry"
	claudeContinueLine  = "Press Enter to continue"
)

var (
	// A numbered row of Claude's login menu: "❯ 1. Claude account with subscription · Individual, …".
	claudeSetupRowRe = regexp.MustCompile(`^\s*(❯)?\s*(\d)\.\s+(\S.*)$`)
	// A theme row: the cursor, then a check on the theme in use, then its name.
	claudeThemeRowRe = regexp.MustCompile(`^\s*(❯)?\s*(?:✔\s*)?(\S.*)$`)
	setupURLRe       = regexp.MustCompile(`^https://\S+$`)
	codexDeviceRe    = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4,6}$`)
	codexSetupRowRe  = regexp.MustCompile(`^\s*(>)?\s*(\d)\.\s+(\S.*)$`)
	codexTrustRowRe  = regexp.MustCompile(`^\s*([›>])?\s*(\d)\.\s+(\S.*)$`)
)

// setupLines is the pane from Claude's latest banner (Codex: its latest
// welcome line) on, trimmed on the right, without trailing blank lines.
func setupLines(pane string) []string {
	lines := strings.Split(strings.ReplaceAll(pane, "\r\n", "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " \t\r")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lastLineStarting is the index of the last line whose trimmed text starts
// with prefix, searched in the last n non-blank lines; -1 when none.
func lastLineStarting(lines []string, prefix string, n int) int {
	for index, seen := len(lines)-1, 0; index >= 0 && seen < n; index-- {
		text := strings.TrimSpace(lines[index])
		if text == "" {
			continue
		}
		seen++
		if strings.HasPrefix(text, prefix) {
			return index
		}
	}
	return -1
}

// paragraph joins the non-blank lines between from and to (exclusive), the
// way a narrow pane wrapped them.
func paragraph(lines []string, from, to int) string {
	var parts []string
	for index := max(from, 0); index < to && index < len(lines); index++ {
		if text := strings.TrimSpace(lines[index]); text != "" {
			parts = append(parts, text)
		}
	}
	return collapse(strings.Join(parts, " "))
}

// claudeSetupPrompt reads the setup screen at the bottom of a Claude pane.
func claudeSetupPrompt(pane string) (Prompt, bool) {
	lines := setupLines(latestClaudePaneWindow(pane))
	if len(lines) == 0 {
		return Prompt{}, false
	}
	if prompt, ok := claudeSignInPrompt(lines); ok {
		return prompt, true
	}
	if prompt, ok := claudeRetryPrompt(lines); ok {
		return prompt, true
	}
	if prompt, ok := claudeLoginMenuPrompt(lines); ok {
		return prompt, true
	}
	if prompt, ok := claudeThemePrompt(lines); ok {
		return prompt, true
	}
	if prompt, ok := claudeContinuePrompt(lines); ok {
		return prompt, true
	}
	return Prompt{}, false
}

// claudeSignInPrompt: "Browser didn't open? Use the url below to sign in",
// the sign-in link (wrapped over as many rows as the pane is narrow), then
// "Paste code here if prompted >". The person opens the link wherever they
// are, signs in, and the page gives them the code to paste.
func claudeSignInPrompt(lines []string) (Prompt, bool) {
	paste := lastLineStarting(lines, claudePasteCodeLine, 1)
	if paste < 0 {
		// A pane narrower than the line wraps it: "Paste code here if" / "prompted >".
		last := len(lines) - 1
		if last < 1 || !strings.HasPrefix(strings.TrimSpace(lines[last-1])+" "+strings.TrimSpace(lines[last]), claudePasteCodeLine) {
			return Prompt{}, false
		}
		paste = last - 1
	}
	start := -1
	for index := paste - 1; index >= 0; index-- {
		if strings.HasPrefix(lines[index], "https://") {
			start = index
			break
		}
	}
	if start < 0 {
		return Prompt{}, false
	}
	var link strings.Builder
	for index := start; index < paste; index++ {
		link.WriteString(strings.TrimSpace(lines[index]))
	}
	if !setupURLRe.MatchString(link.String()) {
		return Prompt{}, false
	}
	return Prompt{
		ID:       PromptID(PromptSignIn, "claude", link.String()),
		Kind:     PromptSignIn,
		Provider: "claude",
		Title:    "Sign in to Claude Code",
		Link:     link.String(),
		Input:    "Code from the sign-in page",
	}, true
}

// claudeRetryPrompt: a sign-in that failed, "OAuth error: …", then "Press
// Enter to retry. (Esc to continue without logging in)".
func claudeRetryPrompt(lines []string) (Prompt, bool) {
	retry := lastLineStarting(lines, claudeRetryLine, 2)
	if retry < 0 {
		return Prompt{}, false
	}
	from := retry - 1
	for from >= 0 && strings.TrimSpace(lines[from]) == "" {
		from--
	}
	for from >= 0 && strings.TrimSpace(lines[from]) != "" && !strings.Contains(lines[from], "OAuth error") {
		from--
	}
	if from < 0 || !strings.Contains(lines[from], "OAuth error") {
		return Prompt{}, false
	}
	request := paragraph(lines, from, retry)
	return Prompt{
		ID:       PromptID(PromptStartup, "claude-sign-in-failed", request),
		Kind:     PromptStartup,
		Provider: "claude",
		Title:    "Sign-in failed",
		Request:  request,
		Options: []PromptOption{
			{Number: 1, Label: "Try again", Key: "Enter", Selected: true},
			{Number: 2, Label: "Continue without signing in", Key: "Escape"},
		},
	}, true
}

// claudeLoginMenuPrompt: "Select login method:" over a numbered menu (no
// footer). A typed digit picks a row and goes on.
func claudeLoginMenuPrompt(lines []string) (Prompt, bool) {
	heading := lastLineStarting(lines, claudeLoginHeading, 20)
	if heading < 0 {
		return Prompt{}, false
	}
	var options []PromptOption
	for _, line := range lines[heading+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if match := claudeSetupRowRe.FindStringSubmatch(line); match != nil {
			if int(match[2][0]-'0') != len(options)+1 {
				return Prompt{}, false
			}
			options = append(options, PromptOption{Number: len(options) + 1, Label: match[3], Selected: match[1] != ""})
			continue
		}
		if len(options) == 0 {
			return Prompt{}, false
		}
		// A row the pane wrapped.
		options[len(options)-1].Label += " " + strings.TrimSpace(line)
	}
	if len(options) < 2 {
		return Prompt{}, false
	}
	parts := []string{"claude-login"}
	for index := range options {
		label, description, _ := strings.Cut(collapse(options[index].Label), " · ")
		options[index].Label, options[index].Description = label, description
		parts = append(parts, label)
	}
	// What the screen says above the heading, back to the banner's art.
	from := heading - 1
	for from >= 0 && strings.TrimSpace(lines[from]) != "" {
		from--
	}
	for from > 0 && strings.TrimSpace(lines[from]) == "" {
		from--
	}
	introStart := from
	for introStart > 0 && strings.TrimSpace(lines[introStart-1]) != "" {
		introStart--
	}
	request := ""
	if from >= 0 && strings.HasPrefix(strings.TrimSpace(lines[introStart]), "Claude Code can be used") {
		request = paragraph(lines, introStart, from+1)
	}
	return Prompt{
		ID:       PromptID(PromptStartup, parts...),
		Kind:     PromptStartup,
		Provider: "claude",
		Title:    "Sign in to Claude Code",
		Request:  request,
		Options:  options,
	}, true
}

// claudeThemePrompt: Claude's first screen, "Choose the text style that looks
// best with your terminal", a menu without numbers under "To change this
// later, run /theme", then a preview. It decides nothing; Mewla answers it
// itself while a message waits (watcher), and the person can answer it here.
func claudeThemePrompt(lines []string) (Prompt, bool) {
	heading := lastLineStarting(lines, claudeThemeHeading, 40)
	if heading < 0 {
		return Prompt{}, false
	}
	start := -1
	for index := heading + 1; index < len(lines) && index <= heading+4; index++ {
		if strings.Contains(lines[index], "/theme") {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return Prompt{}, false
	}
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	var options []PromptOption
	cursor := false
	for index := start; index < len(lines) && strings.TrimSpace(lines[index]) != "" && len(options) < 9; index++ {
		match := claudeThemeRowRe.FindStringSubmatch(lines[index])
		if match == nil {
			return Prompt{}, false
		}
		selected := match[1] != ""
		cursor = cursor || selected
		options = append(options, PromptOption{Number: len(options) + 1, Label: collapse(match[2]), Selected: selected})
	}
	if len(options) < 2 || !cursor {
		return Prompt{}, false
	}
	parts := []string{"claude-theme"}
	for _, option := range options {
		parts = append(parts, option.Label)
	}
	return Prompt{
		ID:         PromptID(PromptStartup, parts...),
		Kind:       PromptStartup,
		Provider:   "claude",
		Title:      "Choose Claude Code's text style",
		Options:    options,
		Unnumbered: true,
	}, true
}

// claudeContinuePrompt: a screen that only says something and ends in "Press
// Enter to continue…" (the security notes, a sign-in that worked).
func claudeContinuePrompt(lines []string) (Prompt, bool) {
	last := lastLineStarting(lines, claudeContinueLine, 1)
	if last < 0 {
		return Prompt{}, false
	}
	from := last - 1
	for from >= 0 && !claudeBannerRe.MatchString(lines[from]) && !strings.HasPrefix(lines[from], "Welcome to Claude Code") && !strings.HasPrefix(strings.TrimLeft(lines[from], " "), "──") {
		from--
	}
	// The welcome banner's picture sits above the words: lines with no letter
	// in them are drawing, not something said.
	var said []string
	for _, line := range lines[from+1 : last] {
		if strings.IndexFunc(line, unicode.IsLetter) >= 0 {
			said = append(said, line)
		}
	}
	body := paragraph(said, 0, len(said))
	title := "Claude Code says"
	if heading, _, ok := strings.Cut(body, ":"); ok && len(heading) <= 40 {
		title = heading
	}
	return Prompt{
		ID:       PromptID(PromptStartup, "claude-continue", body),
		Kind:     PromptStartup,
		Provider: "claude",
		Title:    title,
		Request:  body,
		Options:  []PromptOption{{Number: 1, Label: "Continue", Key: "Enter", Selected: true}},
	}, true
}

// codexSetupPrompt reads the sign-in screens Codex shows before its first
// conversation: the choice of how to sign in, and the page and one-time code
// to sign in with on another device.
func codexSetupPrompt(pane string) (Prompt, bool) {
	if prompt, ok := codexTrustPrompt(pane); ok {
		return prompt, true
	}
	lines := setupLines(pane)
	welcome := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if strings.Contains(lines[index], "Welcome to Codex") {
			welcome = index
			break
		}
	}
	if welcome < 0 {
		return Prompt{}, false
	}
	lines = lines[welcome+1:]
	if prompt, ok := codexDevicePrompt(lines); ok {
		return prompt, true
	}
	return codexLoginPrompt(lines)
}

// codexDevicePrompt: "Finish signing in via your browser", the page to open,
// the one-time code to enter there, "Press esc to cancel". Codex goes on by
// itself once the person signed in.
func codexDevicePrompt(lines []string) (Prompt, bool) {
	cancel := lastLineStarting(lines, "Press esc to cancel", 1)
	if cancel < 0 {
		return Prompt{}, false
	}
	link, code := "", ""
	for _, line := range lines[:cancel] {
		text := strings.TrimSpace(line)
		switch {
		case link == "" && setupURLRe.MatchString(text):
			link = text
		case codexDeviceRe.MatchString(text):
			code = text
		}
	}
	if link == "" {
		return Prompt{}, false
	}
	return Prompt{
		ID:       PromptID(PromptSignIn, "codex", link, code),
		Kind:     PromptSignIn,
		Provider: "codex",
		Title:    "Sign in to Codex",
		Link:     link,
		Code:     code,
		Options:  []PromptOption{{Number: 1, Label: "Cancel", Key: "Escape"}},
	}, true
}

// codexLoginPrompt: how to sign in, a numbered menu whose rows carry an
// indented description, then "Press enter to continue". A typed digit does
// not pick a row there: the answer moves the cursor and presses Enter.
func codexLoginPrompt(lines []string) (Prompt, bool) {
	footer := lastLineStarting(lines, "Press enter to continue", 1)
	if footer < 0 {
		return Prompt{}, false
	}
	var options []PromptOption
	description := false
	first := -1
	for index, line := range lines[:footer] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if match := codexSetupRowRe.FindStringSubmatch(line); match != nil {
			if int(match[2][0]-'0') != len(options)+1 {
				return Prompt{}, false
			}
			if first < 0 {
				first = index
			}
			options = append(options, PromptOption{Number: len(options) + 1, Label: match[3], Selected: match[1] != ""})
			description = false
			continue
		}
		if len(options) == 0 {
			continue
		}
		last := &options[len(options)-1]
		indented := len(line)-len(strings.TrimLeft(line, " ")) >= 4
		switch {
		case indented:
			// A row's description, under it.
			if last.Description != "" {
				last.Description += " "
			}
			last.Description += strings.TrimSpace(line)
			description = true
		case description:
			last.Description += " " + strings.TrimSpace(line)
		default:
			last.Label += " " + strings.TrimSpace(line)
		}
	}
	if len(options) < 2 {
		return Prompt{}, false
	}
	selected := false
	parts := []string{"codex-login"}
	for index := range options {
		options[index].Label = collapse(options[index].Label)
		options[index].Description = collapse(options[index].Description)
		selected = selected || options[index].Selected
		parts = append(parts, options[index].Label)
	}
	if !selected {
		return Prompt{}, false
	}
	return Prompt{
		ID:         PromptID(PromptStartup, parts...),
		Kind:       PromptStartup,
		Provider:   "codex",
		Title:      "Sign in to Codex",
		Request:    paragraph(lines, 0, first),
		Options:    options,
		Unnumbered: true,
	}, true
}

// codexTrustPrompt: "Trust this folder?" for a folder Codex was not told to
// trust (a Session started as plain `codex`; Mewla's own launches pass the
// trust), under "Folder access" and the folder. A digit picks a row and goes on.
func codexTrustPrompt(pane string) (Prompt, bool) {
	lines := setupLines(pane)
	footer := lastLineStarting(lines, "enter continue", 1)
	if footer < 0 {
		return Prompt{}, false
	}
	question := -1
	for index := footer - 1; index >= 0; index-- {
		if strings.HasPrefix(strings.TrimSpace(lines[index]), "Trust this folder?") {
			question = index
			break
		}
	}
	if question < 0 {
		return Prompt{}, false
	}
	from := question
	if heading := lastLineStarting(lines[:question], "Folder access", 3); heading >= 0 {
		from = heading
	}
	var options []PromptOption
	first, selected := -1, false
	for index := question + 1; index < footer; index++ {
		match := codexTrustRowRe.FindStringSubmatch(lines[index])
		if match == nil {
			if len(options) > 0 && strings.TrimSpace(lines[index]) != "" {
				return Prompt{}, false
			}
			continue
		}
		if int(match[2][0]-'0') != len(options)+1 {
			return Prompt{}, false
		}
		if first < 0 {
			first = index
		}
		options = append(options, PromptOption{Number: len(options) + 1, Label: collapse(match[3]), Selected: match[1] != ""})
		selected = selected || match[1] != ""
	}
	if len(options) < 2 || !selected {
		return Prompt{}, false
	}
	parts := []string{"codex-trust", paragraph(lines, from, question)}
	for _, option := range options {
		parts = append(parts, option.Label)
	}
	return Prompt{
		ID:       PromptID(PromptStartup, parts...),
		Kind:     PromptStartup,
		Provider: "codex",
		Title:    "Trust this folder?",
		Request:  paragraph(lines, from, first),
		Options:  options,
	}, true
}
