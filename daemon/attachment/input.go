package attachment

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const (
	envelopeOpenTag  = "<mewla_attachments>"
	envelopeCloseTag = "</mewla_attachments>"
)

// envelopePattern is the one reader of the provider-input rendering. Providers
// (and the tmux terminal a DSH Session reads) can only be handed text, so they
// echo the rendering back in their transcripts; Split turns that echo into the
// typed fields again. Nothing else in Mewla reads the tag.
var envelopePattern = regexp.MustCompile(`(?s)` + envelopeOpenTag + `\s*(.*?)\s*` + envelopeCloseTag)

// ErrMalformedRendering reports a tag whose JSON cannot be read. The text is
// then left exactly as it was.
var ErrMalformedRendering = errors.New("attachment rendering is malformed")

// Entity offsets are UTF-16 units in Text, never in the rendered JSON.
type Entity struct {
	Type          string      `json:"type"`
	Offset        int         `json:"offset"`
	Length        int         `json:"length"`
	URL           string      `json:"url,omitempty"`
	Language      string      `json:"language,omitempty"`
	CustomEmojiID string      `json:"custom_emoji_id,omitempty"`
	User          *EntityUser `json:"user,omitempty"`
}

type EntityUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Caption struct {
	Text     string   `json:"text"`
	Entities []Entity `json:"entities,omitempty"`
}

// Message is one person's input as typed fields: what they wrote, the files
// staged in the upload store, and (Telegram) the captions those files carried.
// Senders hand the daemon this shape; Render is the only place that turns it
// into the text a provider receives.
type Message struct {
	Text     string
	Files    []File
	Captions []Caption
}

type rendering struct {
	Files    []File    `json:"files"`
	Captions []Caption `json:"captions,omitempty"`
}

// Render is the provider input: the trimmed text, then a blank line and the
// attachment tag when there are files or captions. With nothing attached it is
// just the trimmed text. Every provider and every sender receives this form.
func (m Message) Render() string {
	body := strings.TrimSpace(untagged(m.Text))
	if len(m.Files) == 0 && len(m.Captions) == 0 {
		return body
	}
	files := m.Files
	if files == nil {
		files = []File{}
	}
	raw, _ := json.Marshal(rendering{Files: files, Captions: m.Captions})
	tag := envelopeOpenTag + string(raw) + envelopeCloseTag
	if body == "" {
		return tag
	}
	return body + "\n\n" + tag
}

// untagged removes the rendering's own tags from a sender's text. Files reach a
// provider only through Message.Files, which the daemon has confined to the
// upload store; text that carried its own tag would name any path it liked.
func untagged(text string) string {
	if !strings.Contains(text, envelopeOpenTag) && !strings.Contains(text, envelopeCloseTag) {
		return text
	}
	return strings.NewReplacer(envelopeOpenTag, "", envelopeCloseTag, "").Replace(text)
}

// Split reads a provider's echo of Render back into typed fields. Text without
// a tag, or with a tag it cannot read, comes back unchanged (the latter with
// ErrMalformedRendering).
func Split(text string) (Message, error) {
	loc := envelopePattern.FindStringSubmatchIndex(text)
	if loc == nil {
		return Message{Text: text}, nil
	}
	var parsed rendering
	if err := json.Unmarshal([]byte(text[loc[2]:loc[3]]), &parsed); err != nil {
		return Message{Text: text}, ErrMalformedRendering
	}
	return Message{
		Text:     strings.TrimSpace(text[:loc[0]] + text[loc[1]:]),
		Files:    parsed.Files,
		Captions: parsed.Captions,
	}, nil
}
