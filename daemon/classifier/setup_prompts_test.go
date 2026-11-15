package classifier

import (
	"os"
	"strings"
	"testing"
)

func setupSignal(t *testing.T, command, file string) ActivitySignal {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var adapter interface {
		Infer(ActivityInput) ActivitySignal
	} = NewClaudeActivityAdapter()
	if command == "codex" {
		adapter = NewCodexActivityAdapter()
	}
	return adapter.Infer(ActivityInput{Worker: Worker{Command: command}, Capture: exactCaptureInfo, PaneContent: string(raw)})
}

func labels(options []PromptOption) []string {
	var out []string
	for _, option := range options {
		out = append(out, option.Label)
	}
	return out
}

// Every screen a Claude Code or Codex that never ran shows before its first
// conversation waited in the terminal, where nobody sits at a Session: Chat
// said "Ready" or showed nothing (Windows 11, 11-16). Each is a prompt with
// its real choices now. Captured with `mewla worker capture` on Windows 11.
func TestClaudeFirstRunScreensArePrompts(t *testing.T) {
	theme := setupSignal(t, "claude", "claude-2.1.325/first-run-theme-windows.txt")
	if theme.State != StateBlocked || theme.Prompt == nil || !theme.Prompt.Unnumbered || len(theme.Prompt.Options) != 7 ||
		theme.Prompt.Options[1].Label != "Dark mode" || !theme.Prompt.Options[1].Selected || theme.Prompt.Options[0].Label != "Auto (match terminal)" {
		t.Fatalf("theme: %+v %+v", theme, theme.Prompt)
	}

	for _, file := range []string{"claude-2.1.325/login-method-windows-43x39.txt", "claude-2.1.325/login-method-linux-100x40.txt"} {
		login := setupSignal(t, "claude", file)
		prompt := login.Prompt
		if login.State != StateBlocked || prompt == nil || prompt.Unnumbered || prompt.Kind != PromptStartup {
			t.Fatalf("%s: %+v", file, login)
		}
		if got := strings.Join(labels(prompt.Options), "|"); got != "Claude account with subscription|Anthropic Console account|3rd-party platform" {
			t.Fatalf("%s: options %q", file, got)
		}
		if prompt.Options[2].Description != "Bedrock, Microsoft Foundry, Google Vertex AI" || !prompt.Options[0].Selected || !strings.HasPrefix(prompt.Request, "Claude Code can be used with your Claude subscription") {
			t.Fatalf("%s: %+v", file, prompt)
		}
	}
	// The same menu at another width is the same prompt.
	if setupSignal(t, "claude", "claude-2.1.325/login-method-windows-43x39.txt").Prompt.ID != setupSignal(t, "claude", "claude-2.1.325/login-method-linux-100x40.txt").Prompt.ID {
		t.Fatal("the login menu's id depends on the pane's width")
	}

	signIn := setupSignal(t, "claude", "claude-2.1.325/signin-link-windows-43x39.txt").Prompt
	if signIn == nil || signIn.Kind != PromptSignIn || signIn.Input == "" || len(signIn.Options) != 0 ||
		!strings.HasPrefix(signIn.Link, "https://claude.com/cai/oauth/authorize?code=true&client_id=") || !strings.HasSuffix(signIn.Link, "&state="+strings.Repeat("X", 43)) || strings.ContainsAny(signIn.Link, " \n") {
		t.Fatalf("sign-in: %+v", signIn)
	}

	// A phone's terminal view, narrower still, wraps the last line: the same prompt.
	raw, _ := os.ReadFile("testdata/claude-2.1.325/signin-link-windows-43x39.txt")
	wrapped := strings.Replace(string(raw), " Paste code here if prompted >", " Paste code here if\n prompted >", 1)
	again := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: wrapped}).Prompt
	if again == nil || again.ID != signIn.ID {
		t.Fatalf("wrapped sign-in: %+v", again)
	}

	notes := setupSignal(t, "claude", "claude-2.1.325/security-notes-windows-43x39.txt").Prompt
	if notes == nil || notes.Title != "Security notes" || len(notes.Options) != 1 || notes.Options[0].Key != "Enter" || !strings.Contains(notes.Request, "Claude can make mistakes.") {
		t.Fatalf("security notes: %+v", notes)
	}

	// Above the notes, a wider pane draws Claude's banner picture: not part of
	// what the card says.
	drawn := setupSignal(t, "claude", "claude-2.1.325/security-notes-banner-windows.txt").Prompt
	if drawn == nil || !strings.HasPrefix(drawn.Request, "Security notes: 1. Claude can make mistakes.") || drawn.Title != "Security notes" {
		t.Fatalf("security notes under the banner: %+v", drawn)
	}

	// Folder trust (no numbers) stays the menu it was, at a phone's width too.
	trust := setupSignal(t, "claude", "claude-2.1.325/folder-trust-windows-43x39.txt").Prompt
	if trust == nil || trust.Kind != PromptStartup || strings.Join(labels(trust.Options), "|") != "No, exit|Yes, I trust this folder" {
		t.Fatalf("trust: %+v", trust)
	}
}

