package attachment

import (
	"strings"
	"testing"
)

func TestEnvelopeRoundTripsThroughInputFindAndWithoutEnvelope(t *testing.T) {
	files := []File{{Name: "photo.png", Path: "/uploads/photo.png", ContentType: "image/png"}}
	body := Input("  look at this ", files, []Caption{{Text: "caption <b>"}})
	if !strings.HasPrefix(body, "look at this\n\n<mewla_attachments>") || !strings.HasSuffix(body, "</mewla_attachments>") {
		t.Fatalf("body = %q", body)
	}
	match, ok := Find(body)
	if !ok {
		t.Fatal("envelope not found")
	}
	envelope, err := match.Decode()
	if err != nil || len(envelope.Files) != 1 || envelope.Files[0] != files[0] || envelope.Captions[0].Text != "caption <b>" {
		t.Fatalf("envelope = %+v, err=%v", envelope, err)
	}
	if got := body[:match.Start] + Format(envelope) + body[match.End:]; got != body {
		t.Fatalf("Format does not reproduce the span:\n%q\n%q", got, body)
	}
	if got := WithoutEnvelope(body); got != "look at this" {
		t.Fatalf("WithoutEnvelope = %q", got)
	}
}

func TestEnvelopeReadsLooseSpacingAndIgnoresOtherText(t *testing.T) {
	text := "hello <mewla_attachments>\n  {\"files\":[]}\n</mewla_attachments> tail"
	match, ok := Find(text)
	if !ok || match.Raw != `{"files":[]}` {
		t.Fatalf("match = %+v ok=%v", match, ok)
	}
	if got := WithoutEnvelope(text); got != "hello  tail" {
		t.Fatalf("WithoutEnvelope = %q", got)
	}
	for _, plain := range []string{"no envelope", "<mewla_attachments>unclosed", ""} {
		if _, ok := Find(plain); ok || WithoutEnvelope(plain) != plain {
			t.Fatalf("%q was treated as an envelope", plain)
		}
	}
	bad, ok := Find("<mewla_attachments>{broken</mewla_attachments>")
	if !ok {
		t.Fatal("malformed envelope not found")
	}
	if _, err := bad.Decode(); err == nil {
		t.Fatal("malformed envelope decoded")
	}
}
