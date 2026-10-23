package connections

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const (
	gmailPageDefault = 10
	gmailPageMax     = 25
	// A message body is cut here so one long mail cannot fill Brain's context;
	// read_message says when it did.
	gmailBodyLimit   = 20000
	gmailThreadBody  = 6000
	gmailThreadLimit = 30
	gmailMailLimit   = 100000
)

var gmailRead = []string{"gmail.readonly", "gmail.modify"}
var gmailWrite = []string{"gmail.modify"}

func gmailTools() []googleTool {
	address := text(320)
	recipients := list(address, 50)
	mailFields := map[string]any{
		"to":                  recipients,
		"cc":                  recipients,
		"bcc":                 recipients,
		"subject":             text(998),
		"body":                text(gmailMailLimit),
		"reply_to_message_id": text(128),
	}
	return []googleTool{
		{googleToolDef("search_messages", fmt.Sprintf("Search mail with Gmail's search syntax (from:, subject:, is:unread, newer_than:2d, label:, has:attachment). Returns up to %d messages per page with sender, subject, date and snippet; pass next_page_token for more", gmailPageMax), true, object(map[string]any{
			"query": text(1024), "max_results": intRange(1, gmailPageMax), "page_token": text(512), "include_spam_trash": map[string]any{"type": "boolean"},
		})), gmailRead, gmailSearch},
		{googleToolDef("read_message", "Read one message: headers, the body as readable text (long bodies are cut and flagged) and its attachment names", true, object(map[string]any{"message_id": text(128)}, "message_id")), gmailRead, gmailReadMessage},
		{googleToolDef("read_thread", fmt.Sprintf("Read a conversation: up to %d messages, oldest first, each with a shortened body", gmailThreadLimit), true, object(map[string]any{"thread_id": text(128)}, "thread_id")), gmailRead, gmailReadThread},
		{googleToolDef("list_labels", "List the mailbox's labels with their ids (system labels such as INBOX and UNREAD included)", true, object(map[string]any{})), gmailRead, gmailListLabels},
		{googleToolDef("create_draft", "Save a draft in Gmail without sending it. Give to, subject and body; set reply_to_message_id to draft a reply in that conversation", false, object(mailFields, "to", "body")), gmailWrite, func(c *googleCall, a map[string]any) (any, error) { return gmailCompose(c, a, false) }},
		{googleToolDef("send_message", "Send an email now. Only when the person asked for this exact message to be sent; otherwise create a draft", false, object(mailFields, "to", "body")), gmailWrite, func(c *googleCall, a map[string]any) (any, error) { return gmailCompose(c, a, true) }},
		{googleToolDef("send_draft", "Send an existing draft now. Only when the person asked for it to be sent", false, object(map[string]any{"draft_id": text(128)}, "draft_id")), gmailWrite, gmailSendDraft},
		{googleToolDef("update_labels", "Mark a message or a whole conversation read, archive it, or add and remove labels (names or ids from list_labels). Give message_id or thread_id", false, object(map[string]any{
			"message_id": text(128), "thread_id": text(128), "mark_read": map[string]any{"type": "boolean"}, "archive": map[string]any{"type": "boolean"},
			"add_labels": list(text(256), 20), "remove_labels": list(text(256), 20),
		})), gmailWrite, gmailUpdateLabels},
	}
}

// Message shapes.

type gmailPart struct {
	MimeType string `json:"mimeType"`
	Filename string `json:"filename"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		Size         int    `json:"size"`
		Data         string `json:"data"`
		AttachmentID string `json:"attachmentId"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

type gmailMessage struct {
	ID       string    `json:"id"`
	ThreadID string    `json:"threadId"`
	Labels   []string  `json:"labelIds"`
	Snippet  string    `json:"snippet"`
	Payload  gmailPart `json:"payload"`
}

func (p *gmailPart) header(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return decodeHeader(h.Value)
		}
	}
	return ""
}

var headerDecoder = &mime.WordDecoder{CharsetReader: func(label string, input io.Reader) (io.Reader, error) {
	return charset.NewReaderLabel(label, input)
}}

func decodeHeader(value string) string {
	if decoded, err := headerDecoder.DecodeHeader(value); err == nil {
		value = decoded
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, value))
}

func hasLabel(labels []string, name string) bool {
	return contains(labels, name)
}

