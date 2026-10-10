package watcher

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ClaudeChoiceQuestion is the transcript-owned shape of one AskUserQuestion
// question. The server derives it from the provider transcript, never from
// the client.
type ClaudeChoiceQuestion struct {
	Question    string
	MultiSelect bool
	Options     []string
	AllowsOther bool
}

// ClaudeChoiceAnswer selects zero-based option indexes and/or Other text.
type ClaudeChoiceAnswer struct {
	Selected []int
	Other    string
}

// ErrClaudeChoiceRefused means no key was sent, or the keys sent so far left
// the prompt unsubmitted; the user can always finish in the Terminal.
var ErrClaudeChoiceRefused = errors.New("claude choice refused")

const (
	maxClaudeChoiceOtherRunes = 500
	claudeChoiceStepTimeout   = 3 * time.Second
	claudeChoicePollInterval  = 60 * time.Millisecond
)

type claudeChoicePane interface {
	capture() (string, error)
	sendKey(key string) error
	sendText(text string) error
}

// AnswerClaudeChoice drives the live AskUserQuestion prompt to exactly the
// given answers. Every key is preceded by a fresh capture that proves the
// prompt is still the expected one; any mismatch stops before the next key.
func (w *Watcher) AnswerClaudeChoice(sessionID string, questions []ClaudeChoiceQuestion, answers []ClaudeChoiceAnswer) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return refuseClaudeChoice("missing session id")
	}
	expected, err := ExpectedClaudeChoiceAnswers(questions, answers)
	if err != nil {
		return err
	}
	identity, known := w.targetForSession(sessionID)
	if !known {
		return refuseClaudeChoice("target provider could not be proven; no key was sent")
	}
	pane := &tmuxClaudeChoicePane{watcher: w, sessionID: sessionID, identity: identity}
	return w.sessionInputOwner().serialized(sessionID, func() error {
		return driveClaudeChoice(pane, questions, answers, expected, claudeChoiceStepTimeout)
	})
}

type tmuxClaudeChoicePane struct {
	watcher   *Watcher
	sessionID string
	identity  targetProcessIdentity
}

func (p *tmuxClaudeChoicePane) capture() (string, error) {
	out, err := tmuxCommand(p.watcher.socketPathFor(p.sessionID), "capture-pane", "-p", "-t", p.sessionID).Output()
	if err != nil {
		return "", fmt.Errorf("capture pane: %w", err)
	}
	return string(out), nil
}

func (p *tmuxClaudeChoicePane) sendKey(key string) error {
	if err := guardTargetIdentity(p.watcher.targetForSession, p.sessionID, p.identity); err != nil {
		return err
	}
	return tmuxCommand(p.watcher.socketPathFor(p.sessionID), "send-keys", "-t", p.sessionID, key).Run()
}

// sendText types exact bytes. -H avoids tmux argument parsing, which drops a
// trailing ';' and treats a leading '-' as a flag.
func (p *tmuxClaudeChoicePane) sendText(text string) error {
	if err := guardTargetIdentity(p.watcher.targetForSession, p.sessionID, p.identity); err != nil {
		return err
	}
	args := []string{"send-keys", "-t", p.sessionID, "-H"}
	for _, b := range []byte(text) {
		args = append(args, hex.EncodeToString([]byte{b}))
	}
	return tmuxCommand(p.watcher.socketPathFor(p.sessionID), args...).Run()
}

func refuseClaudeChoice(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClaudeChoiceRefused, fmt.Sprintf(format, args...))
}

// ExpectedClaudeChoiceAnswers validates the request and returns the answer
// string Claude records for each question (multi-select joins option order,
// Other last, with ", ").
func ExpectedClaudeChoiceAnswers(questions []ClaudeChoiceQuestion, answers []ClaudeChoiceAnswer) ([][]string, error) {
	if len(questions) == 0 || len(answers) != len(questions) {
		return nil, refuseClaudeChoice("answer every question")
	}
	expected := make([][]string, len(questions))
	for index, question := range questions {
		answer := answers[index]
		other := answer.Other
		if other != "" {
			if !question.AllowsOther {
				return nil, refuseClaudeChoice("question %d has no free-text option", index+1)
			}
			if err := validateClaudeChoiceOther(other); err != nil {
				return nil, err
			}
		}
		seen := map[int]bool{}
		for _, selected := range answer.Selected {
			if selected < 0 || selected >= len(question.Options) || seen[selected] {
				return nil, refuseClaudeChoice("question %d has an invalid option", index+1)
			}
			seen[selected] = true
		}
		count := len(answer.Selected)
		if other != "" {
			count++
		}
		if count == 0 || (!question.MultiSelect && count != 1) {
			return nil, refuseClaudeChoice("question %d needs exactly one answer", index+1)
		}
		var parts []string
		for option := range question.Options {
			if seen[option] {
				parts = append(parts, question.Options[option])
			}
		}
		if other != "" {
			parts = append(parts, other)
		}
		expected[index] = parts
	}
	return expected, nil
}

