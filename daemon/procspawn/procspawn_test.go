package procspawn

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandCountsByLabel(t *testing.T) {
	before := Snapshot()
	_ = Command("test one", "true")
	_ = Command("test one", "true")
	_ = Command("test two", "true")
	delta := Delta(before, Snapshot())
	if delta["test one"] != 2 || delta["test two"] != 1 || Total(delta) != 3 {
		t.Fatalf("delta = %v", delta)
	}
}

func TestHandlerListsLabelsLargestFirst(t *testing.T) {
	for i := 0; i < 3; i++ {
		_ = Command("handler big", "true")
	}
	_ = Command("handler small", "true")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/debug/spawns", nil))
	body := rec.Body.String()
	big, small := strings.Index(body, "handler big"), strings.Index(body, "handler small")
	if big < 0 || small < 0 || big > small {
		t.Fatalf("body = %s", body)
	}
}

func TestSystemToolFindsOSToolsWithoutPATH(t *testing.T) {
	t.Setenv("PATH", "")
	if got := SystemTool("sh"); got != "/bin/sh" && got != "/usr/bin/sh" {
		t.Fatalf("SystemTool(sh) = %q with an empty PATH, want a system directory path", got)
	}
	if got := SystemTool("mewla-no-such-tool"); got != "mewla-no-such-tool" {
		t.Fatalf("a missing tool should come back unchanged, got %q", got)
	}
}
