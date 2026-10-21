package watcher

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrMessageRewindRefused marks a rewind the daemon declined before changing
// anything it could not undo. Callers may show the wrapped reason to the user.
var ErrMessageRewindRefused = errors.New("message rewind refused")

// ErrMessageRewindInputNotClean marks the one failure that comes after the CLI
// has already stepped back: the conversation is rewound, but the agent's input
// box could not be proven empty. Nothing may be sent into it until the person
// clears it, so this is reported apart from a refusal, which changed nothing.
var ErrMessageRewindInputNotClean = errors.New("rewound, but the agent's input box is not empty")

// Providers whose own CLI can step a conversation back to an earlier prompt.
const (
	MessageRewindClaude = "claude"
	MessageRewindCodex  = "codex"
	MessageRewindPi     = "pi"
)

// MessageRewindRequest names the prompt to step back to. Text is what the
// transcript shows for it and Back counts the human prompts after it, so the
// driver can reach it with the CLI's own navigation and prove it got there.
type MessageRewindRequest struct {
	Provider string
	Text     string
	Back     int
}

const (
	messageRewindStepTimeout  = 4 * time.Second
	messageRewindPollInterval = 60 * time.Millisecond
	messageRewindEscapeGap    = 180 * time.Millisecond
	// A prefilled draft is deleted with a few spare backspaces: extra ones on
	// an empty composer do nothing in any supported CLI.
	messageRewindClearSpare = 8
)

type rewindPane interface {
	capture(styled bool) (string, error)
	sendKey(key string) error
	repeatKey(key string, count int) error
}

// RewindMessage steps the live CLI back to an earlier prompt and leaves its
// composer empty. The conversation is rewound; files on disk are not touched.
// Every key is preceded by a capture proving the CLI is on the expected
// screen, so a mismatch stops before anything further is pressed.
func (w *Watcher) RewindMessage(sessionID string, request MessageRewindRequest) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return refuseMessageRewind("missing session id")
	}
	if strings.TrimSpace(request.Text) == "" || request.Back < 0 {
		return refuseMessageRewind("missing message")
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return refuseMessageRewind("target provider could not be proven; no key was sent")
	}
	pane := &tmuxRewindPane{watcher: w, sessionID: sessionID, identity: identity}
	return w.sessionInputOwner().serialized(sessionID, func() error {
		return driveMessageRewind(pane, request, messageRewindStepTimeout)
	})
}

type tmuxRewindPane struct {
	watcher   *Watcher
	sessionID string
	identity  targetProcessIdentity
}

func (p *tmuxRewindPane) capture(styled bool) (string, error) {
	args := []string{"capture-pane", "-p", "-t", p.sessionID}
	if styled {
		args = []string{"capture-pane", "-p", "-e", "-t", p.sessionID}
	}
	out, err := tmuxCommand(p.watcher.socketPathFor(p.sessionID), args...).Output()
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}
	return string(out), nil
}

func (p *tmuxRewindPane) sendKey(key string) error {
	if err := guardTargetIdentity(p.watcher.targetForSession, p.sessionID, p.identity); err != nil {
		return err
	}
	return tmuxCommand(p.watcher.socketPathFor(p.sessionID), "send-keys", "-t", p.sessionID, key).Run()
}

func (p *tmuxRewindPane) repeatKey(key string, count int) error {
	if err := guardTargetIdentity(p.watcher.targetForSession, p.sessionID, p.identity); err != nil {
		return err
	}
	return tmuxCommand(p.watcher.socketPathFor(p.sessionID), "send-keys", "-t", p.sessionID, "-N", fmt.Sprint(count), key).Run()
}

func refuseMessageRewind(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMessageRewindRefused, fmt.Sprintf(format, args...))
}

func driveMessageRewind(pane rewindPane, request MessageRewindRequest, timeout time.Duration) error {
	d := &rewindDriver{pane: pane, timeout: timeout, text: request.Text}
	switch request.Provider {
	case MessageRewindClaude:
		return d.claude(request)
	case MessageRewindCodex:
		return d.codex(request)
	case MessageRewindPi:
		return d.pi(request)
	default:
		return refuseMessageRewind("this agent cannot rewind a conversation")
	}
}

