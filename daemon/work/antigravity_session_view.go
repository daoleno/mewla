package work

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/term"
)

// antigravityView draws an Antigravity Session in its terminal: the
// conversation as it happens, a composer line, and agy's questions with their
// numbered answers. It is the Session's Terminal; the app's Chat reads the log.
type antigravityView struct {
	mu  sync.Mutex
	out io.Writer
	// draft is what the person is typing; drawn after every output.
	draft []byte
	// text holds agent text not yet ended by a newline, by kind.
	text     string
	textKind string
	thinking bool
	tools    map[string]string
	asking   *AntigravityAsk
}

func newAntigravityView(out io.Writer) *antigravityView {
	return &antigravityView{out: out, tools: map[string]string{}}
}

const (
	viewDim   = "\x1b[2m"
	viewBold  = "\x1b[1m"
	viewReset = "\x1b[0m"
	viewClear = "\r\x1b[2K"
)

// emit clears the composer, writes lines and draws the composer again. The
// caller holds v.mu.
func (v *antigravityView) emit(lines ...string) {
	var b strings.Builder
	b.WriteString(viewClear)
	for _, line := range lines {
		b.WriteString(strings.ReplaceAll(line, "\n", "\r\n"))
		b.WriteString("\r\n")
	}
	b.WriteString(v.prompt())
	_, _ = io.WriteString(v.out, b.String())
}

func (v *antigravityView) prompt() string {
	if v.asking != nil && len(v.draft) == 0 {
		return viewBold + "? " + viewReset
	}
	return viewBold + "› " + viewReset + string(v.draft)
}

// flushText ends agent text that has no newline yet. The caller holds v.mu.
func (v *antigravityView) flushText() []string {
	if v.text == "" {
		return nil
	}
	text := v.text
	v.text, v.textKind = "", ""
	return []string{"  " + strings.ReplaceAll(text, "\n", "\n  ")}
}

func (v *antigravityView) header(cwd, model string, resumed bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	line := viewBold + "Antigravity" + viewReset
	if model != "" {
		line += " · " + model
	}
	if cwd != "" {
		line += viewDim + " · " + cwd + viewReset
	}
	lines := []string{line}
	if resumed {
		lines = append(lines, viewDim+"The earlier conversation continues."+viewReset)
	}
	v.emit(lines...)
	v.statusLocked("idle", "")
}

func (v *antigravityView) user(text string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	lines := v.flushText()
	v.thinking = false
	lines = append(lines, "", viewBold+"› "+viewReset+strings.ReplaceAll(text, "\n", "\n  "))
	v.emit(lines...)
}

func (v *antigravityView) note(text string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	lines := v.flushText()
	v.emit(append(lines, viewDim+text+viewReset)...)
}

// status reports the program's state to the terminal (OSC 7501), as agy's
// own terminal UI does, so the Session list shows it working or waiting.
func (v *antigravityView) status(state string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if state == "done" {
		lines := v.flushText()
		if len(lines) > 0 {
			v.emit(lines...)
		}
	}
	v.statusLocked(state, "")
}

func (v *antigravityView) statusLocked(state, kind string) {
	report := "\x1b]7501;state=" + state + ":app=antigravity"
	if kind != "" {
		report += ":kind=" + kind
	}
	_, _ = io.WriteString(v.out, report+"\x07")
}

func (v *antigravityView) update(update antigravityACPUpdate) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch update.Kind {
	case "agent_message_chunk", "agent_thought_chunk":
		chunk := antigravityChunkText(update.Content)
		if chunk == "" {
			return
		}
		if update.Kind == "agent_thought_chunk" {
			if !v.thinking {
				v.thinking = true
				lines := v.flushText()
				v.emit(append(lines, viewDim+"  · Thinking"+viewReset)...)
			}
			return
		}
		v.thinking = false
		if v.textKind != update.Kind {
			lines := v.flushText()
			if len(lines) > 0 {
				v.emit(lines...)
			}
			v.textKind = update.Kind
		}
		v.text += chunk
		if cut := strings.LastIndex(v.text, "\n"); cut >= 0 {
			done := v.text[:cut]
			v.text = v.text[cut+1:]
			if strings.TrimSpace(done) != "" {
				v.emit("  " + strings.ReplaceAll(done, "\n", "\n  "))
			}
		}
	case "tool_call":
		v.thinking = false
		lines := v.flushText()
		title := firstNonEmpty(update.Title, update.Name)
		v.tools[update.ToolCallID] = title
		v.emit(append(lines, "  "+viewBold+"●"+viewReset+" "+oneLine(title, 160))...)
	case "tool_call_update":
		switch strings.ToLower(update.Status) {
		case "failed":
			lines := v.flushText()
			v.emit(append(lines, viewDim+"    failed: "+oneLine(v.tools[update.ToolCallID], 120)+viewReset)...)
		case "completed":
			if output := antigravityToolOutput(update); output != "" && v.tools[update.ToolCallID] != "" {
				if last := lastOutputLine(output); last != "" {
					v.emit(viewDim + "    " + oneLine(last, 160) + viewReset)
				}
			}
		}
	case "plan":
		var plan struct {
			Entries []struct {
				Content string `json:"content"`
				Status  string `json:"status"`
			} `json:"entries"`
		}
		raw, _ := json.Marshal(update)
		_ = json.Unmarshal(raw, &plan)
		lines := v.flushText()
		for _, entry := range plan.Entries {
			mark := "○"
			if entry.Status == "completed" {
				mark = "✓"
			}
			lines = append(lines, "  "+mark+" "+oneLine(entry.Content, 160))
		}
		if len(lines) > 0 {
			v.emit(lines...)
		}
	}
}

