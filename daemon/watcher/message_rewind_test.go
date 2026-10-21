package watcher

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rewindFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "rewind", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The parsers read screens captured from the real CLIs.
func TestParseClaudeRewindScreenReadsRealScreens(t *testing.T) {
	idle := parseClaudeRewindScreen(rewindFixture(t, "claude-idle.txt"))
	if !claudeComposerEmpty(idle) || idle.Picker || idle.Confirm {
		t.Fatalf("idle screen = %+v", idle)
	}
	last := parseClaudeRewindScreen(rewindFixture(t, "claude-picker-last.txt"))
	if !last.Picker || last.Selected != "(current)" {
		t.Fatalf("picker opens on the latest message: %+v", last)
	}
	up := parseClaudeRewindScreen(rewindFixture(t, "claude-picker-up2.txt"))
	if !up.Picker || up.Selected != "reply with exactly: one" {
		t.Fatalf("picker selection = %+v", up)
	}
	confirm := parseClaudeRewindScreen(rewindFixture(t, "claude-confirm.txt"))
	if !confirm.Confirm || confirm.Selected != claudeRestoreConversationLabel || confirm.ConfirmText != "reply with exactly: one" {
		t.Fatalf("confirm screen = %+v", confirm)
	}
	prefilled := parseClaudeRewindScreen(rewindFixture(t, "claude-prefilled.txt"))
	if prefilled.Picker || prefilled.Confirm || prefilled.Composer != "reply with exactly: one" {
		t.Fatalf("prefilled composer = %+v", prefilled)
	}
}

func TestParseCodexRewindScreenReadsRealScreens(t *testing.T) {
	idle := parseCodexRewindScreen(rewindFixture(t, "codex-idle.ansi"))
	if !codexComposerEmpty(idle) || idle.Pager {
		t.Fatalf("idle screen = %+v", idle)
	}
	last := parseCodexRewindScreen(rewindFixture(t, "codex-pager-last.ansi"))
	if !last.Pager || last.Selected != "reply with exactly: BETA2" {
		t.Fatalf("pager opens on the latest prompt: %+v", last)
	}
	left := parseCodexRewindScreen(rewindFixture(t, "codex-pager-left1.ansi"))
	if left.Selected != "reply with exactly: one" {
		t.Fatalf("Left selects an earlier prompt: %+v", left)
	}
	after := parseCodexRewindScreen(rewindFixture(t, "codex-after-rewind.ansi"))
	if after.Pager || after.Composer != "reply with exactly: one" {
		t.Fatalf("composer after the rewind = %+v", after)
	}
}

// A narrow pane shortens the pager's status line and a light theme adds a
// background after the reverse-video marker; both were seen in a phone-sized pane.
func TestParseCodexRewindScreenReadsANarrowLightPane(t *testing.T) {
	screen := "\x1b[1;2m› \x1b[0m\x1b[48;2;245;245;242malpha\x1b[0m\n" +
		"\x1b[1;2m› \x1b[0;7m\x1b[48;2;245;245;242mreply with exactly: beta\x1b[0m\x1b[48;2;245;245;242m\n" +
		"\x1b[1m›\x1b[0;2m\x1b[48;2;240;240;237m Ask Codex to do anything\n" +
		"  GPT-6-Astra high · /home/user/.mewla/t/8r…\n" +
		"  \x1b[38;2;28;100;200mBrowsing\x1b[2m\x1b[39m · \x1b[0;1m↑↓/jk ←→/hl\x1b[0;2m · ↵ rewind · esc\n"
	got := parseCodexRewindScreen(screen)
	if !got.Pager || got.Selected != "reply with exactly: beta" {
		t.Fatalf("narrow light pane = %+v", got)
	}
}