type rewindDriver struct {
	pane    rewindPane
	timeout time.Duration
	text    string
	sent    bool
}

func (d *rewindDriver) key(key string) error {
	if err := d.pane.sendKey(key); err != nil {
		return err
	}
	d.sent = true
	return nil
}

// wait polls until ready accepts a capture, or the step times out.
func (d *rewindDriver) wait(styled bool, ready func(string) bool) (string, error) {
	deadline := time.Now().Add(d.timeout)
	for {
		screen, err := d.pane.capture(styled)
		if err != nil {
			return "", err
		}
		if ready(screen) {
			return screen, nil
		}
		if !time.Now().Before(deadline) {
			return screen, errClaudeChoiceTimeout
		}
		time.Sleep(messageRewindPollInterval)
	}
}

// stopped reports a refusal; when keys were already pressed it first leaves the
// CLI's rewind screen with cancel so the user is not stranded in it.
func (d *rewindDriver) stopped(cancelKeys int, format string, args ...any) error {
	reason := fmt.Sprintf(format, args...)
	for i := 0; i < cancelKeys; i++ {
		_ = d.pane.sendKey("Escape")
		time.Sleep(messageRewindEscapeGap)
	}
	if d.sent {
		reason += "; nothing was rewound"
	}
	return refuseMessageRewind("%s", reason)
}

// doubleEscape is the CLIs' own gesture for "edit a previous message".
func (d *rewindDriver) doubleEscape() error {
	if err := d.key("Escape"); err != nil {
		return err
	}
	time.Sleep(messageRewindEscapeGap)
	return d.key("Escape")
}

// finishRewind runs once the CLI's own confirm key has been pressed: the
// conversation is rewound whatever happens next. It waits for the CLI's input
// box, deletes the prompt the CLI prefilled and proves the box is empty.
// Deleting by count keeps a multi-line draft from leaving lines behind; spare
// backspaces on an empty box do nothing in any supported CLI.
func (d *rewindDriver) finishRewind(backAtInput, composerEmpty func(string) bool, styled bool) error {
	notClean := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrMessageRewindInputNotClean, fmt.Sprintf(format, args...))
	}
	if _, err := d.wait(styled, backAtInput); err != nil {
		return notClean("the agent did not return to its input box")
	}
	count := utf8.RuneCountInString(d.text) + messageRewindClearSpare
	if err := d.pane.repeatKey("BSpace", count); err != nil {
		return notClean("%v", err)
	}
	if _, err := d.wait(styled, composerEmpty); err != nil {
		return notClean("it still holds text")
	}
	return nil
}

// ---- Claude Code ----

const claudeRestoreConversationLabel = "Restore conversation"

type claudeRewindScreen struct {
	Picker      bool
	Confirm     bool
	Selected    string // highlighted picker row, or highlighted confirm option
	ConfirmText string // the prompt quoted by the confirm screen
	Composer    string // text after the composer's prompt mark
	HasComposer bool
	Running     bool
}

var claudeConfirmOption = regexp.MustCompile(`^\s*(❯)?\s*\d+\.\s+(.+?)\s*$`)

