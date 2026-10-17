package work

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDSHNativeLogV4ProjectionAndReloadIdentity(t *testing.T) {
	rows := []string{
		`{"type":"session","version":4,"id":"session-fixture","cwd":"/fixture"}`,
		`{"type":"turn/start","seq":4,"time":1000,"data":{"turn":1}}`,
		`{"type":"step/start","seq":5,"time":1000,"data":{"turn":1,"step":1}}`,
		`{"type":"user/message","seq":6,"time":1001,"data":{"id":"user-1","role":"user","source":{"kind":"user","rpcId":"req-1"},"content":[{"type":"text","text":"inspect"}]}}`,
		`{"type":"user/message","seq":7,"time":1002,"data":{"id":"system-1","role":"user","source":{"kind":"time-context"},"content":[{"type":"text","text":"private runtime context"}]}}`,
		`{"type":"assistant/message","seq":8,"time":1006,"data":{"turn":1,"step":1,"message":{"role":"assistant","content":[{"type":"reasoning","text":"look first"},{"type":"text","text":"hello!"},{"type":"tool-call","id":"call-1","name":"read_file","arguments":"{}"}]},"stream":[]}}`,
		`{"type":"tool/call","seq":9,"time":1007,"data":{"turn":1,"step":1,"callId":"call-1","name":"read_file","arguments":"{}"}}`,
	}
	path := filepath.Join(t.TempDir(), dshSessionLogName)
	write := func() {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encoder, _ := zstd.NewWriter(file)
		for _, row := range rows {
			encoder.Write([]byte(row + "\n"))
		}
		encoder.Close()
		file.Close()
	}
	write()
	partial, err := parseDSHConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Events) != 4 || partial.Events[0].Body != "inspect" || partial.Events[1].Kind != "reasoning" || partial.Events[2].Body != "hello!" || partial.Events[3].Status != "running" {
		t.Fatalf("partial=%+v", partial.Events)
	}
	if partial.Activity == nil || partial.Activity.Status == ProviderActivityCompleted {
		t.Fatalf("open turn settled: %+v", partial.Activity)
	}
	rows = append(rows,
		`{"type":"tool/result","seq":10,"time":1008,"data":{"turn":1,"step":1,"message":{"role":"tool","toolCallId":"call-1","content":[{"type":"text","text":"result"},{"type":"image","attachment":{"attachmentId":"image-1"}}]}}}`,
		`{"type":"step/end","seq":11,"time":1009,"data":{"turn":1,"step":1}}`,
		`{"type":"turn/end","seq":12,"time":1009,"data":{"turn":1,"reason":{"kind":"completed"}}}`)
	write()
	complete, err := parseDSHConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Events[2].ID != partial.Events[2].ID || complete.Events[2].Seq != partial.Events[2].Seq {
		t.Fatal("reload identity changed")
	}
	if len(complete.Events) != 4 || complete.Events[3].CallID != "call-1" || complete.Events[3].Output != "result\n![Image](dsh-attachment:image-1)" || complete.Events[3].Partial || complete.Events[3].Status != "completed" {
		t.Fatalf("tool correlation=%+v", complete.Events)
	}
	if complete.Activity == nil || complete.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("activity=%+v", complete.Activity)
	}
	_, _ = json.Marshal(complete)
}

