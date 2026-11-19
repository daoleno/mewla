package agentuse

import (
	"os/exec"
	"strings"
	"testing"
)

// The extension refuses an agent's wipe of every site's cookies in the shared
// browser, and its removal of another site's cookies or data (2026-11-18: one
// Worker's cleanup signed the person out of every site).
func TestSiteDataGuard(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	out, err := exec.Command(node, "--no-warnings", "testdata/sitedata_test.mjs", "extension/sitedata.js").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("sitedata_test.mjs: %v\n%s", err, out)
	}
}