func parseClaudeRewindScreen(screen string) claudeRewindScreen {
	lines := strings.Split(screen, "\n")
	var out claudeRewindScreen
	headerAt := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Rewind" {
			headerAt = index
		}
		if strings.Contains(line, "esc to interrupt") {
			out.Running = true
		}
	}
	if headerAt >= 0 {
		for index := headerAt + 1; index < len(lines); index++ {
			trimmed := strings.TrimSpace(lines[index])
			switch {
			case strings.HasPrefix(trimmed, "Restore the code and/or conversation"):
				out.Picker = true
			case strings.HasPrefix(trimmed, "Confirm you want to restore"):
				out.Confirm = true
			}
		}
		if out.Picker {
			for index := headerAt + 1; index < len(lines); index++ {
				trimmed := strings.TrimLeft(lines[index], " ")
				if strings.HasPrefix(trimmed, "❯ ") {
					out.Selected = strings.TrimSpace(strings.TrimPrefix(trimmed, "❯ "))
					break
				}
			}
		}
		if out.Confirm {
			var quoted []string
			for index := headerAt + 1; index < len(lines); index++ {
				trimmed := strings.TrimSpace(lines[index])
				if strings.HasPrefix(trimmed, "│") {
					body := strings.TrimSpace(strings.TrimPrefix(trimmed, "│"))
					if !strings.HasPrefix(body, "(") || !strings.HasSuffix(body, "ago)") {
						quoted = append(quoted, body)
					}
					continue
				}
				if match := claudeConfirmOption.FindStringSubmatch(lines[index]); match != nil && match[1] != "" {
					out.Selected = match[2]
				}
			}
			out.ConfirmText = strings.Join(quoted, " ")
		}
		return out
	}
	for index := len(lines) - 1; index >= 0; index-- {
		trimmed := strings.TrimLeft(lines[index], " ")
		if strings.HasPrefix(trimmed, "❯") {
			out.HasComposer = true
			out.Composer = strings.TrimSpace(strings.TrimPrefix(trimmed, "❯"))
			break
		}
	}
	return out
}

// claudeComposerEmpty accepts the start-up placeholder, which is dimmed text.
func claudeComposerEmpty(screen claudeRewindScreen) bool {
	return screen.HasComposer && !screen.Picker && !screen.Confirm &&
		(screen.Composer == "" || strings.HasPrefix(screen.Composer, `Try "`))
}