func TestParsePiRewindScreenReadsRealScreens(t *testing.T) {
	// The driver captures pi without styling; the fixtures keep it, so strip it.
	rewindFixture := func(t *testing.T, name string) string {
		return ansiSequence.ReplaceAllString(rewindFixture(t, name), "")
	}
	idle := parsePiRewindScreen(rewindFixture(t, "pi-idle.ansi"))
	if !idle.HasComposer || idle.Composer != "" || idle.Tree {
		t.Fatalf("idle screen = %+v", idle)
	}
	tree := parsePiRewindScreen(rewindFixture(t, "pi-tree-user.ansi"))
	if !tree.Tree || !tree.UserFilter || tree.Selected != "reply with exactly: TWO-EDITED" {
		t.Fatalf("tree screen = %+v", tree)
	}
	summarize := parsePiRewindScreen(rewindFixture(t, "pi-summarize.ansi"))
	if !summarize.Summarize || summarize.SummarizePick != "No summary" {
		t.Fatalf("summarize screen = %+v", summarize)
	}
	after := parsePiRewindScreen(rewindFixture(t, "pi-after.ansi"))
	if after.Composer != "reply with exactly: one" {
		t.Fatalf("composer after navigating = %+v", after)
	}
}

func TestRewindTextMatchesShortenedPrompts(t *testing.T) {
	for _, tc := range []struct {
		shown, want string
		ok          bool
	}{
		{"reply with exactly: one", "reply with exactly: one", true},
		{"reply with exactly:  one", "reply with exactly: one", true},
		{"refactor the parser and…", "refactor the parser and add tests\nthen ship", true},
		{"refactor the parser and add tests", "refactor the parser and add tests\nthen ship", true},
		{"reply with exactly: two", "reply with exactly: one", false},
		{"yes please", "yes", false},
		{"", "reply with exactly: one", false},
		{"…", "reply with exactly: one", false},
	} {
		if got := rewindTextMatches(tc.shown, tc.want); got != tc.ok {
			t.Errorf("rewindTextMatches(%q, %q) = %v, want %v", tc.shown, tc.want, got, tc.ok)
		}
	}
}

// scriptedPane plays a CLI: each key moves a small state machine and every
// capture renders the current screen. It records the keys it was sent.
type scriptedPane struct {
	t      *testing.T
	keys   []string
	render func(p *scriptedPane) string
	key    func(p *scriptedPane, key string, count int)
	state  string
	index  int
	draft  string
}

func (p *scriptedPane) capture(bool) (string, error) { return p.render(p), nil }

func (p *scriptedPane) sendKey(key string) error {
	p.keys = append(p.keys, key)
	p.key(p, key, 1)
	return nil
}

func (p *scriptedPane) repeatKey(key string, count int) error {
	p.keys = append(p.keys, fmt.Sprintf("%s×%d", key, count))
	p.key(p, key, count)
	return nil
}

func claudeScreen(p *scriptedPane, rows []string) string {
	switch p.state {
	case "picker":
		var b strings.Builder
		b.WriteString("   Rewind\n   Restore the code and/or conversation to the point before…\n")
		for i, row := range append(append([]string{}, rows...), "(current)") {
			mark := "  "
			if i == p.index {
				mark = "❯ "
			}
			b.WriteString("   " + mark + row + "\n")
		}
		return b.String()
	case "confirm":
		options := []string{"Restore conversation", "Summarize from here", "Never mind"}
		var b strings.Builder
		b.WriteString("   Rewind\n   Confirm you want to restore to the point before you sent this message:\n   │ " + rows[len(rows)-p.index-1+0] + "\n")
		for i, o := range options {
			mark := "  "
			if i == p.index {
				mark = "❯ "
			}
			b.WriteString(fmt.Sprintf("   %s%d. %s\n", mark, i+1, o))
		}
		return b.String()
	default:
		return "✻ done\n──────\n❯ " + p.draft + "\n──────\n  ⏵⏵ auto mode on\n"
	}
}