func validateClaudeChoiceOther(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) != text {
		return refuseClaudeChoice("other text must be trimmed UTF-8")
	}
	if utf8.RuneCountInString(text) > maxClaudeChoiceOtherRunes {
		return refuseClaudeChoice("other text is too long")
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return refuseClaudeChoice("other text must be a single line")
		}
	}
	return nil
}

// ClaudeChoiceAnswerMatches compares one recorded answer string with the
// intended parts. Claude quotes a multi-select item that contains quotes,
// commas or backslashes, so each item may appear raw or quoted.
func ClaudeChoiceAnswerMatches(recorded string, parts []string) bool {
	return claudeChoiceAnswerMatches(recorded, parts, func(value string) string { return value })
}

func claudeChoiceAnswerMatches(recorded string, parts []string, normalize func(string) string) bool {
	recorded = normalize(recorded)
	var build func(index int, prefix string) bool
	build = func(index int, prefix string) bool {
		if index == len(parts) {
			return normalize(prefix) == recorded
		}
		separator := ""
		if index > 0 {
			separator = ", "
		}
		raw := parts[index]
		quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(raw) + `"`
		return build(index+1, prefix+separator+raw) || build(index+1, prefix+separator+quoted)
	}
	return build(0, "")
}

type claudeChoiceDriver struct {
	pane    claudeChoicePane
	timeout time.Duration
	sent    bool
}

func driveClaudeChoice(pane claudeChoicePane, questions []ClaudeChoiceQuestion, answers []ClaudeChoiceAnswer, expected [][]string, timeout time.Duration) error {
	d := &claudeChoiceDriver{pane: pane, timeout: timeout}
	screen, err := d.read()
	if err != nil {
		return err
	}
	if err := verifyPristineClaudeChoice(screen, questions); err != nil {
		return err
	}
	for index, question := range questions {
		if index > 0 {
			if screen, err = d.waitFor(func(s claudeChoiceScreen) bool { return isClaudeChoiceQuestion(s, question) }); err != nil {
				return d.stopped("question %d did not appear", index+1)
			}
		}
		if question.MultiSelect {
			screen, err = d.answerMulti(screen, question, answers[index])
		} else {
			screen, err = d.answerSingle(screen, question, answers[index], len(questions) == 1)
		}
		if err != nil {
			return err
		}
	}
	if len(questions) == 1 && !questions[0].MultiSelect {
		// A lone single-select question submits on Enter; there is no review.
		_, err := d.waitFor(func(s claudeChoiceScreen) bool { return s.Kind == claudeChoiceScreenNone })
		if err != nil {
			return d.stopped("prompt did not close after the answer")
		}
		return nil
	}
	return d.submitReview(questions, expected)
}

func verifyPristineClaudeChoice(screen claudeChoiceScreen, questions []ClaudeChoiceQuestion) error {
	if !isClaudeChoiceQuestion(screen, questions[0]) {
		return refuseClaudeChoice("the Terminal is not showing this question")
	}
	if len(screen.Tabs) != len(questions) {
		return refuseClaudeChoice("the Terminal prompt has %d questions, expected %d", len(screen.Tabs), len(questions))
	}
	for _, answered := range screen.TabsAnswered {
		if answered {
			return refuseClaudeChoice("the prompt was partly answered in the Terminal; finish it there")
		}
	}
	for _, row := range screen.Rows {
		if row.Checked {
			return refuseClaudeChoice("the prompt was partly answered in the Terminal; finish it there")
		}
	}
	return nil
}

// isClaudeChoiceQuestion proves the screen shows this question with these options.
func isClaudeChoiceQuestion(screen claudeChoiceScreen, question ClaudeChoiceQuestion) bool {
	if screen.Kind != claudeChoiceScreenQuestion || screen.Question != claudeChoiceCompact(question.Question) {
		return false
	}
	rows := len(question.Options)
	if question.AllowsOther {
		rows++
	}
	if len(screen.Rows) != rows || screen.SubmitRow != question.MultiSelect {
		return false
	}
	for index, label := range question.Options {
		if !claudeChoiceRowShowsLabel(screen.Rows[index], label) {
			return false
		}
		if screen.Rows[index].HasBox != question.MultiSelect {
			return false
		}
	}
	return true
}