func (d *rewindDriver) claude(request MessageRewindRequest) error {
	raw, err := d.pane.capture(false)
	if err != nil {
		return err
	}
	screen := parseClaudeRewindScreen(raw)
	if screen.Running {
		return d.stopped(0, "Claude is working; wait for it to finish")
	}
	if !claudeComposerEmpty(screen) {
		return d.stopped(0, "Claude's input box is not empty or is showing another screen")
	}
	if err := d.doubleEscape(); err != nil {
		return err
	}
	if raw, err = d.wait(false, func(s string) bool { return parseClaudeRewindScreen(s).Picker }); err != nil {
		return d.stopped(1, "Claude did not open its message list")
	}
	// The list opens on "(current)"; each Up steps one prompt back.
	if parseClaudeRewindScreen(raw).Selected != "(current)" {
		return d.stopped(1, "Claude's message list did not start at the latest message")
	}
	for step := 0; step <= request.Back; step++ {
		if err := d.key("Up"); err != nil {
			return err
		}
		time.Sleep(messageRewindPollInterval)
	}
	want := request.Text
	raw, err = d.wait(false, func(s string) bool {
		picked := parseClaudeRewindScreen(s)
		return picked.Picker && picked.Selected != "(current)" && rewindTextMatches(picked.Selected, want)
	})
	if err != nil {
		return d.stopped(1, "Claude highlighted a different message")
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	raw, err = d.wait(false, func(s string) bool {
		picked := parseClaudeRewindScreen(s)
		return picked.Confirm && rewindTextMatches(picked.ConfirmText, want)
	})
	if err != nil {
		return d.stopped(2, "Claude asked about a different message")
	}
	// Choose the conversation-only restore by its label; the option order
	// changes when Claude also offers to restore code.
	for attempt := 0; parseClaudeRewindScreen(raw).Selected != claudeRestoreConversationLabel; attempt++ {
		if attempt >= 6 {
			return d.stopped(2, "Claude did not offer %q", claudeRestoreConversationLabel)
		}
		if err := d.key("Down"); err != nil {
			return err
		}
		time.Sleep(messageRewindPollInterval)
		if raw, err = d.pane.capture(false); err != nil {
			return err
		}
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	return d.finishRewind(
		func(s string) bool {
			picked := parseClaudeRewindScreen(s)
			return picked.HasComposer && !picked.Picker && !picked.Confirm
		},
		func(s string) bool { return claudeComposerEmpty(parseClaudeRewindScreen(s)) },
		false,
	)
}

// ---- Codex ----

type codexRewindScreen struct {
	Pager       bool
	Selected    string // reverse-video prompt in the transcript pager
	HasComposer bool
	Composer    string
	Running     bool
}

var (
	ansiSequence     = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	codexSelectedRow = regexp.MustCompile(`\x1b\[0;7m(.*?)\x1b\[0m`)
)

const codexComposerPlaceholder = "Ask Codex to do anything"

func parseCodexRewindScreen(styled string) codexRewindScreen {
	var out codexRewindScreen
	lines := strings.Split(styled, "\n")
	for _, line := range lines {
		plain := ansiSequence.ReplaceAllString(line, "")
		// A narrow pane shortens the status line to "Browsing · ↑↓/jk ←→/hl …".
		if strings.HasPrefix(strings.TrimSpace(plain), "Browsing") {
			out.Pager = true
		}
		if strings.Contains(plain, "esc to interrupt") {
			out.Running = true
		}
	}
	if out.Pager {
		for _, line := range lines {
			if match := codexSelectedRow.FindStringSubmatch(line); match != nil && strings.Contains(ansiSequence.ReplaceAllString(line, ""), "›") {
				out.Selected = strings.TrimSpace(ansiSequence.ReplaceAllString(match[1], ""))
			}
		}
	}
	// The composer is the last "›" line that is not the pager's transcript
	// prompts, which are followed by the status line.
	for index := len(lines) - 1; index >= 0; index-- {
		plain := strings.TrimSpace(ansiSequence.ReplaceAllString(lines[index], ""))
		if strings.HasPrefix(plain, "›") {
			out.HasComposer = true
			out.Composer = strings.TrimSpace(strings.TrimPrefix(plain, "›"))
			break
		}
	}
	return out
}

func codexComposerEmpty(screen codexRewindScreen) bool {
	return screen.HasComposer && !screen.Pager &&
		(screen.Composer == "" || screen.Composer == codexComposerPlaceholder)
}

func (d *rewindDriver) codex(request MessageRewindRequest) error {
	raw, err := d.pane.capture(true)
	if err != nil {
		return err
	}
	screen := parseCodexRewindScreen(raw)
	if screen.Running {
		return d.stopped(0, "Codex is working; wait for it to finish")
	}
	if !codexComposerEmpty(screen) {
		return d.stopped(0, "Codex's input box is not empty or is showing another screen")
	}
	if err := d.doubleEscape(); err != nil {
		return err
	}
	if raw, err = d.wait(true, func(s string) bool { return parseCodexRewindScreen(s).Pager }); err != nil {
		return d.stopped(1, "Codex did not open its message browser")
	}
	// The browser opens on the latest prompt; Left steps one prompt back.
	for step := 0; step < request.Back; step++ {
		if err := d.key("Left"); err != nil {
			return err
		}
		time.Sleep(messageRewindPollInterval)
	}
	want := request.Text
	if _, err = d.wait(true, func(s string) bool {
		picked := parseCodexRewindScreen(s)
		return picked.Pager && rewindTextMatches(picked.Selected, want)
	}); err != nil {
		return d.stopped(1, "Codex highlighted a different message")
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	return d.finishRewind(
		func(s string) bool {
			picked := parseCodexRewindScreen(s)
			return picked.HasComposer && !picked.Pager
		},
		func(s string) bool { return codexComposerEmpty(parseCodexRewindScreen(s)) },
		true,
	)
}

// ---- pi ----

type piRewindScreen struct {
	Tree          bool
	UserFilter    bool
	Selected      string // highlighted user row
	Summarize     bool
	SummarizePick string
	Composer      string
	HasComposer   bool
}

// The "•" marks a row on the active branch; only those may be chosen.
var piUserRow = regexp.MustCompile(`^›\s*[├└│─ ]*•\s*user:\s*(.*)$`)

func parsePiRewindScreen(screen string) piRewindScreen {
	var out piRewindScreen
	lines := strings.Split(screen, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Session Tree":
			out.Tree = true
		case strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, "[user]"):
			out.UserFilter = true
		case trimmed == "Summarize branch?":
			out.Summarize = true
		}
	}
	if out.Tree {
		for _, line := range lines {
			if match := piUserRow.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				out.Selected = strings.TrimSpace(match[1])
			}
		}
	}
	if out.Summarize {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "→ ") {
				out.SummarizePick = strings.TrimSpace(strings.TrimPrefix(trimmed, "→ "))
			}
		}
	}
	// The editor sits between the last two rules, above the working directory.
	var rules []int
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "────────") {
			rules = append(rules, index)
		}
	}
	if !out.Tree && !out.Summarize && len(rules) >= 2 {
		last := len(rules) - 1
		var body []string
		for _, line := range lines[rules[last-1]+1 : rules[last]] {
			body = append(body, strings.TrimSpace(line))
		}
		out.HasComposer = true
		out.Composer = strings.TrimSpace(strings.Join(body, " "))
	}
	return out
}

