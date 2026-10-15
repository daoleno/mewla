package computers

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Handoff is a message this computer's Brain sent to another computer, as
// its conversation shows it.
type Handoff struct {
	ID        string
	Computer  string
	DaemonID  string
	Kind      string
	ReplyTo   string
	UserSaid  string
	Text      string
	State     string
	CreatedAt time.Time
}

// Handoffs lists the messages sent from one Brain conversation.
func (m *Manager) Handoffs(thread string) []Handoff {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := make(map[string]string, len(m.state.Outbox))
	for _, item := range m.state.Outbox {
		live[item.Message.ID] = item.State
	}
	var out []Handoff
	for _, h := range m.state.Handoffs {
		if h.Thread != thread {
			continue
		}
		state := h.State
		if current, ok := live[h.ID]; ok {
			state = current
		}
		out = append(out, Handoff{
			ID: h.ID, Computer: h.Computer, DaemonID: m.daemonIDLocked(h.PeerKey), Kind: h.Kind,
			ReplyTo: h.ReplyTo, UserSaid: h.UserSaid, Text: h.Text, State: state, CreatedAt: h.CreatedAt,
		})
	}
	return out
}

// DaemonIDForReceipt names the computer a delivered message came from, by
// the receipt it was delivered with: its daemon id, or the start of it when
// that computer has not said hello since this one started.
func (m *Manager) DaemonIDForReceipt(receipt string) string {
	rest, ok := strings.CutPrefix(receipt, "computer:")
	if !ok {
		return ""
	}
	direction, rest, _ := strings.Cut(rest, ":")
	id, _, _ := strings.Cut(rest, ":")
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.daemonIDLocked(direction + ":" + id)
}

func (m *Manager) daemonIDLocked(key string) string {
	if id, ok := strings.CutPrefix(key, "out:"); ok {
		return id
	}
	if hello, ok := m.hellos[key]; ok && hello.DaemonID != "" {
		return hello.DaemonID
	}
	device, _ := strings.CutPrefix(key, "in:")
	prefix, _ := strings.CutPrefix(device, "computer-")
	return prefix
}

// ParsedEnvelope is a message from another computer, read back from the
// text its Brain was given.
type ParsedEnvelope struct {
	From, ID, Kind, ReplyTo, UserSaid, Text string
}

var (
	envelopeOpen = regexp.MustCompile(`^<mewla_computer_message((?: [a-z_]+="[^"<>]*")*)>\n`)
	envelopeAttr = regexp.MustCompile(`([a-z_]+)="([^"<>]*)"`)
)

// ParseEnvelope reads text written by Envelope.
func ParseEnvelope(body string) (ParsedEnvelope, bool) {
	body = strings.TrimSpace(body)
	open := envelopeOpen.FindStringSubmatchIndex(body)
	if open == nil {
		return ParsedEnvelope{}, false
	}
	var parsed ParsedEnvelope
	for _, attr := range envelopeAttr.FindAllStringSubmatch(body[open[2]:open[3]], -1) {
		switch attr[1] {
		case "from":
			parsed.From = attr[2]
		case "id":
			parsed.ID = attr[2]
		case "kind":
			parsed.Kind = attr[2]
		case "reply_to":
			parsed.ReplyTo = attr[2]
		}
	}
	inner, _, ok := strings.Cut(body[open[1]:], "\n</mewla_computer_message>")
	if !ok || parsed.ID == "" {
		return ParsedEnvelope{}, false
	}
	if said, ok := strings.CutPrefix(inner, "<user_said>"); ok {
		if words, rest, ok := strings.Cut(said, "</user_said>\n"); ok {
			parsed.UserSaid, inner = unclean(words), rest
		}
	}
	parsed.Text = unclean(inner)
	return parsed, true
}

func unclean(value string) string {
	value = strings.ReplaceAll(value, "&lt;/mewla_computer_message", "</mewla_computer_message")
	value = strings.ReplaceAll(value, "&lt;mewla_computer_message", "<mewla_computer_message")
	value = strings.ReplaceAll(value, "&lt;/user_said", "</user_said")
	return strings.TrimSpace(value)
}

func clip(text string) string {
	if len(text) <= handoffText {
		return text
	}
	cut := handoffText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
