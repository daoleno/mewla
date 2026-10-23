package codexctl

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeModelCodex answers model/list with one page per cursor.
func fakeModelCodex(t *testing.T, first, second string) string {
	t.Helper()
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"id":%s,"result":{"userAgent":"fake"}}\n' "$id" ;;
    *'"method":"model/list"'*'"cursor":"page2"'*) printf '{"id":%s,"result":%s}\n' "$id" '` + second + `' ;;
    *'"method":"model/list"'*) printf '{"id":%s,"result":%s}\n' "$id" '` + first + `' ;;
  esac
done
`
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadModelListFollowsPagesAndDropsHiddenModels(t *testing.T) {
	binary := fakeModelCodex(t,
		`{"data":[{"id":"a","model":"gpt-a","displayName":"GPT-A","hidden":false,"defaultReasoningEffort":"low","supportedReasoningEfforts":[{"reasoningEffort":"low","description":"x"},{"reasoningEffort":"high","description":"y"}],"inputModalities":["text","image"]},{"id":"h","model":"gpt-hidden","hidden":true}],"nextCursor":"page2"}`,
		`{"data":[{"id":"b","model":"","displayName":"B","hidden":false,"supportedReasoningEfforts":[],"inputModalities":["text"]}],"nextCursor":null}`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := ReadModelList(ctx, binary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	a := models[0]
	if a.ID != "gpt-a" || a.DisplayName != "GPT-A" || a.DefaultEffort != "low" || strings.Join(a.Efforts, ",") != "low,high" || strings.Join(a.Modalities, ",") != "text,image" {
		t.Fatalf("a = %+v", a)
	}
	if models[1].ID != "b" || len(models[1].Efforts) != 0 {
		t.Fatalf("second page = %+v", models[1])
	}
}

func TestReadModelListSurfacesCodexRefusalAndMissingBinary(t *testing.T) {
	script := "#!/bin/sh\nwhile IFS= read -r line; do\n id=$(printf '%s' \"$line\" | sed -n 's/.*\"id\":\\([0-9]*\\).*/\\1/p')\n case \"$line\" in\n  *'\"method\":\"initialize\"'*) printf '{\"id\":%s,\"result\":{}}\\n' \"$id\" ;;\n  *'\"method\":\"model/list\"'*) printf '{\"id\":%s,\"error\":{\"code\":-32601,\"message\":\"method not found\"}}\\n' \"$id\" ;;\n esac\ndone\n"
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := ReadModelList(ctx, path, nil); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ReadModelList(ctx, filepath.Join(t.TempDir(), "absent"), nil); err == nil {
		t.Fatal("a missing binary listed models")
	}
}

// Opt-in proof against the installed Codex, in an empty CODEX_HOME so nothing
// of the user's is read or written:
//
//	MEWLA_CODEX_LIVE_MODELS=1 go test ./codexctl -run TestLiveModelList -count=1 -v
func TestLiveModelList(t *testing.T) {
	if os.Getenv("MEWLA_CODEX_LIVE_MODELS") == "" {
		t.Skip("set MEWLA_CODEX_LIVE_MODELS=1 for the installed-Codex model/list proof")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("codex not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	env := append(os.Environ(), "CODEX_HOME="+t.TempDir())
	models, err := ReadModelList(ctx, binary, env)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("the installed Codex lists no models")
	}
	for _, model := range models {
		if model.ID == "" || len(model.Efforts) == 0 {
			t.Fatalf("incomplete model %+v", model)
		}
	}
	t.Logf("%d models, first %+v", len(models), models[0])
}