// summary is the row shown for a search hit.
func (g *gmailMessage) summary() map[string]any {
	return map[string]any{
		"id": g.ID, "thread_id": g.ThreadID,
		"from": g.Payload.header("From"), "to": g.Payload.header("To"), "subject": g.Payload.header("Subject"), "date": g.Payload.header("Date"),
		"snippet": g.Snippet, "unread": hasLabel(g.Labels, "UNREAD"), "labels": g.Labels,
	}
}

func (g *gmailMessage) full(bodyLimit int) map[string]any {
	out := g.summary()
	out["cc"] = g.Payload.header("Cc")
	body, format := messageText(&g.Payload)
	body, cut := truncate(body, bodyLimit)
	out["body"], out["body_format"], out["body_truncated"] = body, format, cut
	attachments := []map[string]any{}
	collectAttachments(&g.Payload, &attachments)
	out["attachments"] = attachments
	return out
}

func collectAttachments(p *gmailPart, out *[]map[string]any) {
	if p.Filename != "" {
		*out = append(*out, map[string]any{"filename": p.Filename, "mime_type": p.MimeType, "size": p.Body.Size})
	}
	for i := range p.Parts {
		collectAttachments(&p.Parts[i], out)
	}
}

// messageText returns the readable body: the text/plain parts, or the HTML
// parts reduced to text when a message has none.
func messageText(root *gmailPart) (string, string) {
	var plain, markup []string
	var walk func(p *gmailPart)
	walk = func(p *gmailPart) {
		if p.Filename == "" && p.Body.Data != "" {
			switch {
			case strings.EqualFold(p.MimeType, "text/plain"):
				plain = append(plain, partText(p))
			case strings.EqualFold(p.MimeType, "text/html"):
				markup = append(markup, partText(p))
			}
		}
		for i := range p.Parts {
			walk(&p.Parts[i])
		}
	}
	walk(root)
	if len(plain) > 0 {
		return strings.TrimSpace(strings.Join(plain, "\n\n")), "text"
	}
	if len(markup) > 0 {
		return strings.TrimSpace(htmlText(strings.Join(markup, "\n"))), "html_as_text"
	}
	return "", "none"
}

// partText decodes a part's base64url data and converts it to UTF-8 from the
// charset its Content-Type names.
func partText(p *gmailPart) string {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(p.Body.Data, "="))
	if err != nil {
		return ""
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	_, params, _ := mime.ParseMediaType(p.header("Content-Type"))
	if reader, err := charset.NewReaderLabel(params["charset"], bytes.NewReader(raw)); err == nil {
		if converted, err := io.ReadAll(reader); err == nil {
			return string(converted)
		}
	}
	return strings.ToValidUTF8(string(raw), "�")
}

// htmlText keeps the words of an HTML mail: no scripts or styles, a line
// break after blocks, link addresses dropped.
func htmlText(markup string) string {
	var out strings.Builder
	skip := 0
	tokens := html.NewTokenizer(strings.NewReader(markup))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			break
		}
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, _ := tokens.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" || tag == "head" {
				if kind == html.StartTagToken {
					skip++
				} else if kind == html.EndTagToken && skip > 0 {
					skip--
				}
			}
			switch tag {
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "table", "blockquote":
				out.WriteString("\n")
			}
		case html.TextToken:
			if skip == 0 {
				out.WriteString(string(tokens.Text()))
			}
		}
	}
	lines := strings.Split(out.String(), "\n")
	var kept []string
	blank := 0
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// Reads.

func gmailSearch(c *googleCall, args map[string]any) (any, error) {
	query := url.Values{"maxResults": {fmt.Sprint(argInt(args, "max_results", gmailPageDefault))}}
	if q := argString(args, "query"); q != "" {
		query.Set("q", q)
	}
	if t := argString(args, "page_token"); t != "" {
		query.Set("pageToken", t)
	}
	if argBool(args, "include_spam_trash") {
		query.Set("includeSpamTrash", "true")
	}
	var page struct {
		Messages      []struct{ ID string } `json:"messages"`
		NextPageToken string                `json:"nextPageToken"`
		Estimate      int                   `json:"resultSizeEstimate"`
	}
	if err := c.do("", "GET", "/gmail/v1/users/me/messages", query, nil, &page); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, len(page.Messages))
	failures := make([]error, len(page.Messages))
	var wait sync.WaitGroup
	slots := make(chan struct{}, 5)
	for i, hit := range page.Messages {
		wait.Add(1)
		go func(i int, id string) {
			defer wait.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			headers := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "To", "Subject", "Date"}}
			var message gmailMessage
			if failures[i] = c.do("", "GET", "/gmail/v1/users/me/messages/"+pathID(id), headers, nil, &message); failures[i] == nil {
				rows[i] = message.summary()
			}
		}(i, hit.ID)
	}
	wait.Wait()
	for _, err := range failures {
		if err != nil {
			return nil, err
		}
	}
	out := map[string]any{"messages": rows, "result_size_estimate": page.Estimate}
	if page.NextPageToken != "" {
		out["next_page_token"] = page.NextPageToken
	}
	return out, nil
}