func TestDriveClaudeRewindSelectsConversationOnlyRestore(t *testing.T) {
	prompts := []string{"one", "two", "three"}
	target := 1 // "two": one prompt after it
	var confirmIndex int
	pane := &scriptedPane{t: t, state: "idle"}
	pane.render = func(p *scriptedPane) string {
		if p.state == "confirm" {
			// the confirm screen quotes the prompt that was highlighted
			var b strings.Builder
			b.WriteString("   Rewind\n   Confirm you want to restore to the point before you sent this message:\n   │ " + prompts[target] + "\n   │ (1m ago)\n")
			for i, o := range []string{"Restore conversation", "Summarize from here", "Never mind"} {
				mark := "  "
				if i == confirmIndex {
					mark = "❯ "
				}
				b.WriteString(fmt.Sprintf("   %s%d. %s\n", mark, i+1, o))
			}
			return b.String()
		}
		return claudeScreen(p, prompts)
	}
	pane.key = func(p *scriptedPane, key string, count int) {
		switch {
		case p.state == "idle" && key == "Escape":
			if p.index == 1 {
				p.state, p.index = "picker", len(prompts)
			}
			p.index = 1
		case p.state == "picker" && key == "Up":
			p.index--
		case p.state == "picker" && key == "Enter":
			p.state, confirmIndex = "confirm", 0
		case p.state == "confirm" && key == "Enter":
			p.state, p.draft = "idle", prompts[target]
		case p.state == "confirm" && key == "Down":
			confirmIndex++
		case key == "BSpace":
			p.draft = ""
		}
	}
	// The first Escape arms the picker, the second opens it.
	pane.key = wrapEscapeOpens(pane.key, func(p *scriptedPane) { p.state, p.index = "picker", len(prompts) })
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindClaude, Text: "two", Back: 1}, time.Second)
	if err != nil {
		t.Fatalf("driveMessageRewind: %v (keys %v)", err, pane.keys)
	}
	want := "Escape Escape Up Up Enter Enter BSpace×11"
	if got := strings.Join(pane.keys, " "); got != want {
		t.Fatalf("keys = %q, want %q", got, want)
	}
	if pane.draft != "" {
		t.Fatalf("composer not cleared: %q", pane.draft)
	}
}

// wrapEscapeOpens turns the second consecutive Escape in the idle state into
// the open action, as the CLIs' double-Escape gesture does.
func wrapEscapeOpens(inner func(*scriptedPane, string, int), open func(*scriptedPane)) func(*scriptedPane, string, int) {
	escapes := 0
	return func(p *scriptedPane, key string, count int) {
		if key == "Escape" && p.state == "idle" {
			escapes++
			if escapes == 2 {
				open(p)
			}
			return
		}
		inner(p, key, count)
	}
}

func TestDriveClaudeRewindRefusesWhenTheHighlightedMessageDiffers(t *testing.T) {
	pane := &scriptedPane{t: t, state: "idle"}
	prompts := []string{"one", "two", "three"}
	pane.render = func(p *scriptedPane) string { return claudeScreen(p, prompts) }
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		if p.state == "picker" && key == "Up" {
			p.index--
		}
	}, func(p *scriptedPane) { p.state, p.index = "picker", len(prompts) })
	// The transcript says "two" is one prompt back, but the CLI's list holds a
	// prompt the transcript does not know about, so Up lands on "three".
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindClaude, Text: "zero", Back: 1}, 300*time.Millisecond)
	if !errors.Is(err, ErrMessageRewindRefused) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	for _, key := range pane.keys {
		if key == "Enter" || strings.HasPrefix(key, "BSpace") {
			t.Fatalf("pressed %q without proof of the target: %v", key, pane.keys)
		}
	}
	if pane.keys[len(pane.keys)-1] != "Escape" {
		t.Fatalf("did not leave the picker: %v", pane.keys)
	}
}

func TestDriveRewindRefusesWhileTheComposerHoldsADraftOrWorkIsRunning(t *testing.T) {
	for name, screen := range map[string]string{
		"draft":   "──────\n❯ half-written thought\n──────\n",
		"running": "✻ Thinking… (3s · esc to interrupt)\n──────\n❯ \n──────\n",
	} {
		pane := &scriptedPane{t: t, render: func(*scriptedPane) string { return screen }, key: func(*scriptedPane, string, int) {}}
		err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindClaude, Text: "two", Back: 0}, 100*time.Millisecond)
		if !errors.Is(err, ErrMessageRewindRefused) || len(pane.keys) != 0 {
			t.Fatalf("%s: err = %v, keys = %v; nothing may be pressed", name, err, pane.keys)
		}
	}
}