func TestDSHNativeLogV4SurfacesFailedTurn(t *testing.T) {
	rows := []string{
		`{"type":"session","version":4,"id":"session-failed","cwd":"/fixture"}`,
		`{"type":"turn/start","seq":4,"time":1000,"data":{"turn":1}}`,
		`{"type":"user/message","seq":8,"time":1001,"data":{"id":"user-1","role":"user","source":{"kind":"user","rpcId":"req-1"},"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant/attempt","seq":16,"time":1002,"data":{"turn":1,"step":1,"stream":[]}}`,
		`{"type":"turn/end","seq":18,"time":1003,"data":{"turn":1,"reason":{"kind":"error","error":{"message":"no API key for provider route","code":"MISSING_CREDENTIAL"}}}}`,
	}
	path := filepath.Join(t.TempDir(), dshSessionLogName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder, _ := zstd.NewWriter(file)
	for _, row := range rows {
		encoder.Write([]byte(row + "\n"))
	}
	encoder.Close()
	file.Close()
	conversation, err := parseDSHConversation(path)
	if err != nil {
		t.Fatal(err)
	}
	last := conversation.Events[len(conversation.Events)-1]
	if last.Kind != "status" || last.Status != "failed" || !strings.Contains(last.Body, "no API key") {
		t.Fatalf("events=%+v", conversation.Events)
	}
	if conversation.Activity == nil || conversation.Activity.Status != ProviderActivityFailed {
		t.Fatalf("activity=%+v", conversation.Activity)
	}
}

func TestDSHRejectsOtherLogGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), dshSessionLogName)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder, _ := zstd.NewWriter(file)
	encoder.Write([]byte(`{"type":"session","version":3,"id":"session-old","cwd":"/fixture"}` + "\n"))
	encoder.Close()
	file.Close()
	if _, err := parseDSHConversation(path); err == nil {
		t.Fatal("older log generation accepted")
	}
}

func TestDSHLaunchAndLockOwnership(t *testing.T) {
	t.Setenv("MEWLA_STATE_DIR", t.TempDir())
	command, err := EnsureDSHSessionLaunchCommand("dsh")
	if err != nil {
		t.Fatal(err)
	}
	if InferWorkerProvider(command) != WorkerProviderDSH || DSHSessionID(command) == "" {
		t.Fatalf("command=%s", command)
	}
	again, err := EnsureDSHSessionLaunchCommand(command)
	if err != nil || again != command {
		t.Fatal("launch ownership changed")
	}
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := lockDSHSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if release, err := lockDSHSession(path); err == nil {
		release()
		t.Fatal("duplicate owner admitted")
	}
	unlock()
	release, err := lockDSHSession(path)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestDSHNativeImageIntakeRejectsForeignPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEWLA_STATE_DIR", root)
	if err := os.MkdirAll(filepath.Join(root, "uploads"), 0700); err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile("../../app/assets/reading-fixture/normal.png")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "uploads", "photo.png")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	body := "<mewla_attachments>{\"files\":[{\"name\":\"photo.png\",\"path\":" + strconv.Quote(path) + "}]}</mewla_attachments>"
	blocks, err := dshPromptContent(body)
	if err != nil || len(blocks) != 2 || blocks[1]["type"] != "image" {
		t.Fatalf("native intake: %v %d", err, len(blocks))
	}
	outside := filepath.Join(t.TempDir(), "foreign.png")
	if err := os.WriteFile(outside, image, 0600); err != nil {
		t.Fatal(err)
	}
	foreign := strings.Replace(body, path, outside, 1)
	if _, err := dshPromptContent(foreign); err == nil {
		t.Fatal("foreign path acquired image authority")
	}
	link := filepath.Join(root, "uploads", "escape.png")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := dshPromptContent(strings.Replace(body, path, link, 1)); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestDSHPersistedImageReplacesPhoneUploadReference(t *testing.T) {
	text := `photo <mewla_attachments>{"files":[{"name":"photo","path":"/uploads/temporary","content_type":"image/png"}]}</mewla_attachments>`
	var blocks []dshBlock
	encoded := `[{"type":"text","text":` + strconv.Quote(text) + `},{"type":"image","attachment":{"attachmentId":"durable-image","mediaType":"image/png","name":"photo"}}]`
	if err := json.Unmarshal([]byte(encoded), &blocks); err != nil {
		t.Fatal(err)
	}
	display, exact := dshUserContent(blocks)
	if exact != text || strings.Contains(display, "/uploads/temporary") || !strings.Contains(display, "dsh-attachment:durable-image") || strings.Contains(display, "![Image]") {
		t.Fatalf("unexpected projection %q", display)
	}
}