func gmailReadMessage(c *googleCall, args map[string]any) (any, error) {
	var message gmailMessage
	if err := c.do("", "GET", "/gmail/v1/users/me/messages/"+pathID(argString(args, "message_id")), url.Values{"format": {"full"}}, nil, &message); err != nil {
		return nil, err
	}
	return message.full(gmailBodyLimit), nil
}

func gmailReadThread(c *googleCall, args map[string]any) (any, error) {
	var thread struct {
		ID       string         `json:"id"`
		Messages []gmailMessage `json:"messages"`
	}
	if err := c.do("", "GET", "/gmail/v1/users/me/threads/"+pathID(argString(args, "thread_id")), url.Values{"format": {"full"}}, nil, &thread); err != nil {
		return nil, err
	}
	total := len(thread.Messages)
	// Keep the newest messages when a thread is longer than the cap.
	if total > gmailThreadLimit {
		thread.Messages = thread.Messages[total-gmailThreadLimit:]
	}
	rows := make([]map[string]any, 0, len(thread.Messages))
	for i := range thread.Messages {
		rows = append(rows, thread.Messages[i].full(gmailThreadBody))
	}
	return map[string]any{"thread_id": thread.ID, "message_count": total, "messages": rows, "older_messages_left_out": total - len(rows)}, nil
}

type gmailLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func gmailLabels(c *googleCall) ([]gmailLabel, error) {
	var labels struct {
		Labels []gmailLabel `json:"labels"`
	}
	err := c.do("", "GET", "/gmail/v1/users/me/labels", nil, nil, &labels)
	return labels.Labels, err
}

func gmailListLabels(c *googleCall, _ map[string]any) (any, error) {
	labels, err := gmailLabels(c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"labels": labels}, nil
}

// Writes.

// mailAddresses parses one recipient field. Parsing, not string joining, is
// what keeps a crafted value from adding headers.
func mailAddresses(field string, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%s: an address cannot contain a line break", field)
		}
		parsed, err := mail.ParseAddress(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an email address", field, value)
		}
		out = append(out, parsed.String())
	}
	return out, nil
}

type gmailDraft struct {
	to, cc, bcc              []string
	subject, body            string
	inReplyTo, references    string
	threadID                 string
	replySubjectFromOriginal bool
	originalSubject          string
}

