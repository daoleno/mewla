package translate

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Codex keeps a reasoning item's encrypted_content and sends it back on the
// next turn. Anthropic accepts a thinking block back only with the signature
// it gave, and refuses a tool-use turn with thinking on when the block is
// missing. So Anthropic's thinking block (its text and signature, or a
// redacted block) travels in encrypted_content under Mewla's own prefix, and
// is put back as the block it was when the conversation goes to Anthropic
// again. Sent to any other API it is dropped.
const anthropicSealPrefix = "mewla.anthropic-thinking.v1:"

type sealedThinking struct {
	Text      string `json:"t,omitempty"`
	Signature string `json:"s,omitempty"`
	Redacted  string `json:"r,omitempty"`
}

func sealAnthropicThinking(text, signature, redacted string) string {
	raw, _ := json.Marshal(sealedThinking{Text: text, Signature: signature, Redacted: redacted})
	return anthropicSealPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// unsealAnthropicThinking reads what sealAnthropicThinking wrote.
func unsealAnthropicThinking(sealed string) (signature string, thinking Part, ok bool) {
	rest, found := strings.CutPrefix(sealed, anthropicSealPrefix)
	if !found {
		return "", Part{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return "", Part{}, false
	}
	var s sealedThinking
	if json.Unmarshal(raw, &s) != nil || (s.Signature == "" && s.Redacted == "") {
		return "", Part{}, false
	}
	return s.Signature, Part{Kind: PartThinking, Text: s.Text, Redacted: s.Redacted}, true
}
