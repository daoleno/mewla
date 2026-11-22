package work

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The same uploaded photo reaches DSH and agy as each one's own image block:
// DSH reads mediaType (and the file's name), ACP reads mimeType.
func TestUploadedImageBlockUsesEachProtocolsMediaKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEWLA_STATE_DIR", root)
	uploads := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploads, 0700); err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile("../testdata/reading-normal.png")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(uploads, "photo.png")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	body := "look <mewla_attachments>{\"files\":[{\"name\":\"photo.png\",\"path\":" + strconv.Quote(path) + "}]}</mewla_attachments>"

	dsh, err := dshPromptContent(body)
	if err != nil || len(dsh) != 2 {
		t.Fatalf("dsh blocks: %v %v", err, dsh)
	}
	if block := dsh[1]; block["type"] != "image" || block["mediaType"] != "image/png" || block["name"] != "photo.png" || block["data"] == "" {
		t.Fatalf("dsh image block = %v", keysOf(block))
	}
	if _, ok := dsh[1]["mimeType"]; ok {
		t.Fatal("dsh image block carries ACP's mimeType")
	}

	acp, err := acpPromptContent(body, uploads)
	if err != nil || len(acp) != 2 {
		t.Fatalf("acp blocks: %v %v", err, acp)
	}
	if block := acp[1]; block["type"] != "image" || block["mimeType"] != "image/png" || block["data"] == "" {
		t.Fatalf("acp image block = %v", keysOf(block))
	}
	if _, ok := acp[1]["mediaType"]; ok {
		t.Fatal("acp image block carries DSH's mediaType")
	}
}

func keysOf(block map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range block {
		if key == "data" {
			value = len(value.(string))
		}
		out[key] = value
	}
	return out
}