func buildMail(d gmailDraft) (string, error) {
	if strings.ContainsAny(d.subject, "\r\n") {
		return "", errors.New("subject cannot contain a line break")
	}
	var out bytes.Buffer
	header := func(name, value string) {
		if value != "" {
			out.WriteString(name + ": " + value + "\r\n")
		}
	}
	header("To", strings.Join(d.to, ", "))
	header("Cc", strings.Join(d.cc, ", "))
	header("Bcc", strings.Join(d.bcc, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", d.subject))
	header("In-Reply-To", d.inReplyTo)
	header("References", d.references)
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="UTF-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	out.WriteString("\r\n")
	writer := quotedprintable.NewWriter(&out)
	body := strings.ReplaceAll(strings.ReplaceAll(d.body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := writer.Write([]byte(body)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(out.Bytes()), nil
}

func gmailPrepare(c *googleCall, args map[string]any) (gmailDraft, error) {
	var d gmailDraft
	var err error
	if d.to, err = mailAddresses("to", argStrings(args, "to")); err != nil {
		return d, err
	}
	if d.cc, err = mailAddresses("cc", argStrings(args, "cc")); err != nil {
		return d, err
	}
	if d.bcc, err = mailAddresses("bcc", argStrings(args, "bcc")); err != nil {
		return d, err
	}
	if len(d.to) == 0 {
		return d, errors.New("name at least one recipient in to")
	}
	d.subject, d.body = argString(args, "subject"), argString(args, "body")
	if id := argString(args, "reply_to_message_id"); id != "" {
		var original gmailMessage
		query := url.Values{"format": {"metadata"}, "metadataHeaders": {"Message-ID", "References", "Subject"}}
		if err = c.do("", "GET", "/gmail/v1/users/me/messages/"+pathID(id), query, nil, &original); err != nil {
			return d, err
		}
		d.threadID = original.ThreadID
		d.inReplyTo = original.Payload.header("Message-ID")
		d.references = strings.TrimSpace(original.Payload.header("References") + " " + d.inReplyTo)
		if d.subject == "" {
			subject := original.Payload.header("Subject")
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
			d.subject = subject
		}
	}
	if d.subject == "" {
		return d, errors.New("give a subject, or reply_to_message_id to reuse the conversation's")
	}
	return d, nil
}

func gmailCompose(c *googleCall, args map[string]any, send bool) (any, error) {
	d, err := gmailPrepare(c, args)
	if err != nil {
		return nil, err
	}
	raw, err := buildMail(d)
	if err != nil {
		return nil, err
	}
	message := map[string]any{"raw": raw}
	if d.threadID != "" {
		message["threadId"] = d.threadID
	}
	if send {
		var sent struct{ ID, ThreadID string }
		if err = c.do("", "POST", "/gmail/v1/users/me/messages/send", nil, message, &sent); err != nil {
			return nil, err
		}
		return map[string]any{"sent": true, "message_id": sent.ID, "thread_id": sent.ThreadID}, nil
	}
	var draft struct {
		ID      string
		Message struct{ ID, ThreadID string }
	}
	if err = c.do("", "POST", "/gmail/v1/users/me/drafts", nil, map[string]any{"message": message}, &draft); err != nil {
		return nil, err
	}
	return map[string]any{"sent": false, "draft_id": draft.ID, "message_id": draft.Message.ID, "thread_id": draft.Message.ThreadID}, nil
}

func gmailSendDraft(c *googleCall, args map[string]any) (any, error) {
	var sent struct{ ID, ThreadID string }
	if err := c.do("", "POST", "/gmail/v1/users/me/drafts/send", nil, map[string]any{"id": argString(args, "draft_id")}, &sent); err != nil {
		return nil, err
	}
	return map[string]any{"sent": true, "message_id": sent.ID, "thread_id": sent.ThreadID}, nil
}

func gmailUpdateLabels(c *googleCall, args map[string]any) (any, error) {
	message, thread := argString(args, "message_id"), argString(args, "thread_id")
	if (message == "") == (thread == "") {
		return nil, errors.New("give exactly one of message_id and thread_id")
	}
	add, remove := argStrings(args, "add_labels"), argStrings(args, "remove_labels")
	if argBool(args, "mark_read") {
		remove = append(remove, "UNREAD")
	}
	if argBool(args, "archive") {
		remove = append(remove, "INBOX")
	}
	if len(add)+len(remove) == 0 {
		return nil, errNoChange
	}
	labels, err := gmailLabels(c)
	if err != nil {
		return nil, err
	}
	resolve := func(names []string) ([]string, error) {
		ids := []string{}
		for _, name := range names {
			found := ""
			for _, label := range labels {
				if label.ID == name || strings.EqualFold(label.Name, name) {
					found = label.ID
					break
				}
			}
			if found == "" {
				return nil, fmt.Errorf("no label named %q; list_labels shows the names and ids", name)
			}
			ids = append(ids, found)
		}
		return ids, nil
	}
	body := map[string]any{}
	if body["addLabelIds"], err = resolve(add); err != nil {
		return nil, err
	}
	if body["removeLabelIds"], err = resolve(remove); err != nil {
		return nil, err
	}
	path := "/gmail/v1/users/me/messages/" + pathID(message) + "/modify"
	if thread != "" {
		path = "/gmail/v1/users/me/threads/" + pathID(thread) + "/modify"
	}
	if err = c.do("", "POST", path, nil, body, nil); err != nil {
		return nil, err
	}
	return map[string]any{"updated": true}, nil
}
