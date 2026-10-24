package providerpaths

import (
	"path/filepath"
	"testing"
)

func TestCodexHomeHonoursCODEXHOME(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	if got := CodexStateDB("/home/u"); got != filepath.Join("/home/u", ".codex", "state_5.sqlite") {
		t.Fatalf("default = %s", got)
	}
	t.Setenv("CODEX_HOME", "/elsewhere/codex")
	if got := CodexStateDB("/home/u"); got != "/elsewhere/codex/state_5.sqlite" {
		t.Fatalf("override = %s", got)
	}
}
