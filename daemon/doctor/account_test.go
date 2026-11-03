package doctor

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestAccountLabelsComeFromEachCLIsOwnRecord(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"work@example.com","organizationName":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := accountLabel("claude", dir); got != "work@example.com" {
		t.Fatalf("claude = %q", got)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"me@example.org","sub":"x"}`))
	token := "h." + payload + ".s"
	codex := t.TempDir()
	if err := os.WriteFile(filepath.Join(codex, "auth.json"), []byte(`{"tokens":{"id_token":"`+token+`","access_token":"SECRET"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := accountLabel("codex", codex); got != "me@example.org" {
		t.Fatalf("codex = %q", got)
	}
	if got := accountLabel("codex", t.TempDir()); got != "" {
		t.Fatalf("no auth.json = %q", got)
	}
	if got := accountLabel("grok", dir); got != "" {
		t.Fatalf("unsupported provider = %q", got)
	}
}

func TestSeparateSignInIsProbedWithItsOwnConfigDir(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Signed in only where CLAUDE_CONFIG_DIR holds a "signed-in" file.
	script := "#!/bin/sh\n" +
		"case \"$1\" in --version) echo 2.1.0; exit 0;; esac\n" +
		"if [ -f \"$CLAUDE_CONFIG_DIR/signed-in\" ]; then echo '{\"loggedIn\":true}'; else echo '{\"loggedIn\":false}'; exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	signedIn, signedOut := filepath.Join(home, "work"), filepath.Join(home, "spare")
	for _, dir := range []string{signedIn, signedOut} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(signedIn, "signed-in"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(signedIn, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"work@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutorsTOML(t, home, "[[executors]]\nname = \"claude-work\"\ncommand = \"claude\"\nconfig_dir = \""+signedIn+"\"\n\n[[executors]]\nname = \"claude-spare\"\ncommand = \"claude\"\nconfig_dir = \""+signedOut+"\"\n")
	report, err := Run(Options{
		Home: home, StateDir: filepath.Join(home, ".mewla"), ExecutorsPath: filepath.Join(home, ".mewla", "executors.toml"),
		Addr: "127.0.0.1:0", PathEnv: binDir + ":/usr/bin:/bin",
		Listen:  func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatal(err)
	}
	work, spare := findExecutor(report, "claude-work"), findExecutor(report, "claude-spare")
	if work == nil || work.Auth != AuthAuthenticated || work.Account != "work@example.com" || work.ConfigDir != signedIn {
		t.Fatalf("claude-work = %+v", work)
	}
	if spare == nil || spare.Auth != AuthUnauthenticated || spare.Account != "" {
		t.Fatalf("claude-spare = %+v; each sign-in must be probed with its own directory", spare)
	}
}
