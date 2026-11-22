package work

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// One relaunch path for every agent: the launch the Worker was spawned with
// and the conversation it is in give the launch that resumes it.
func TestRelaunchCommandResumesEachAgentsConversation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, provider, launch, token string
		want                          []string
		absent                        []string
	}{
		{
			name: "claude's fresh session id", provider: WorkerProviderClaude,
			launch: "claude --model claude-sonnet-5-5 --permission-mode bypassPermissions --session-id 11111111-1111-4111-8111-111111111111",
			token:  "11111111-1111-4111-8111-111111111111",
			want:   []string{"claude ", "--model claude-sonnet-5-5", "--resume 11111111-1111-4111-8111-111111111111"}, absent: []string{"--session-id"},
		},
		{
			name: "claude after a /clear", provider: WorkerProviderClaude,
			launch: "claude --session-id 11111111-1111-4111-8111-111111111111",
			token:  "22222222-2222-4222-8222-222222222222",
			want:   []string{"--resume 22222222-2222-4222-8222-222222222222"}, absent: []string{"--session-id", "11111111"},
		},
		{
			name: "codex", provider: WorkerProviderCodex,
			launch: "codex --model gpt-6.2-astra --dangerously-bypass-approvals-and-sandbox",
			token:  "01a200b3-2360-7510-b282-2571a5a88d2d",
			// The subcommand first: the app-server wrap refuses permission flags on a resume.
			want: []string{"codex resume 01a200b3-2360-7510-b282-2571a5a88d2d", "--dangerously-bypass-approvals-and-sandbox"},
		},
		{
			name: "antigravity", provider: WorkerProviderAntigravity,
			launch: "agy --dangerously-skip-permissions", token: "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
			want: []string{"agy --dangerously-skip-permissions", "--conversation 0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"},
		},
		{
			name: "cursor", provider: WorkerProviderCursor,
			launch: "cursor-agent --force --sandbox disabled", token: "chat-1",
			want: []string{"cursor-agent --force --sandbox disabled", "--resume chat-1"},
		},
		{
			name: "grok", provider: WorkerProviderGrok,
			launch: "grok --no-alt-screen --permission-mode bypassPermissions", token: "grok-session-1",
			want: []string{"grok --no-alt-screen", "--resume grok-session-1"},
		},
		{
			name: "opencode continues its own session", provider: WorkerProviderOpenCode,
			launch: "opencode --auto --session ses_0123456789abABCDEFGHIJKLMN", token: "ses_0123456789abABCDEFGHIJKLMN",
			want: []string{"opencode --auto --session ses_0123456789abABCDEFGHIJKLMN"},
		},
		{
			name: "pi continues its own session file", provider: WorkerProviderPi,
			launch: "pi --session /home/u/.mewla/pi/s.jsonl", token: "/home/u/.mewla/pi/s.jsonl",
			want: []string{"pi --session /home/u/.mewla/pi/s.jsonl"},
		},
		{
			name: "dsh", provider: WorkerProviderDSH,
			launch: "dsh", token: "session-0123456789abcdef",
			want: []string{filepath.Base(executable), "dsh-session --dsh-session session-0123456789abcdef"},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := RelaunchCommand(test.provider, test.launch, test.token)
			if err != nil {
				t.Fatalf("RelaunchCommand: %v", err)
			}
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Fatalf("%q lacks %q", got, want)
				}
			}
			for _, absent := range test.absent {
				if strings.Contains(got, absent) {
					t.Fatalf("%q still has %q", got, absent)
				}
			}
			if token, present, err := ProviderResumeToken(test.provider, got); test.provider != WorkerProviderDSH &&
				(err != nil || !present || token != test.token) {
				t.Fatalf("%q resumes %q (%v, %v), want %q", got, token, present, err, test.token)
			}
		})
	}
}

func TestRelaunchCommandRefusesAnotherAgentsConversation(t *testing.T) {
	if _, err := RelaunchCommand(WorkerProviderOpenCode, "opencode --auto --session ses_0123456789abABCDEFGHIJKLMN", "ses_ffffffffffffZZZZZZZZZZZZZZ"); err == nil {
		t.Fatal("an OpenCode launch was pointed at a different session")
	}
	if _, err := RelaunchCommand(WorkerProviderClaude, "claude", ""); err == nil {
		t.Fatal("a relaunch without a conversation was built")
	}
}

func TestProgramUpdatedBetween(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "claude")
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(SetAgentLookPath(func(string) (string, error) { return link, nil }))
	set := func(at time.Time) {
		t.Helper()
		if err := os.Chtimes(bin, at, at); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().Add(-2 * time.Hour)
	end := start.Add(time.Hour)
	set(start.Add(-time.Hour))
	if ProgramUpdatedBetween("claude --resume x", start, end) {
		t.Fatal("a program older than the start reads as updated")
	}
	set(start.Add(30 * time.Minute))
	if !ProgramUpdatedBetween("claude --resume x", start, end) {
		t.Fatal("a program replaced while it ran does not read as updated")
	}
	// Claude Code updates itself about daily: a replacement after the death
	// says nothing about it.
	set(end.Add(time.Minute))
	if ProgramUpdatedBetween("claude --resume x", start, end) {
		t.Fatal("a program replaced after the end reads as updated before it")
	}
}
