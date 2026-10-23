package codexctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCodex is a stand-in for `codex app-server --listen stdio://`: it answers
// each request line with the canned result for its method.
func fakeCodex(t *testing.T, limits string) string {
	t.Helper()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"id":%s,"result":{"userAgent":"fake"}}\n' "$id" ;;
    *'"method":"account/rateLimits/read"'*) printf '{"id":%s,"result":%s}\n' "$id" '` + limits + `' ;;
  esac
done
`
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadAccountUsageChatGPT(t *testing.T) {
	binary := fakeCodex(t,
		`{"rateLimits":{"planType":"plus","primary":{"usedPercent":42,"windowDurationMins":300,"resetsAt":1795266347},"secondary":{"usedPercent":7,"windowDurationMins":10080,"resetsAt":null}}}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	usage, err := ReadAccountUsage(ctx, binary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Plan != "plus" {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.Primary == nil || usage.Primary.UsedPercent != 42 || usage.Primary.WindowMinutes != 300 || usage.Primary.ResetsAt.Unix() != 1795266347 {
		t.Fatalf("primary = %+v", usage.Primary)
	}
	if usage.Secondary == nil || usage.Secondary.UsedPercent != 7 || !usage.Secondary.ResetsAt.IsZero() {
		t.Fatalf("secondary = %+v", usage.Secondary)
	}
}

func TestReadAccountUsageSurfacesCodexRefusal(t *testing.T) {
	script := "#!/bin/sh\nwhile IFS= read -r line; do\n id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p')\n case \"$line\" in\n  *'\"method\":\"initialize\"'*) printf '{\"id\":%s,\"result\":{}}\\n' \"$id\" ;;\n  *'\"method\":\"account/rateLimits/read\"'*) printf '{\"id\":%s,\"error\":{\"code\":-32600,\"message\":\"chatgpt authentication required\"}}\\n' \"$id\" ;;\n esac\ndone\n"
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := ReadAccountUsage(ctx, path, nil); err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadAccountUsageReportsAMissingBinary(t *testing.T) {
	_, err := ReadAccountUsage(context.Background(), filepath.Join(t.TempDir(), "absent"), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
}