func codexScreen(p *scriptedPane, prompts []string) string {
	var b strings.Builder
	for i, prompt := range prompts {
		if p.state == "pager" && i == p.index {
			b.WriteString("\x1b[1;2m› \x1b[0;7m" + prompt + "\x1b[0m\n")
		} else {
			b.WriteString("\x1b[1;2m› \x1b[0m" + prompt + "\n")
		}
	}
	if p.state == "pager" {
		b.WriteString("\x1b[1m›\x1b[0;2m Ask Codex to do anything\n  Browsing transcript · ←→/hl prompts · ↵ rewind\n")
		return b.String()
	}
	if p.draft != "" {
		b.WriteString("\x1b[1m›\x1b[0m " + p.draft + "\n")
	} else {
		b.WriteString("\x1b[1m›\x1b[0m \x1b[2mAsk Codex to do anything\x1b[0m\n")
	}
	return b.String()
}

func TestDriveCodexRewindWalksBackToTheTargetPrompt(t *testing.T) {
	prompts := []string{"alpha", "beta", "gamma"}
	pane := &scriptedPane{t: t, state: "idle"}
	pane.render = func(p *scriptedPane) string { return codexScreen(p, prompts) }
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		switch {
		case p.state == "pager" && key == "Left":
			p.index--
		case p.state == "pager" && key == "Enter":
			p.state, p.draft = "idle", prompts[p.index]
		case key == "BSpace":
			p.draft = ""
		}
	}, func(p *scriptedPane) { p.state, p.index = "pager", len(prompts)-1 })
	if err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindCodex, Text: "beta", Back: 1}, time.Second); err != nil {
		t.Fatalf("driveMessageRewind: %v (keys %v)", err, pane.keys)
	}
	if got, want := strings.Join(pane.keys, " "), "Escape Escape Left Enter BSpace×12"; got != want {
		t.Fatalf("keys = %q, want %q", got, want)
	}
}

func TestDriveCodexRewindDoesNotEnterOnAnotherPrompt(t *testing.T) {
	prompts := []string{"alpha", "beta", "gamma"}
	pane := &scriptedPane{t: t, state: "idle"}
	pane.render = func(p *scriptedPane) string { return codexScreen(p, prompts) }
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		if p.state == "pager" && key == "Left" {
			p.index--
		}
	}, func(p *scriptedPane) { p.state, p.index = "pager", len(prompts)-1 })
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindCodex, Text: "alpha", Back: 1}, 300*time.Millisecond)
	if !errors.Is(err, ErrMessageRewindRefused) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	for _, key := range pane.keys {
		if key == "Enter" {
			t.Fatalf("pressed Enter on the wrong prompt: %v", pane.keys)
		}
	}
}

func piScreen(p *scriptedPane, prompts []string) string {
	switch p.state {
	case "tree":
		var b strings.Builder
		b.WriteString("  Session Tree\n  Type to search:\n────────\n")
		for i, prompt := range prompts {
			mark := "  "
			if i == p.index {
				mark = "› "
			}
			b.WriteString(mark + "• user: " + prompt + "\n")
		}
		b.WriteString(fmt.Sprintf("  (%d/%d) [user]\n────────\n", p.index+1, len(prompts)))
		return b.String()
	case "summarize":
		return " Summarize branch?\n → No summary\n   Summarize\n"
	default:
		return " earlier output\n────────\n" + p.draft + "\n────────\n~/repo\n"
	}
}

func TestDrivePiRewindChoosesNoSummary(t *testing.T) {
	prompts := []string{"one", "two", "three"}
	pane := &scriptedPane{t: t, state: "idle"}
	pane.render = func(p *scriptedPane) string { return piScreen(p, prompts) }
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		switch {
		case p.state == "tree" && key == "C-u":
			p.index = len(prompts) - 1
		case p.state == "tree" && key == "Up":
			p.index--
		case p.state == "tree" && key == "Enter":
			p.state = "summarize"
		case p.state == "summarize" && key == "Enter":
			p.state, p.draft = "idle", prompts[p.index]
		case key == "BSpace":
			p.draft = ""
		}
	}, func(p *scriptedPane) { p.state, p.index = "tree", len(prompts)-1 })
	if err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindPi, Text: "two", Back: 1}, time.Second); err != nil {
		t.Fatalf("driveMessageRewind: %v (keys %v)", err, pane.keys)
	}
	if got, want := strings.Join(pane.keys, " "), "Escape Escape C-u Up Enter Enter BSpace×11"; got != want {
		t.Fatalf("keys = %q, want %q", got, want)
	}
}

