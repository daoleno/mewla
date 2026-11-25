package work

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/procspawn"
	"github.com/daoleno/mewla/daemon/shellquote"
)

// ClaudeModel is one entry of Claude Code's own model list, as its model
// picker offers it with the person's sign-in.
type ClaudeModel struct {
	// Value is what Claude takes as --model ("default", "opus", "sonnet").
	Value string
	// DisplayName is the picker's label ("Default (recommended)", "Opus").
	DisplayName string
	// Resolved is the model id the value runs on ("claude-opus-5-5").
	Resolved string
	// Name is the model's name as Claude's own lines say it ("Opus 5.5"),
	// from the head of its picker description; "" for "default".
	Name string
	// Efforts are the effort levels the model takes, in Claude's order.
	Efforts []string
}

// ReadClaudeModels asks the Claude Code of a launch command for its model
// list: the `initialize` control request of its stream-json mode answers
// with the models of its picker, without a model call. The run saves no
// session, runs no hooks and loads no MCP server.
func ReadClaudeModels(ctx context.Context, command, cwd string, env []string) ([]ClaudeModel, error) {
	program := launchProgram(command)
	if program == "" {
		return nil, errors.New("no Claude Code program in the Session's command")
	}
	cmd := procspawn.CommandContext(ctx, "claude models", program, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--no-session-persistence", "--setting-sources", "user", "--settings", `{"disableAllHooks":true}`, "--strict-mcp-config")
	cmd.Dir = cwd
	cmd.Env = withLaunchEnv(env, command)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := io.WriteString(stdin, `{"type":"control_request","request_id":"mewla-models","request":{"subtype":"initialize"}}`+"\n"); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for scanner.Scan() {
		if models, ok, err := ParseClaudeInitialize(scanner.Bytes()); ok || err != nil {
			return models, err
		}
	}
	if reason := strings.TrimSpace(stderr.String()); reason != "" {
		return nil, fmt.Errorf("Claude Code: %s", reason)
	}
	return nil, errors.New("Claude Code did not answer with its models")
}

// withLaunchEnv applies the leading assignments of a launch command
// ("env CLAUDE_CONFIG_DIR=/dir claude", the way an executor with its own
// sign-in starts) to env, so the list is that sign-in's.
func withLaunchEnv(env []string, command string) []string {
	fields, ok := shellquote.Fields(command)
	if !ok {
		fields = strings.Fields(command)
	}
	if len(fields) > 0 && fields[0] == "env" {
		fields = fields[1:]
	}
	out := append([]string(nil), env...)
	for _, field := range fields {
		key, value, found := strings.Cut(field, "=")
		if !found || key == "" || strings.HasPrefix(key, "-") || strings.ContainsAny(key, "/ ") {
			break
		}
		kept := out[:0]
		for _, entry := range out {
			if !strings.HasPrefix(entry, key+"=") {
				kept = append(kept, entry)
			}
		}
		out = append(kept, key+"="+value)
	}
	return out
}

// ParseClaudeInitialize reads one stream-json line; ok is true when it is the
// answer to Mewla's initialize request.
func ParseClaudeInitialize(line []byte) (models []ClaudeModel, ok bool, err error) {
	var frame struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
			Response  struct {
				Models []struct {
					Value                 string   `json:"value"`
					DisplayName           string   `json:"displayName"`
					ResolvedModel         string   `json:"resolvedModel"`
					Description           string   `json:"description"`
					SupportsEffort        bool     `json:"supportsEffort"`
					SupportedEffortLevels []string `json:"supportedEffortLevels"`
				} `json:"models"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &frame) != nil || frame.Type != "control_response" || frame.Response.RequestID != "mewla-models" {
		return nil, false, nil
	}
	if frame.Response.Subtype != "success" {
		return nil, true, fmt.Errorf("Claude Code: %s", strings.TrimSpace(frame.Response.Error))
	}
	for _, entry := range frame.Response.Response.Models {
		value := strings.TrimSpace(entry.Value)
		if value == "" {
			continue
		}
		model := ClaudeModel{Value: value, DisplayName: strings.TrimSpace(entry.DisplayName), Resolved: strings.TrimSpace(entry.ResolvedModel)}
		if name, _, found := strings.Cut(entry.Description, " · "); found && value != "default" {
			model.Name = strings.TrimSpace(name)
		}
		if entry.SupportsEffort {
			model.Efforts = entry.SupportedEffortLevels
		}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, true, errors.New("Claude Code listed no models")
	}
	return models, true, nil
}

// ClaudeLaunchModel reads --model and --effort off a Claude launch command.
func ClaudeLaunchModel(command string) (model, effort string) {
	fields, ok := shellquote.Fields(command)
	if !ok {
		fields = strings.Fields(command)
	}
	for i := 0; i < len(fields); i++ {
		key, value, inline := strings.Cut(fields[i], "=")
		if key != "--model" && key != "--effort" {
			continue
		}
		if !inline {
			if i+1 >= len(fields) {
				break
			}
			i++
			value = fields[i]
		}
		if key == "--model" {
			model = value
		} else {
			effort = value
		}
	}
	return model, effort
}

// ClaudeTranscriptChoice is what a Claude transcript says the Session runs
// on: the model of its latest answer, or the model and effort of a later
// switch line ("Set model to `Sonnet 5.5` for this session only with `low`
// effort"), and when.
type ClaudeTranscriptChoice struct {
	// Resolved is the model id of the latest answer when no switch followed it.
	Resolved string
	// Named is the model name of a switch after the latest answer.
	Named string
	// Effort is the effort of the latest switch that named one.
	Effort string
	At     time.Time
}

var claudeSwitchLine = regexp.MustCompile("^Set model to `?([^`<]+?)`?( for this session only| and saved as your default for new sessions)?(?: with `?([a-z]+)`? effort)?$")

var claudeANSI = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// ClaudeSwitch is what Claude's /model answered, in its own words: "Set
// model to `Sonnet 5.5` for this session only with `low` effort", "Kept
// model as Sonnet 5.5".
type ClaudeSwitch struct {
	// Line is Claude's answer as plain text.
	Line string
	// Named is the model a switch named; "" when the answer is no switch.
	Named string
	// Effort is the effort the switch named, if any.
	Effort string
	// SessionOnly: the switch is for this session only (Mewla's), not saved
	// as the default for new sessions.
	SessionOnly bool
	At          time.Time
}

// ParseClaudeSwitch reads Claude's answer to a /model (its command output,
// with or without the <local-command-stdout> wrapper).
func ParseClaudeSwitch(text string) ClaudeSwitch {
	text = strings.TrimSpace(claudeANSI.ReplaceAllString(text, ""))
	for _, tag := range []string{"local-command-stderr", "local-command-stdout"} {
		text = strings.TrimSuffix(strings.TrimPrefix(text, "<"+tag+">"), "</"+tag+">")
	}
	text = strings.TrimSpace(text)
	out := ClaudeSwitch{Line: strings.ReplaceAll(text, "`", "")}
	if match := claudeSwitchLine.FindStringSubmatch(text); match != nil {
		out.Named, out.SessionOnly, out.Effort = strings.TrimSpace(match[1]), match[2] == " for this session only", match[3]
	}
	return out
}

// claudeModelRecords walks a transcript tail's /model answers in order: each
// <local-command-stdout> that follows a <command-name>/model</command-name>
// record, and each answer Claude gave (assistant) as itself.
func claudeModelRecords(tail []byte, visit func(kind string, at time.Time, model string, answer ClaudeSwitch)) {
	afterModel := false
	for _, line := range bytes.Split(tail, []byte("\n")) {
		isAnswer := bytes.Contains(line, []byte(`"assistant"`))
		if !isAnswer && !bytes.Contains(line, []byte("local-command-stdout")) && !bytes.Contains(line, []byte("command-name")) {
			continue
		}
		var entry struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Model   string          `json:"model"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		switch entry.Type {
		case "assistant":
			if m := strings.TrimSpace(entry.Message.Model); m != "" && m != "<synthetic>" {
				visit("answer", entry.Timestamp, m, ClaudeSwitch{})
			}
		case "user":
			var text string
			if json.Unmarshal(entry.Message.Content, &text) != nil {
				continue
			}
			text = strings.TrimSpace(text)
			switch {
			case strings.HasPrefix(text, "<command-name>"):
				afterModel = strings.HasPrefix(text, "<command-name>/model</command-name>")
			case strings.HasPrefix(text, "<local-command-stdout>"):
				answer := ParseClaudeSwitch(text)
				// A switch line names itself; Claude's other answers to
				// /model ("Kept model as …") only follow its command record.
				if afterModel || answer.Named != "" {
					answer.At = entry.Timestamp
					visit("model", entry.Timestamp, "", answer)
				}
				afterModel = false
			}
		}
	}
}

// ClaudeTranscriptState reads a Claude transcript tail for the model the
// Session runs on.
func ClaudeTranscriptState(tail []byte) ClaudeTranscriptChoice {
	var choice ClaudeTranscriptChoice
	claudeModelRecords(tail, func(kind string, at time.Time, model string, answer ClaudeSwitch) {
		switch {
		case kind == "answer":
			choice.Resolved, choice.Named, choice.At = model, "", at
		case answer.Named != "":
			choice.Resolved, choice.Named, choice.At = "", answer.Named, at
			if answer.Effort != "" {
				choice.Effort = answer.Effort
			}
		}
	})
	return choice
}

// ClaudeModelAnswerSince is Claude's latest answer to a /model written at or
// after since: how a switch Mewla made in Claude's picker turned out. ok is
// false while Claude has written none. The transcript, not the screen, is
// read: the screen keeps only its last lines, and after a few switches the
// earlier answers have scrolled away (0.4.29 held-pick loop).
func ClaudeModelAnswerSince(tail []byte, since time.Time) (answer ClaudeSwitch, ok bool) {
	claudeModelRecords(tail, func(kind string, at time.Time, _ string, got ClaudeSwitch) {
		if kind == "model" && !at.Before(since) {
			answer, ok = got, true
		}
	})
	return answer, ok
}

// ReadClaudeModelAnswerSince is ClaudeModelAnswerSince over the end of a
// transcript file.
func ReadClaudeModelAnswerSince(path string, since time.Time) (ClaudeSwitch, bool) {
	return ClaudeModelAnswerSince(readClaudeTail(path), since)
}

// ReadClaudeTranscriptState reads the end of a transcript file.
func ReadClaudeTranscriptState(path string) ClaudeTranscriptChoice {
	return ClaudeTranscriptState(readClaudeTail(path))
}

// readClaudeTail is the last 512 KiB of a transcript file (nil when it
// cannot be read).
func readClaudeTail(path string) []byte {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	const window = 512 << 10
	if info, err := file.Stat(); err == nil && info.Size() > window {
		_, _ = file.Seek(info.Size()-window, io.SeekStart)
	}
	tail, _ := io.ReadAll(io.LimitReader(file, window))
	return tail
}

// ClaudePicker is Claude Code's model picker as its screen shows it.
type ClaudePicker struct {
	// Options are the picker's labels in order.
	Options []string
	// Selected is the index of the highlighted option.
	Selected int
	// Effort is the effort level the picker shows ("" when the model has none).
	Effort string
	// SessionOnly is true when the picker offers its session-only key.
	SessionOnly bool
}

var (
	claudePickerOption = regexp.MustCompile(`^\s*(❯)?\s*(\d+)\.\s+(.+?)(?:\s+✔)?(?:\s{2,}.*)?$`)
	claudePickerEffort = regexp.MustCompile(`(?i)\b([a-z]+) effort\b(?: \(default\))?.*(?:←|to adjust)`)
)

// ParseClaudePicker reads the last model picker on a Claude Code screen.
func ParseClaudePicker(screen string) (ClaudePicker, bool) {
	lines := strings.Split(screen, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "Select model" {
			start = i
		}
	}
	if start < 0 {
		return ClaudePicker{}, false
	}
	picker := ClaudePicker{Selected: -1}
	for _, line := range lines[start+1:] {
		if match := claudePickerOption.FindStringSubmatch(line); match != nil {
			number, _ := strconv.Atoi(match[2])
			if number != len(picker.Options)+1 {
				continue
			}
			if match[1] != "" {
				picker.Selected = len(picker.Options)
			}
			picker.Options = append(picker.Options, strings.TrimSpace(match[3]))
			continue
		}
		if match := claudePickerEffort.FindStringSubmatch(line); match != nil {
			picker.Effort = strings.ToLower(match[1])
			continue
		}
		if strings.Contains(line, "use this session only") {
			picker.SessionOnly = true
		}
	}
	if len(picker.Options) == 0 || picker.Selected < 0 {
		return ClaudePicker{}, false
	}
	return picker, true
}

// ClaudePromptInput reads Claude Code's prompt on a screen: the text after
// "❯" on the line under a rule, through the rule under it (a long text wraps
// over several lines; its dimmed placeholder reads as ""). prompt is false when the screen shows no prompt (a
// picker or a question in its place, or Claude not started yet).
func ClaudePromptInput(screen string) (input string, prompt bool) {
	lines := strings.Split(screen, "\n")
	rule := func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "──") }
	for i := len(lines) - 1; i > 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "❯") || !rule(lines[i-1]) {
			continue
		}
		parts := []string{strings.TrimSpace(strings.TrimPrefix(line, "❯"))}
		closed := false
		for _, next := range lines[i+1:] {
			if rule(next) {
				closed = true
				break
			}
			parts = append(parts, strings.TrimSpace(next))
		}
		if !closed {
			return "", false
		}
		input := strings.TrimSpace(strings.Join(parts, " "))
		if claudePromptPlaceholder.MatchString(input) {
			input = ""
		}
		return input, true
	}
	return "", false
}

// claudePromptPlaceholder is the dimmed text Claude shows in an empty prompt
// (a suggestion on a new conversation, a hint after a provider error), the
// shapes watcher's claudeComposerRe accepts as an empty composer.
var claudePromptPlaceholder = regexp.MustCompile(`^(?:Try[\t ]+(?:"[^"\n]+"|“[^”\n]+”|'[^'\n]+')|Resend your message to try again|Once fixed, resend your message)$`)

// ClaudeTranscriptFor is the transcript of a Claude Worker the way its Chat
// finds it ("" when it has none yet).
func ClaudeTranscriptFor(worker classifier.Worker) string {
	candidate, ok, err := findClaudeTranscript(worker, time.Now())
	if err != nil || !ok {
		return ""
	}
	return candidate.Path
}
