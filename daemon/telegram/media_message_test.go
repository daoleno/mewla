package telegram

import (
	"encoding/json"
	"testing"

	"github.com/daoleno/mewla/daemon/attachment"
)

// A staged batch keeps text, files and captions as typed fields in the
// durable journal; the provider input is rendered from them only when the
// batch is admitted, and a text-only follow-up is staged without files.
func TestMediaInputKeepsTypedFieldsAndRendersOnce(t *testing.T) {
	input := mediaInput{
		ID:       "update:2",
		Text:     "Replying to: hi\n\nlook",
		Files:    []attachment.File{{Name: "a.png", Path: "/uploads/a.png", ContentType: "image/png", Size: 3, Kind: "photo"}},
		Captions: []attachment.Caption{{Text: "look"}},
	}
	if !input.staged() || (mediaInput{ID: "update:3"}).staged() || !(mediaInput{TextOnly: true}).staged() {
		t.Fatal("staged state is wrong")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var restored mediaInput
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	want := "Replying to: hi\n\nlook\n\n<mewla_attachments>{\"files\":[{\"name\":\"a.png\",\"path\":\"/uploads/a.png\",\"content_type\":\"image/png\",\"size\":3,\"kind\":\"photo\"}],\"captions\":[{\"text\":\"look\"}]}</mewla_attachments>"
	if got := restored.message().Render(); got != want {
		t.Fatalf("rendered input = %q\nwant %q", got, want)
	}
}
