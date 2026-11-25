package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateClaudeHome keeps this machine's own Claude credentials, gateway and
// settings out of the test.
func isolateClaudeHome(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// No managed settings of this machine.
	previous := claudeManagedSettingsDir
	claudeManagedSettingsDir = filepath.Join(home, "no-managed-settings")
	t.Cleanup(func() { claudeManagedSettingsDir = previous })
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", ClaudePromptCacheTTLEnv, claudeForce5mEnv} {
		t.Setenv(name, "") // restored when the test ends
		os.Unsetenv(name)
	}
	return home
}

func writeClaudeSettingsFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClaudePromptCacheFollowsTheCredentialsOfTheLaunch(t *testing.T) {
	routed := `claude --model claude-opus-5-5 --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:1","ANTHROPIC_AUTH_TOKEN":"mewla-loopback-placeholder-not-a-secret"},"permissions":{"defaultMode":"bypassPermissions"}}'`
	passthrough := `claude --settings '{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:1"},"permissions":{"defaultMode":"bypassPermissions"}}'`
	cases := []struct {
		name    string
		command string
		env     map[string]string
		setup   func(t *testing.T, home string)
		want    string
	}{
		{name: "key in the Session env", command: "claude", env: map[string]string{"ANTHROPIC_API_KEY": "k"}, want: "1h"},
		{name: "token in the Session env", command: "claude", env: map[string]string{"ANTHROPIC_AUTH_TOKEN": "k"}, want: "1h"},
		{name: "key in the command's env prefix", command: "env ANTHROPIC_API_KEY=k claude --model x", want: "1h"},
		{name: "routed Session: loopback token in --settings", command: routed, want: "1h"},
		{name: "routed Session behind an env wrapper", command: "env PATH='/a b:/bin' " + routed, want: "1h"},
		{name: "token in the daemon's env", command: "claude", setup: func(t *testing.T, _ string) { t.Setenv("ANTHROPIC_AUTH_TOKEN", "k") }, want: "1h"},
		{
			name: "token in the user's settings.json", command: "claude",
			setup: func(t *testing.T, home string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"k","ANTHROPIC_BASE_URL":"http://127.0.0.1:1","N":1,"B":true}}`)
			},
			want: "1h",
		},
		{
			name: "apiKeyHelper in the user's settings.json", command: "claude",
			setup: func(t *testing.T, home string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"apiKeyHelper":"/bin/key"}`)
			},
			want: "1h",
		},
		{
			name: "token in the settings.json of CLAUDE_CONFIG_DIR", command: "env CLAUDE_CONFIG_DIR=@HOME@/other claude",
			setup: func(t *testing.T, home string) {
				writeClaudeSettingsFile(t, filepath.Join(home, "other"), `{"env":{"ANTHROPIC_API_KEY":"k"}}`)
			},
			want: "1h",
		},
		{
			name: "CLAUDE_CONFIG_DIR in the Session env outranks ~/.claude", command: "claude", env: map[string]string{"CLAUDE_CONFIG_DIR": "@HOME@/other"},
			setup: func(t *testing.T, home string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"env":{"ANTHROPIC_API_KEY":"k"}}`)
				writeClaudeSettingsFile(t, filepath.Join(home, "other"), `{}`)
			},
			want: "",
		},
		{name: "subscription login: nothing to say", command: "claude", want: ""},
		{name: "subscription behind a loopback route: base URL alone is not a key", command: passthrough, want: ""},
		{name: "base URL in the env alone", command: "claude", env: map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1"}, want: ""},
		{name: "Codex", command: "codex", env: map[string]string{"ANTHROPIC_API_KEY": "k"}, want: ""},
		{name: "a shell", command: "exec /bin/sh", env: map[string]string{"ANTHROPIC_API_KEY": "k"}, want: ""},
		{name: "the person's TTL in the Session env", command: "claude", env: map[string]string{"ANTHROPIC_API_KEY": "k", ClaudePromptCacheTTLEnv: "5m"}, want: "5m"},
		{name: "the person's TTL in the command", command: "env CLAUDE_CODE_PROMPT_CACHE_TTL=5m claude", env: map[string]string{"ANTHROPIC_API_KEY": "k"}, want: ""},
		{name: "the person's TTL in the daemon's env", command: "claude", env: map[string]string{"ANTHROPIC_API_KEY": "k"}, setup: func(t *testing.T, _ string) { t.Setenv(ClaudePromptCacheTTLEnv, "5m") }, want: ""},
		{
			name: "the person's TTL in settings.json", command: "claude", env: map[string]string{"ANTHROPIC_API_KEY": "k"},
			setup: func(t *testing.T, home string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"env":{"CLAUDE_CODE_PROMPT_CACHE_TTL":"5m"}}`)
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateClaudeHome(t)
			env := map[string]string{}
			for k, v := range tc.env {
				env[k] = strings.ReplaceAll(v, "@HOME@", home)
			}
			command := strings.ReplaceAll(tc.command, "@HOME@", home)
			if tc.setup != nil {
				tc.setup(t, home)
			}
			got := withClaudePromptCache(command, "", env)[ClaudePromptCacheTTLEnv]
			if got != tc.want {
				t.Fatalf("%s = %q, want %q", ClaudePromptCacheTTLEnv, got, tc.want)
			}
			if _, ok := env[ClaudePromptCacheTTLEnv]; ok && tc.env[ClaudePromptCacheTTLEnv] == "" {
				t.Fatal("the caller's env map was written to")
			}
		})
	}
}

