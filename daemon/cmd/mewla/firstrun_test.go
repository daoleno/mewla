package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/enrollment"
)

func TestFirstRunPromptOnlyForAFreshManualStart(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("XPC_SERVICE_NAME", "")
	fresh := t.TempDir()
	if !firstRunPromptAllowed(nil, fresh, true) {
		t.Fatal("fresh interactive start did not prompt")
	}
	if !firstRunPromptAllowed([]string{"-state-dir", fresh}, fresh, true) {
		t.Fatal("-state-dir alone suppressed the prompt")
	}
	for name, args := range map[string][]string{"lan": {"--lan"}, "addr": {"-addr", "127.0.0.1:1"}} {
		if firstRunPromptAllowed(args, fresh, true) {
			t.Fatalf("%s flag still prompted", name)
		}
	}
	if firstRunPromptAllowed(nil, fresh, false) {
		t.Fatal("non-interactive start prompted")
	}
	t.Setenv("INVOCATION_ID", "unit")
	if firstRunPromptAllowed(nil, fresh, true) {
		t.Fatal("systemd start prompted")
	}
	t.Setenv("INVOCATION_ID", "")
	existing := t.TempDir()
	if err := os.WriteFile(filepath.Join(existing, "identity.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if firstRunPromptAllowed(nil, existing, true) {
		t.Fatal("an existing daemon identity prompted")
	}
	if err := saveStartupChoice(fresh, choiceLater); err != nil {
		t.Fatal(err)
	}
	if firstRunPromptAllowed(nil, fresh, true) {
		t.Fatal("a saved answer prompted again")
	}
}

func TestFirstRunChoiceRemembersWiFi(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	choice, err := runFirstRunChoice(context.Background(), bufio.NewReader(strings.NewReader("x\n2\n")), &out, dir)
	if err != nil || choice != choiceLAN {
		t.Fatalf("choice = %q, %v", choice, err)
	}
	if !strings.Contains(out.String(), "Type 1, 2, 3 or 4.") {
		t.Fatalf("invalid input was not explained: %q", out.String())
	}
	if saved, ok := loadStartupChoice(dir); !ok || saved != choiceLAN {
		t.Fatalf("saved = %q ok=%v", saved, ok)
	}
}

func TestEnrollmentRequestPrintKeepsControlSequencesOut(t *testing.T) {
	var out bytes.Buffer
	printEnrollmentRequest(&out, "", enrollment.Request{
		ID: "req_1", DeviceName: "Evil\x1b[2J", Origin: "https://x.test\x07", Number: "482",
		ExpiresAt: time.Now().Add(time.Minute),
	})
	text := out.String()
	if strings.ContainsAny(text, "\x1b\x07") {
		t.Fatalf("control characters reached the terminal: %q", text)
	}
	if !strings.Contains(text, "Number: 482") || !strings.Contains(text, "mewla devices approve -id req_1 -number 482") {
		t.Fatalf("missing number or command: %q", text)
	}
}