func (d *rewindDriver) pi(request MessageRewindRequest) error {
	raw, err := d.pane.capture(false)
	if err != nil {
		return err
	}
	screen := parsePiRewindScreen(raw)
	if !screen.HasComposer || screen.Composer != "" {
		return d.stopped(0, "pi's input box is not empty or is showing another screen")
	}
	if err := d.doubleEscape(); err != nil {
		return err
	}
	if _, err = d.wait(false, func(s string) bool { return parsePiRewindScreen(s).Tree }); err != nil {
		return d.stopped(1, "pi did not open its session tree")
	}
	// Show only the user's prompts so each Up is one prompt; the tree opens on
	// the latest one.
	if err := d.pane.sendKey("C-u"); err != nil {
		return err
	}
	d.sent = true
	if _, err = d.wait(false, func(s string) bool {
		picked := parsePiRewindScreen(s)
		return picked.Tree && picked.UserFilter && picked.Selected != ""
	}); err != nil {
		return d.stopped(1, "pi did not filter the tree to your messages")
	}
	for step := 0; step < request.Back; step++ {
		if err := d.key("Up"); err != nil {
			return err
		}
		time.Sleep(messageRewindPollInterval)
	}
	want := request.Text
	if _, err = d.wait(false, func(s string) bool {
		picked := parsePiRewindScreen(s)
		return picked.Tree && picked.UserFilter && rewindTextMatches(picked.Selected, want)
	}); err != nil {
		return d.stopped(1, "pi highlighted a different message")
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	if _, err = d.wait(false, func(s string) bool { return parsePiRewindScreen(s).Summarize }); err != nil {
		return d.stopped(1, "pi did not ask how to continue from that message")
	}
	if pick := parsePiRewindScreen(mustCapture(d.pane)).SummarizePick; pick != "No summary" {
		return d.stopped(1, "pi would summarize the abandoned branch")
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	return d.finishRewind(
		func(s string) bool { return parsePiRewindScreen(s).HasComposer },
		func(s string) bool {
			picked := parsePiRewindScreen(s)
			return picked.HasComposer && picked.Composer == ""
		},
		false,
	)
}

func mustCapture(pane rewindPane) string {
	screen, _ := pane.capture(false)
	return screen
}

// ---- shared ----

// rewindTextMatches compares what a CLI shows for a prompt with the transcript
// text. A CLI shortens long prompts and shows only the first line, never more
// than the transcript holds, so the shown text must be a whitespace-normalised
// prefix of it; an empty side never matches.
func rewindTextMatches(shown, want string) bool {
	shown = normalizeRewindText(shown)
	want = normalizeRewindText(want)
	if shown == "" || want == "" {
		return false
	}
	shown = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(shown, "…"), "..."))
	if shown == "" {
		return false
	}
	return strings.HasPrefix(want, shown)
}

func normalizeRewindText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