// A sign-in that failed: "Press Enter to retry. (Esc to continue without
// logging in)" (Windows 11 at 43 columns), answered with those keys.
func TestClaudeFailedSignInIsAPromptAnsweredWithItsKeys(t *testing.T) {
	pane := "mewla-origin 00112233445566778899aabb\nWelcome to Claude Code v2.1.325\n\n OAuth error: Invalid code. Please make\n sure the full code was copied\n\n Press Enter to retry. (Esc to continue\n without logging in)\n"
	signal := NewClaudeActivityAdapter().Infer(ActivityInput{Worker: Worker{Command: "claude"}, Capture: exactCaptureInfo, PaneContent: pane})
	prompt := signal.Prompt
	if signal.State != StateBlocked || prompt == nil || prompt.Request != "OAuth error: Invalid code. Please make sure the full code was copied" {
		t.Fatalf("%+v %+v", signal, prompt)
	}
	if len(prompt.Options) != 2 || prompt.Options[0].Key != "Enter" || prompt.Options[1].Key != "Escape" || prompt.Options[1].Label != "Continue without signing in" {
		t.Fatalf("options %+v", prompt.Options)
	}
}

// A Session started as plain `codex` in a folder Codex was not told to trust
// opens on "Trust this folder?": a card with its two rows, answered by digit
// (Codex 0.167 picks the row and goes on).
func TestCodexFolderTrustIsAPrompt(t *testing.T) {
	trust := setupSignal(t, "codex", "codex-0.167.0/folder-trust-linux-100x40.txt")
	prompt := trust.Prompt
	if trust.State != StateBlocked || prompt == nil || prompt.Kind != PromptStartup || prompt.Unnumbered || prompt.Title != "Trust this folder?" {
		t.Fatalf("trust: %+v", trust)
	}
	if got := strings.Join(labels(prompt.Options), "|"); got != "Trust and continue|Back to Agent Command Center" || !prompt.Options[0].Selected {
		t.Fatalf("trust options: %+v", prompt.Options)
	}
	if !strings.HasPrefix(prompt.Request, "Folder access /home/ana/app Trust this folder? Codex can read") {
		t.Fatalf("trust request: %q", prompt.Request)
	}
}

func TestCodexSignInScreensArePrompts(t *testing.T) {
	login := setupSignal(t, "codex", "codex-0.169.0/login-windows-200x50.txt")
	prompt := login.Prompt
	if login.State != StateBlocked || prompt == nil || !prompt.Unnumbered || prompt.Kind != PromptStartup {
		t.Fatalf("login: %+v", login)
	}
	if got := strings.Join(labels(prompt.Options), "|"); got != "Sign in with ChatGPT|Sign in with Device Code|Sign in another way" {
		t.Fatalf("login options %q", got)
	}
	if prompt.Options[1].Description != "Sign in from another device with a one-time code" || !prompt.Options[0].Selected ||
		prompt.Request != "Sign in with ChatGPT to use Codex as part of your paid plan or connect an API key for usage-based billing" {
		t.Fatalf("login: %+v", prompt)
	}
	// At a phone's width the rows and their descriptions wrap; the cursor
	// moved to the second row. The same prompt.
	narrow := setupSignal(t, "codex", "codex-0.169.0/login-cursor-on-2-windows-43x39.txt").Prompt
	if narrow == nil || narrow.ID != prompt.ID || !narrow.Options[1].Selected || narrow.Options[1].Description != prompt.Options[1].Description {
		t.Fatalf("narrow: %+v", narrow)
	}

	device := setupSignal(t, "codex", "codex-0.169.0/device-code-windows-43x39.txt").Prompt
	if device == nil || device.Kind != PromptSignIn || device.Link != "https://auth.openai.com/codex/device" || device.Code != "ABCD-1234Z" ||
		len(device.Options) != 1 || device.Options[0].Key != "Escape" || device.Input != "" {
		t.Fatalf("device: %+v", device)
	}
}