func lastOutputLine(output string) string {
	if _, after, ok := strings.Cut(output, "Command output:\n"); ok {
		output = after
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		text = string(runes[:limit-1]) + "…"
	}
	return text
}

// ask shows a permission agy waits on, with its numbered answers.
func (v *antigravityView) ask(ask AntigravityAsk) {
	v.mu.Lock()
	defer v.mu.Unlock()
	copied := ask
	v.asking = &copied
	lines := v.flushText()
	lines = append(lines, "", viewBold+"Allow "+oneLine(ask.Summary, 200)+"?"+viewReset)
	for index, option := range ask.Options {
		lines = append(lines, fmt.Sprintf("  %d. %s", index+1, option.Label))
	}
	v.emit(lines...)
	v.statusLocked("blocked", "permission")
}

func (v *antigravityView) answered() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.asking != nil {
		v.asking = nil
		v.statusLocked("working", "")
		v.emit()
	}
}

// runInput reads the terminal: typed text is a message on Enter, Ctrl-C stops
// a running turn, a digit answers a waiting permission, Ctrl-D on an empty
// composer closes the Session.
func (v *antigravityView) runInput(ctx context.Context, in io.Reader, s *antigravitySession) error {
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		state, err := term.MakeRaw(int(file.Fd()))
		if err == nil {
			defer term.Restore(int(file.Fd()), state)
		}
	}
	_, _ = io.WriteString(v.out, "\x1b[?2004h")
	defer io.WriteString(v.out, "\x1b[?2004l")
	input := make(chan byte, 256)
	go func() {
		defer close(input)
		buffer := make([]byte, 4096)
		for {
			n, err := in.Read(buffer)
			for _, b := range buffer[:n] {
				select {
				case input <- b:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var escape []byte
	pasted := false
	for {
		var value byte
		var ok bool
		select {
		case <-ctx.Done():
			return ctx.Err()
		case value, ok = <-input:
			if !ok {
				return nil
			}
		}
		if len(escape) > 0 || value == 27 {
			escape = append(escape, value)
			sequence := string(escape)
			switch {
			case sequence == "\x1b[200~":
				pasted, escape = true, nil
			case sequence == "\x1b[201~":
				pasted, escape = false, nil
			case len(escape) >= 2 && escape[1] != '[' && escape[1] != 'O':
				escape = nil
			case len(escape) >= 3 && (value >= 0x40 && value <= 0x7e) && sequence != "\x1b[2" && sequence != "\x1b[20":
				escape = nil
			case len(escape) >= 8:
				escape = nil
			}
			continue
		}
		switch {
		case value == 3 && !pasted:
			if s.cancel() {
				v.note("Stopping…")
			} else {
				v.mu.Lock()
				v.draft = nil
				v.emit()
				v.mu.Unlock()
			}
		case value == 4 && !pasted:
			v.mu.Lock()
			empty := len(v.draft) == 0
			v.mu.Unlock()
			if empty {
				return nil
			}
		case value == 21 && !pasted:
			// Ctrl-U clears the draft; Mewla sends it before typed input.
			v.mu.Lock()
			v.draft = nil
			v.emit()
			v.mu.Unlock()
		case (value == '\r' || value == '\n') && !pasted:
			v.mu.Lock()
			text := string(v.draft)
			v.draft = nil
			v.emit()
			v.mu.Unlock()
			if strings.TrimSpace(text) == "" {
				continue
			}
			if err := s.submit("", text); err != nil {
				v.note(err.Error())
			}
		case value == 127 || value == 8:
			v.mu.Lock()
			if len(v.draft) > 0 {
				_, size := utf8.DecodeLastRune(v.draft)
				v.draft = v.draft[:len(v.draft)-size]
				v.emit()
			}
			v.mu.Unlock()
		case value >= '1' && value <= '9' && !pasted:
			v.mu.Lock()
			asking := v.asking
			empty := len(v.draft) == 0
			v.mu.Unlock()
			if asking != nil && empty && asking.Kind == "permission" {
				index := int(value - '1')
				if index < len(asking.Options) {
					if err := s.answer(asking.ID, asking.Options[index].ID, "terminal"); err != nil {
						v.note(err.Error())
					}
					v.answered()
				}
				continue
			}
			v.typed(value)
		default:
			if value == '\r' || value == '\n' {
				value = '\n'
			}
			if value < 32 && value != '\n' && value != '\t' {
				continue
			}
			v.typed(value)
		}
	}
}

func (v *antigravityView) typed(value byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.draft) >= 4<<20 {
		return
	}
	v.draft = append(v.draft, value)
	if value == '\n' {
		v.emit()
		return
	}
	_, _ = v.out.Write([]byte{value})
}