func claudeChoiceRowShowsLabel(row claudeChoiceRow, label string) bool {
	text := claudeChoiceCompact(row.Text)
	return text != "" && strings.HasPrefix(claudeChoiceCompact(label), text)
}

func (d *claudeChoiceDriver) answerSingle(screen claudeChoiceScreen, question ClaudeChoiceQuestion, answer ClaudeChoiceAnswer, final bool) (claudeChoiceScreen, error) {
	target := len(question.Options) + 1
	if answer.Other == "" {
		target = answer.Selected[0] + 1
	}
	screen, err := d.moveTo(screen, question, target)
	if err != nil {
		return screen, err
	}
	if answer.Other != "" {
		if screen, err = d.typeOther(screen, question, target, answer.Other); err != nil {
			return screen, err
		}
	}
	// Last proof before the selecting Enter (the submission for a lone question).
	if !isClaudeChoiceQuestion(screen, question) || screen.cursorRow() != target {
		return screen, d.stopped("the prompt changed before selection")
	}
	if answer.Other == "" && !claudeChoiceRowShowsLabel(screen.Rows[target-1], question.Options[target-1]) {
		return screen, d.stopped("the selected row no longer shows %q", question.Options[target-1])
	}
	if err := d.key("Enter"); err != nil {
		return screen, err
	}
	if final {
		return screen, nil
	}
	return d.waitFor(func(s claudeChoiceScreen) bool { return !isClaudeChoiceQuestion(s, question) })
}

func (d *claudeChoiceDriver) answerMulti(screen claudeChoiceScreen, question ClaudeChoiceQuestion, answer ClaudeChoiceAnswer) (claudeChoiceScreen, error) {
	want := map[int]bool{}
	for _, selected := range answer.Selected {
		want[selected+1] = true
	}
	var err error
	for number := 1; number <= len(question.Options); number++ {
		if !want[number] {
			continue
		}
		if screen, err = d.moveTo(screen, question, number); err != nil {
			return screen, err
		}
		row, _ := screen.row(number)
		if row.Checked {
			continue
		}
		if !claudeChoiceRowShowsLabel(row, question.Options[number-1]) {
			return screen, d.stopped("row %d no longer shows %q", number, question.Options[number-1])
		}
		if err := d.key("Enter"); err != nil {
			return screen, err
		}
		if screen, err = d.waitFor(func(s claudeChoiceScreen) bool {
			row, ok := s.row(number)
			return isClaudeChoiceQuestion(s, question) && ok && row.Checked && row.Cursor
		}); err != nil {
			return screen, d.stopped("option %q did not toggle on", question.Options[number-1])
		}
	}
	if answer.Other != "" {
		target := len(question.Options) + 1
		if screen, err = d.moveTo(screen, question, target); err != nil {
			return screen, err
		}
		if screen, err = d.typeOther(screen, question, target, answer.Other); err != nil {
			return screen, err
		}
	}
	if screen, err = d.moveToSubmit(screen, question); err != nil {
		return screen, err
	}
	// Last proof before Submit: exactly the intended rows are checked.
	for number, row := range screen.Rows {
		wanted := want[number+1] || (answer.Other != "" && number+1 == len(question.Options)+1)
		if row.Checked != wanted {
			return screen, d.stopped("checked options do not match the answer")
		}
	}
	if err := d.key("Enter"); err != nil {
		return screen, err
	}
	return d.waitFor(func(s claudeChoiceScreen) bool { return !isClaudeChoiceQuestion(s, question) })
}

func (d *claudeChoiceDriver) typeOther(screen claudeChoiceScreen, question ClaudeChoiceQuestion, target int, text string) (claudeChoiceScreen, error) {
	row, _ := screen.row(target)
	if row.Cursor && claudeChoiceCompact(row.Full) == claudeChoiceCompact(text) && (!row.HasBox || row.Checked) {
		return screen, nil
	}
	if !strings.HasPrefix(claudeChoiceCompact(row.Full), "Typesomething") {
		return screen, d.stopped("the free-text row already holds text")
	}
	if err := d.pane.sendText(text); err != nil {
		return screen, err
	}
	d.sent = true
	return d.waitForStable(func(s claudeChoiceScreen) bool {
		row, ok := s.row(target)
		return isClaudeChoiceQuestion(s, question) && ok && row.Cursor &&
			claudeChoiceCompact(row.Full) == claudeChoiceCompact(text) &&
			(!row.HasBox || row.Checked)
	})
}