func TestClaudePromptCacheLeavesAMissingOrBrokenSettingsFileAlone(t *testing.T) {
	home := isolateClaudeHome(t)
	writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{not json`)
	if got := withClaudePromptCache("claude", "", nil)[ClaudePromptCacheTTLEnv]; got != "" {
		t.Fatalf("broken settings.json set %q", got)
	}
	if got := withClaudePromptCache("claude --settings /nonexistent.json", "", map[string]string{"ANTHROPIC_API_KEY": "k"})[ClaudePromptCacheTTLEnv]; got != "1h" {
		t.Fatalf("unreadable --settings file hid the key: %q", got)
	}
}

// Every way a Session starts (Brain's host, a Worker, a person's new Session,
// a relaunch) is CreateSession: a Claude Session on a key carries the TTL into
// the environment the session host starts it with.
func TestCreateSessionCarriesThePromptCacheTTLIntoTheSessionEnvironment(t *testing.T) {
	isolateClaudeHome(t)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "k")
	for _, tc := range []struct {
		name string
		opts CreateSessionOptions
		want bool
	}{
		{"plain Claude Session", CreateSessionOptions{Name: "a", Command: "claude --model x", Detached: true}, true},
		{"hidden Claude Session (Brain's host)", CreateSessionOptions{Name: "b", Command: "env PATH=/bin claude", Detached: true, Hidden: true, ProgressEnv: true}, true},
		{"routed launch env", CreateSessionOptions{Name: "c", Command: "claude", Detached: true, Env: map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:1"}}, true},
		{"Codex Session", CreateSessionOptions{Name: "d", Command: "codex", Detached: true}, false},
		{"shell Session", CreateSessionOptions{Name: "e", Command: "sh", Detached: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := New(0)
			backend := useFakeSessionBackend(w, newFakeSessionBackend())
			if _, err := w.CreateSession("", tc.opts); err != nil {
				t.Fatal(err)
			}
			if len(backend.created) != 1 {
				t.Fatalf("created %d Sessions", len(backend.created))
			}
			has := false
			for _, entry := range backend.created[0].env {
				if entry == ClaudePromptCacheTTLEnv+"=1h" {
					has = true
				}
				if strings.HasPrefix(entry, ClaudePromptCacheTTLEnv+"=") && entry != ClaudePromptCacheTTLEnv+"=1h" {
					t.Fatalf("unexpected %q", entry)
				}
			}
			if has != tc.want {
				t.Fatalf("session env has the 1h TTL = %v, want %v", has, tc.want)
			}
		})
	}
}

func writeClaudeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A top-level "promptCacheTtl" is the person's choice in every settings source
// Claude Code reads for the launch (settings docs, "Any file" scope: user,
// project, project local, --settings and managed), so Mewla leaves the launch
// alone, whatever the value.
func TestClaudePromptCacheLeavesAPromptCacheTtlSettingAlone(t *testing.T) {
	const ttl = `"promptCacheTtl":"5m"`
	key := map[string]string{"ANTHROPIC_API_KEY": "k"}
	cases := []struct {
		name    string
		command string
		setup   func(t *testing.T, home, project string)
		cwd     func(project string) string
		want    string
	}{
		{name: "none set", want: "1h"},
		{
			name: "user settings.json",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{`+ttl+`}`)
			},
		},
		{
			name: "user settings.json with 1h",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"promptCacheTtl":"1h"}`)
			},
		},
		{
			name: "settings.json of CLAUDE_CONFIG_DIR",
			setup: func(t *testing.T, home, _ string) {
				dir := filepath.Join(home, "elsewhere")
				t.Setenv("CLAUDE_CONFIG_DIR", dir)
				writeClaudeSettingsFile(t, dir, `{`+ttl+`}`)
			},
		},
		{
			name: "the default dir is not read when CLAUDE_CONFIG_DIR points elsewhere",
			setup: func(t *testing.T, home, _ string) {
				t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "elsewhere"))
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{`+ttl+`}`)
			},
			want: "1h",
		},
		{
			name: "project .claude/settings.json",
			setup: func(t *testing.T, _, project string) {
				writeClaudeFile(t, filepath.Join(project, ".claude", "settings.json"), `{`+ttl+`}`)
			},
		},
		{
			name: "project .claude/settings.local.json",
			setup: func(t *testing.T, _, project string) {
				writeClaudeFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{`+ttl+`}`)
			},
		},
		{
			name: "local settings at the git root of a subfolder",
			setup: func(t *testing.T, _, project string) {
				writeClaudeFile(t, filepath.Join(project, ".git", "HEAD"), "ref: refs/heads/main\n")
				writeClaudeFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{`+ttl+`}`)
			},
			cwd: func(project string) string { return filepath.Join(project, "pkg", "sub") },
		},
		{
			name: "local settings at the main checkout root of a worktree",
			setup: func(t *testing.T, home, project string) {
				main := filepath.Join(home, "main-checkout")
				writeClaudeFile(t, filepath.Join(main, ".git", "HEAD"), "ref: refs/heads/main\n")
				writeClaudeFile(t, filepath.Join(main, ".claude", "settings.local.json"), `{`+ttl+`}`)
				writeClaudeFile(t, filepath.Join(project, ".git"), "gitdir: "+filepath.Join(main, ".git", "worktrees", "w")+"\n")
			},
		},
		{
			name: "project settings of another folder are not read",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeFile(t, filepath.Join(home, "other", ".claude", "settings.json"), `{`+ttl+`}`)
			},
			want: "1h",
		},
		{
			name: "managed-settings.json",
			setup: func(t *testing.T, home, _ string) {
				claudeManagedSettingsDir = filepath.Join(home, "managed")
				writeClaudeFile(t, filepath.Join(home, "managed", "managed-settings.json"), `{`+ttl+`}`)
			},
		},
		{
			name: "managed-settings.d drop-in",
			setup: func(t *testing.T, home, _ string) {
				claudeManagedSettingsDir = filepath.Join(home, "managed")
				writeClaudeFile(t, filepath.Join(home, "managed", "managed-settings.d", "10-cache.json"), `{`+ttl+`}`)
			},
		},
		{
			name:    "--settings payload",
			command: `claude --settings '{"promptCacheTtl":"5m","permissions":{"defaultMode":"bypassPermissions"}}'`,
		},
		{
			name:    "--settings file",
			command: "claude --settings @PROJECT@/flag.json",
			setup: func(t *testing.T, _, project string) {
				writeClaudeFile(t, filepath.Join(project, "flag.json"), `{`+ttl+`}`)
			},
		},
		{
			name: "FORCE_PROMPT_CACHING_5M in the daemon's env",
			setup: func(t *testing.T, _, _ string) {
				t.Setenv(claudeForce5mEnv, "1")
			},
		},
		{
			name:    "FORCE_PROMPT_CACHING_5M in the command",
			command: "env FORCE_PROMPT_CACHING_5M=1 claude",
		},
		{
			name: "a value Claude Code ignores is not a choice",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"promptCacheTtl":"10m"}`)
			},
			want: "1h",
		},
		{
			name: "a non-string value is not a choice",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"promptCacheTtl":5}`)
			},
			want: "1h",
		},
		{
			name: "subagentPromptCacheTtl alone is not the main conversation's choice",
			setup: func(t *testing.T, home, _ string) {
				writeClaudeSettingsFile(t, filepath.Join(home, ".claude"), `{"subagentPromptCacheTtl":"5m"}`)
			},
			want: "1h",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateClaudeHome(t)
			project := filepath.Join(home, "project")
			if err := os.MkdirAll(filepath.Join(project, "pkg", "sub"), 0o700); err != nil {
				t.Fatal(err)
			}
			if tc.setup != nil {
				tc.setup(t, home, project)
			}
			command := tc.command
			if command == "" {
				command = "claude"
			}
			command = strings.ReplaceAll(command, "@PROJECT@", project)
			cwd := project
			if tc.cwd != nil {
				cwd = tc.cwd(project)
			}
			got := withClaudePromptCache(command, cwd, key)[ClaudePromptCacheTTLEnv]
			if got != tc.want {
				t.Fatalf("%s = %q, want %q", ClaudePromptCacheTTLEnv, got, tc.want)
			}
		})
	}
}

// The same through CreateSession: a project that chose its TTL keeps its
// Session untouched, and one that did not gets the hour.
func TestCreateSessionLeavesAProjectsPromptCacheTtlAlone(t *testing.T) {
	home := isolateClaudeHome(t)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "k")
	chosen := filepath.Join(home, "chosen")
	writeClaudeFile(t, filepath.Join(chosen, ".claude", "settings.local.json"), `{"promptCacheTtl":"5m"}`)
	plain := filepath.Join(home, "plain")
	if err := os.MkdirAll(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cwd  string
		want bool
	}{{chosen, false}, {plain, true}} {
		w := New(0)
		backend := useFakeSessionBackend(w, newFakeSessionBackend())
		if _, err := w.CreateSession("", CreateSessionOptions{Name: "p", Command: "claude", Cwd: tc.cwd, Detached: true}); err != nil {
			t.Fatal(err)
		}
		has := false
		for _, entry := range backend.created[0].env {
			has = has || strings.HasPrefix(entry, ClaudePromptCacheTTLEnv+"=")
		}
		if has != tc.want {
			t.Fatalf("cwd %s: Session env has the TTL = %v, want %v", filepath.Base(tc.cwd), has, tc.want)
		}
	}
}