func TestDriveRewindRefusesUnsupportedProvider(t *testing.T) {
	pane := &scriptedPane{t: t, render: func(*scriptedPane) string { return "" }, key: func(*scriptedPane, string, int) {}}
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: "grok", Text: "hi", Back: 0}, 50*time.Millisecond)
	if !errors.Is(err, ErrMessageRewindRefused) || len(pane.keys) != 0 {
		t.Fatalf("err = %v keys = %v", err, pane.keys)
	}
}

func claudeRewindPane(prompts []string, target int, backspaceClears bool) *scriptedPane {
	var confirmIndex int
	pane := &scriptedPane{state: "idle"}
	pane.render = func(p *scriptedPane) string {
		if p.state == "confirm" {
			var b strings.Builder
			b.WriteString("   Rewind\n   Confirm you want to restore to the point before you sent this message:\n   │ " + prompts[target] + "\n")
			for i, o := range []string{"Restore conversation", "Never mind"} {
				mark := "  "
				if i == confirmIndex {
					mark = "❯ "
				}
				b.WriteString(fmt.Sprintf("   %s%d. %s\n", mark, i+1, o))
			}
			return b.String()
		}
		return claudeScreen(p, prompts)
	}
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		switch {
		case p.state == "picker" && key == "Up":
			p.index--
		case p.state == "picker" && key == "Enter":
			p.state, confirmIndex = "confirm", 0
		case p.state == "confirm" && key == "Enter":
			p.state, p.draft = "idle", prompts[target]
		case key == "BSpace" && backspaceClears:
			p.draft = ""
		}
	}, func(p *scriptedPane) { p.state, p.index = "picker", len(prompts) })
	return pane
}

// After the CLI's own confirm key the conversation is rewound whatever happens
// next, so a draft that will not clear is not a refusal: nothing may be sent
// into that input box, and the caller must be told the rewind did happen.
func TestDriveClaudeRewindReportsARewoundButDirtyInputBox(t *testing.T) {
	pane := claudeRewindPane([]string{"one", "two", "three"}, 1, false)
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindClaude, Text: "two", Back: 1}, 300*time.Millisecond)
	if !errors.Is(err, ErrMessageRewindInputNotClean) || errors.Is(err, ErrMessageRewindRefused) {
		t.Fatalf("err = %v, want the rewound-but-dirty error and not a refusal", err)
	}
}

func TestDriveClaudeRewindReportsARewoundAgentThatNeverReturnsToItsInputBox(t *testing.T) {
	pane := claudeRewindPane([]string{"one", "two", "three"}, 1, true)
	inner := pane.key
	pane.key = func(p *scriptedPane, key string, count int) {
		inner(p, key, count)
		if p.state == "idle" && p.draft != "" {
			p.state = "stuck" // an unreadable screen after the confirm key
		}
	}
	render := pane.render
	pane.render = func(p *scriptedPane) string {
		if p.state == "stuck" {
			return "working…\n"
		}
		return render(p)
	}
	err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindClaude, Text: "two", Back: 1}, 300*time.Millisecond)
	if !errors.Is(err, ErrMessageRewindInputNotClean) {
		t.Fatalf("err = %v, want the rewound-but-dirty error", err)
	}
}

// A prompt that the CLI shows differently (a pasted block) still rewinds: the
// text is proven before the confirm key, never after it.
func TestDriveCodexRewindClearsWhateverTheCLIPrefilled(t *testing.T) {
	prompts := []string{"alpha", "beta"}
	pane := &scriptedPane{state: "idle"}
	pane.render = func(p *scriptedPane) string { return codexScreen(p, prompts) }
	pane.key = wrapEscapeOpens(func(p *scriptedPane, key string, count int) {
		switch {
		case p.state == "pager" && key == "Left":
			p.index--
		case p.state == "pager" && key == "Enter":
			p.state, p.draft = "idle", "[Pasted Content 1200 chars]"
		case key == "BSpace":
			p.draft = ""
		}
	}, func(p *scriptedPane) { p.state, p.index = "pager", len(prompts)-1 })
	if err := driveMessageRewind(pane, MessageRewindRequest{Provider: MessageRewindCodex, Text: "alpha", Back: 1}, time.Second); err != nil {
		t.Fatalf("driveMessageRewind: %v (keys %v)", err, pane.keys)
	}
}