// moveTo walks the cursor one verified row at a time. It never assumes that
// Up/Down stop or wrap at the ends.
func (d *claudeChoiceDriver) moveTo(screen claudeChoiceScreen, question ClaudeChoiceQuestion, target int) (claudeChoiceScreen, error) {
	for steps := 0; ; steps++ {
		if !isClaudeChoiceQuestion(screen, question) {
			return screen, d.stopped("the prompt changed while moving")
		}
		current := screen.cursorRow()
		if current == target && !screen.SubmitCursor {
			return screen, nil
		}
		if steps > len(screen.Rows)+2 {
			return screen, d.stopped("could not reach option %d", target)
		}
		key, next := "Down", current+1
		if screen.SubmitCursor {
			key, next = "Up", len(screen.Rows)
		} else if current == 0 {
			return screen, d.stopped("no cursor on the prompt")
		} else if current > target {
			key, next = "Up", current-1
		}
		if err := d.key(key); err != nil {
			return screen, err
		}
		var err error
		if screen, err = d.waitFor(func(s claudeChoiceScreen) bool { return !s.SubmitCursor && s.cursorRow() == next }); err != nil {
			return screen, d.stopped("cursor did not move to option %d", next)
		}
	}
}

func (d *claudeChoiceDriver) moveToSubmit(screen claudeChoiceScreen, question ClaudeChoiceQuestion) (claudeChoiceScreen, error) {
	screen, err := d.moveTo(screen, question, len(screen.Rows))
	if err != nil {
		return screen, err
	}
	if err := d.key("Down"); err != nil {
		return screen, err
	}
	if screen, err = d.waitFor(func(s claudeChoiceScreen) bool { return isClaudeChoiceQuestion(s, question) && s.SubmitCursor }); err != nil {
		return screen, d.stopped("cursor did not reach Submit")
	}
	return screen, nil
}

func (d *claudeChoiceDriver) submitReview(questions []ClaudeChoiceQuestion, expected [][]string) error {
	screen, err := d.waitFor(func(s claudeChoiceScreen) bool { return s.Kind == claudeChoiceScreenReview })
	if err != nil {
		return d.stopped("the review screen did not appear")
	}
	if len(screen.Review) != len(questions) {
		return d.stopped("the review lists %d answers, expected %d", len(screen.Review), len(questions))
	}
	for index, item := range screen.Review {
		if claudeChoiceCompact(item.Question) != claudeChoiceCompact(questions[index].Question) ||
			!claudeChoiceAnswerMatches(item.Answer, expected[index], claudeChoiceCompact) {
			return d.stopped("the review shows %q for question %d; not submitted", item.Answer, index+1)
		}
	}
	if len(screen.ReviewRows) < 1 || screen.ReviewRows[0].Text != "Submit answers" || !screen.ReviewRows[0].Cursor {
		return d.stopped("Submit answers is not selected; not submitted")
	}
	if err := d.key("Enter"); err != nil {
		return err
	}
	if _, err := d.waitFor(func(s claudeChoiceScreen) bool { return s.Kind == claudeChoiceScreenNone }); err != nil {
		return d.stopped("prompt did not close after submit")
	}
	return nil
}

func (d *claudeChoiceDriver) key(key string) error {
	if err := d.pane.sendKey(key); err != nil {
		return err
	}
	d.sent = true
	return nil
}

func (d *claudeChoiceDriver) read() (claudeChoiceScreen, error) {
	pane, err := d.pane.capture()
	if err != nil {
		return claudeChoiceScreen{}, err
	}
	return parseClaudeChoiceScreen(pane), nil
}

func (d *claudeChoiceDriver) waitFor(ready func(claudeChoiceScreen) bool) (claudeChoiceScreen, error) {
	deadline := time.Now().Add(d.timeout)
	for {
		screen, err := d.read()
		if err != nil {
			return screen, err
		}
		if ready(screen) {
			return screen, nil
		}
		if !time.Now().Before(deadline) {
			return screen, errClaudeChoiceTimeout
		}
		time.Sleep(claudeChoicePollInterval)
	}
}

// waitForStable requires two consecutive matching captures so typed text has
// fully landed before the next key.
func (d *claudeChoiceDriver) waitForStable(ready func(claudeChoiceScreen) bool) (claudeChoiceScreen, error) {
	if _, err := d.waitFor(ready); err != nil {
		return claudeChoiceScreen{}, d.stopped("typed text did not appear exactly")
	}
	time.Sleep(claudeChoicePollInterval)
	screen, err := d.waitFor(ready)
	if err != nil {
		return screen, d.stopped("typed text did not appear exactly")
	}
	return screen, nil
}

var errClaudeChoiceTimeout = errors.New("timed out")

func (d *claudeChoiceDriver) stopped(format string, args ...any) error {
	reason := fmt.Sprintf(format, args...)
	if d.sent {
		reason += "; the prompt is still open in the Terminal"
	}
	return refuseClaudeChoice("%s", reason)
}
