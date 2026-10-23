package attachment

import (
	"encoding/json"
	"regexp"
	"strings"
)

const (
	envelopeOpenTag  = "<mewla_attachments>"
	envelopeCloseTag = "</mewla_attachments>"
)

// envelopePattern is the one reader of the in-band envelope the phone app and
// Telegram append to a message; app/components/terminal/attachmentEnvelope.ts
// is its counterpart.
var envelopePattern = regexp.MustCompile(`(?s)` + envelopeOpenTag + `\s*(.*?)\s*` + envelopeCloseTag)

// Entity offsets are UTF-16 units in Text, never in the serialized envelope.
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

// Envelope extends the existing mobile mewla_attachments contract. Providers
// receive local file references through their normal receipt-owned input.
type Envelope struct {
	Files    []File    `json:"files"`
	Captions []Caption `json:"captions,omitempty"`
}

func Input(body string, files []File, captions []Caption) string {
	if len(files) == 0 && len(captions) == 0 {
		return body
	}
	return strings.TrimSpace(body) + "\n\n" + Format(Envelope{Files: files, Captions: captions})
}

// Format is the envelope as appended to a message body.
func Format(envelope Envelope) string {
	raw, _ := json.Marshal(envelope)
	return envelopeOpenTag + string(raw) + envelopeCloseTag
}

// Match is the first envelope found in a message: text[Start:End] is the whole
// tagged span and Raw its JSON.
type Match struct {
	Start, End int
	Raw        string
}

func Find(text string) (Match, bool) {
	loc := envelopePattern.FindStringSubmatchIndex(text)
	if loc == nil {
		return Match{}, false
	}
	return Match{Start: loc[0], End: loc[1], Raw: text[loc[2]:loc[3]]}, true
}

func (m Match) Decode() (Envelope, error) {
	var envelope Envelope
	err := json.Unmarshal([]byte(m.Raw), &envelope)
	return envelope, err
}

// WithoutEnvelope is the message as the person wrote it.
func WithoutEnvelope(text string) string {
	match, ok := Find(text)
	if !ok {
		return text
	}
	return strings.TrimSpace(text[:match.Start] + text[match.End:])
}
