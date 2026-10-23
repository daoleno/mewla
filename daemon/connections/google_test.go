package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type googleCallLog struct {
	method, host, path string
	query              url.Values
	body               map[string]any
}

// googleAccount is a connected Google account on a manager whose network is
// the given fake. It returns the manager, the account id and the call log.
func googleAccount(t *testing.T, kind string, scopes []string, writes bool, fake func(googleCallLog) (int, string)) (*Manager, string, *[]googleCallLog) {
	t.Helper()
	var log []googleCallLog
	m := testManager(t, func(req *http.Request) (*http.Response, error) {
		entry := googleCallLog{method: req.Method, host: req.URL.Host, path: req.URL.Path, query: req.URL.Query()}
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &entry.body)
		}
		if req.Header.Get("Authorization") != "Bearer google-access" {
			t.Errorf("wrong bearer on %s", req.URL)
		}
		log = append(log, entry)
		if entry.path == "/v1/userinfo" {
			return reply(200, `{"sub":"1","email":"me@example.test"}`), nil
		}
		status, body := fake(entry)
		return reply(status, body), nil
	})
	_, endpoint, _ := googleService(kind)
	r := &record{Account: Account{ID: "acct", Integration: kind, Endpoint: endpoint, Enabled: true, Status: "connected", AuthMethod: "oauth", Scopes: scopes, History: []Event{}}, OAuth: &oauthAccount{GoogleExchange: "https://exchange.example"}, Grants: map[string]string{}}
	if err := m.discover(context.Background(), r, "google-access"); err != nil {
		t.Fatal(err)
	}
	m.setGroup(r, "read", true)
	if writes {
		m.setGroup(r, "write", true)
	}
	raw, _ := json.Marshal(oauthSecret{Token: &oauth2.Token{AccessToken: "google-access", Expiry: time.Now().Add(time.Hour)}})
	if err := m.vault.Set("integration:acct", string(raw)); err != nil {
		t.Fatal(err)
	}
	m.records["acct"] = r
	log = nil
	return m, "acct", &log
}

func invokeGoogleTool(t *testing.T, m *Manager, tool string, args string) (map[string]any, error) {
	t.Helper()
	response, err := m.Handle(context.Background(), Request{Action: "invoke", ID: "acct", Tool: tool, Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(response.Result, &out); err != nil {
		t.Fatalf("%s result: %v", tool, err)
	}
	return out, nil
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func toolNames(m *Manager) map[string]bool {
	out := map[string]bool{}
	for _, tool := range m.records["acct"].Account.Tools {
		out[tool.Name] = true
	}
	return out
}

func TestGoogleScopesDecideTheTools(t *testing.T) {
	none := func(googleCallLog) (int, string) { return 404, `{}` }
	cases := []struct {
		kind    string
		scopes  []string
		offered []string
		absent  []string
	}{
		{googleCalendarID, googleScopes(googleCalendarID, false), []string{"get_me", "list_calendars", "list_events", "get_event", "check_availability"}, []string{"create_event", "update_event", "delete_event", "respond_to_event", "search_messages"}},
		{googleCalendarID, googleScopes(googleCalendarID, true), []string{"list_events", "create_event", "update_event", "delete_event", "respond_to_event"}, []string{"search_messages", "send_message"}},
		{gmailID, googleScopes(gmailID, false), []string{"get_me", "search_messages", "read_message", "read_thread", "list_labels"}, []string{"create_draft", "send_message", "send_draft", "update_labels", "list_events"}},
		{gmailID, googleScopes(gmailID, true), []string{"search_messages", "create_draft", "send_message", "send_draft", "update_labels"}, []string{"list_events", "create_event"}},
	}
	for _, c := range cases {
		m, _, _ := googleAccount(t, c.kind, c.scopes, false, none)
		names := toolNames(m)
		for _, name := range c.offered {
			if !names[name] {
				t.Errorf("%s %v: %s missing", c.kind, c.scopes, name)
			}
		}
		for _, name := range c.absent {
			if names[name] {
				t.Errorf("%s %v: %s offered", c.kind, c.scopes, name)
			}
		}
	}
	// A read-only sign-in's write consent is still to be asked for.
	m, _, _ := googleAccount(t, gmailID, googleScopes(gmailID, false), false, none)
	if !writeConsent(m.records["acct"]) {
		t.Fatal("read-only Gmail does not ask for write consent")
	}
}

func TestGoogleWritesNeedTheirSwitch(t *testing.T) {
	m, _, log := googleAccount(t, gmailID, googleScopes(gmailID, true), false, func(googleCallLog) (int, string) { return 200, `{}` })
	// Signed in with writes, but the switch is off: nothing is sent.
	if _, err := invokeGoogleTool(t, m, "send_message", `{"to":["a@example.test"],"subject":"s","body":"b"}`); err == nil {
		t.Fatal("ungranted send ran")
	}
	if len(*log) != 0 {
		t.Fatalf("ungranted write reached Google: %v", *log)
	}
}

func TestGmailSearchReturnsReadableRows(t *testing.T) {
	m, _, log := googleAccount(t, gmailID, googleScopes(gmailID, false), false, func(c googleCallLog) (int, string) {
		switch c.path {
		case "/gmail/v1/users/me/messages":
			return 200, `{"messages":[{"id":"m1","threadId":"t1"},{"id":"m2","threadId":"t2"}],"nextPageToken":"next","resultSizeEstimate":40}`
		case "/gmail/v1/users/me/messages/m1":
			return 200, `{"id":"m1","threadId":"t1","labelIds":["INBOX","UNREAD"],"snippet":"hello there","payload":{"headers":[{"name":"From","value":"=?UTF-8?B?5byg5LiJ?= <zhang@example.test>"},{"name":"Subject","value":"=?UTF-8?Q?Caf=C3=A9_menu?="},{"name":"Date","value":"Fri, 23 Oct 2026 09:00:00 +0800"}]}}`
		case "/gmail/v1/users/me/messages/m2":
			return 200, `{"id":"m2","threadId":"t2","labelIds":["INBOX"],"snippet":"second","payload":{"headers":[{"name":"From","value":"b@example.test"},{"name":"Subject","value":"Two"}]}}`
		}
		return 404, `{}`
	})
	out, err := invokeGoogleTool(t, m, "search_messages", `{"query":"from:zhang is:unread","max_results":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rows := out["messages"].([]any)
	first := rows[0].(map[string]any)
	if first["from"] != "张三 <zhang@example.test>" || first["subject"] != "Café menu" || first["unread"] != true || first["snippet"] != "hello there" {
		t.Fatalf("row not readable: %v", first)
	}
	if rows[1].(map[string]any)["id"] != "m2" || out["next_page_token"] != "next" {
		t.Fatalf("order or paging lost: %v", out)
	}
	list := (*log)[0]
	if list.query.Get("q") != "from:zhang is:unread" || list.query.Get("maxResults") != "2" {
		t.Fatalf("search not passed: %v", list.query)
	}
}

func TestGmailReadMessageDecodesTheBody(t *testing.T) {
	long := strings.Repeat("long line ", 4000)
	messages := map[string]string{
		"plain": `{"id":"plain","threadId":"t","labelIds":["INBOX"],"payload":{"mimeType":"multipart/alternative","headers":[{"name":"Subject","value":"Hi"}],"parts":[
			{"mimeType":"text/plain","body":{"size":5,"data":"` + b64("Hello\r\nworld") + `"}},
			{"mimeType":"text/html","body":{"data":"` + b64("<p>Hello <b>html</b></p>") + `"}},
			{"mimeType":"application/pdf","filename":"bill.pdf","body":{"size":900,"attachmentId":"a1"}}]}}`,
		"html":  `{"id":"html","threadId":"t","payload":{"mimeType":"text/html","body":{"data":"` + b64("<style>p{}</style><script>x()</script><div>Total:<br>12 &amp; 3</div><p>Thanks</p>") + `"}}}`,
		"latin": `{"id":"latin","threadId":"t","payload":{"mimeType":"text/plain","headers":[{"name":"Content-Type","value":"text/plain; charset=iso-8859-1"}],"body":{"data":"` + base64.RawURLEncoding.EncodeToString([]byte("caf\xe9")) + `"}}}`,
		"long":  `{"id":"long","threadId":"t","payload":{"mimeType":"text/plain","body":{"data":"` + b64(long) + `"}}}`,
	}
	m, _, _ := googleAccount(t, gmailID, googleScopes(gmailID, false), false, func(c googleCallLog) (int, string) {
		if body, ok := messages[strings.TrimPrefix(c.path, "/gmail/v1/users/me/messages/")]; ok && c.query.Get("format") == "full" {
			return 200, body
		}
		return 404, `{}`
	})
	read := func(id string) map[string]any {
		out, err := invokeGoogleTool(t, m, "read_message", `{"message_id":"`+id+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	plain := read("plain")
	if plain["body"] != "Hello\r\nworld" || plain["body_format"] != "text" || plain["subject"] != "Hi" {
		t.Fatalf("plain body: %v", plain)
	}
	attachments := plain["attachments"].([]any)
	if len(attachments) != 1 || attachments[0].(map[string]any)["filename"] != "bill.pdf" {
		t.Fatalf("attachments: %v", attachments)
	}
	html := read("html")
	if html["body"] != "Total:\n12 & 3\n\nThanks" || html["body_format"] != "html_as_text" {
		t.Fatalf("html body: %q", html["body"])
	}
	if read("latin")["body"] != "café" {
		t.Fatal("charset not converted")
	}
	cut := read("long")
	if cut["body_truncated"] != true || len(cut["body"].(string)) > gmailBodyLimit {
		t.Fatalf("long body not cut: %v %d", cut["body_truncated"], len(cut["body"].(string)))
	}
}

func decodeRaw(t *testing.T, raw any) string {
	t.Helper()
	out, err := base64.URLEncoding.DecodeString(raw.(string))
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestGmailDraftAndSendBuildSafeMail(t *testing.T) {
	m, _, log := googleAccount(t, gmailID, googleScopes(gmailID, true), true, func(c googleCallLog) (int, string) {
		switch {
		case c.method == "GET" && c.path == "/gmail/v1/users/me/messages/orig":
			return 200, `{"id":"orig","threadId":"thread-9","payload":{"headers":[{"name":"Message-ID","value":"<orig@example.test>"},{"name":"Subject","value":"Lunch?"},{"name":"References","value":"<first@example.test>"}]}}`
		case c.path == "/gmail/v1/users/me/drafts":
			return 200, `{"id":"d1","message":{"id":"dm1","threadId":"thread-9"}}`
		case c.path == "/gmail/v1/users/me/messages/send":
			return 200, `{"id":"sent1","threadId":"thread-9"}`
		case c.path == "/gmail/v1/users/me/drafts/send":
			return 200, `{"id":"sent2","threadId":"t"}`
		}
		return 404, `{}`
	})
	draft, err := invokeGoogleTool(t, m, "create_draft", `{"to":["Ann <ann@example.test>"],"cc":["bob@example.test"],"body":"Sounds good.\nSee you at noon — 好的","reply_to_message_id":"orig"}`)
	if err != nil {
		t.Fatal(err)
	}
	if draft["draft_id"] != "d1" || draft["sent"] != false {
		t.Fatalf("draft result: %v", draft)
	}
	var created googleCallLog
	for _, c := range *log {
		if c.path == "/gmail/v1/users/me/drafts" {
			created = c
		}
	}
	message := created.body["message"].(map[string]any)
	mail := decodeRaw(t, message["raw"])
	for _, want := range []string{"To: \"Ann\" <ann@example.test>", "Cc: <bob@example.test>", "Subject: Re: Lunch?", "In-Reply-To: <orig@example.test>", "References: <first@example.test> <orig@example.test>", "Content-Transfer-Encoding: quoted-printable"} {
		if !strings.Contains(mail, want) && !strings.Contains(strings.ReplaceAll(mail, "\"", ""), strings.ReplaceAll(want, "\"", "")) {
			t.Errorf("mail lacks %q:\n%s", want, mail)
		}
	}
	if message["threadId"] != "thread-9" {
		t.Fatalf("reply left its conversation: %v", message)
	}
	sent, err := invokeGoogleTool(t, m, "send_message", `{"to":["ann@example.test"],"subject":"Hello 世界","body":"Hi"}`)
	if err != nil || sent["sent"] != true || sent["message_id"] != "sent1" {
		t.Fatalf("send: %v %v", sent, err)
	}
	last := (*log)[len(*log)-1]
	if mail := decodeRaw(t, last.body["raw"]); !strings.Contains(mail, "Subject: =?utf-8?") {
		t.Fatalf("non-ASCII subject not encoded:\n%s", mail)
	}
	if out, err := invokeGoogleTool(t, m, "send_draft", `{"draft_id":"d1"}`); err != nil || out["message_id"] != "sent2" {
		t.Fatalf("send_draft: %v %v", out, err)
	}
	// Nothing a caller types can add a header or a recipient.
	before := len(*log)
	for _, args := range []string{
		`{"to":["a@example.test\r\nBcc: x@example.test"],"subject":"s","body":"b"}`,
		`{"to":["a@example.test"],"subject":"s\r\nBcc: x@example.test","body":"b"}`,
		`{"to":["not an address"],"subject":"s","body":"b"}`,
		`{"to":["a@example.test"],"cc":["b@example.test, c@example.test, d"],"subject":"s","body":"b"}`,
	} {
		if _, err := invokeGoogleTool(t, m, "send_message", args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	if len(*log) != before {
		t.Fatal("a rejected message reached Google")
	}
	if _, err := invokeGoogleTool(t, m, "create_draft", `{"to":["a@example.test"],"body":"no subject"}`); err == nil {
		t.Fatal("draft without a subject accepted")
	}
}

func TestGmailUpdateLabelsResolvesNames(t *testing.T) {
	m, _, log := googleAccount(t, gmailID, googleScopes(gmailID, true), true, func(c googleCallLog) (int, string) {
		if c.path == "/gmail/v1/users/me/labels" {
			return 200, `{"labels":[{"id":"INBOX","name":"INBOX","type":"system"},{"id":"UNREAD","name":"UNREAD","type":"system"},{"id":"Label_7","name":"Receipts","type":"user"}]}`
		}
		return 200, `{}`
	})
	if _, err := invokeGoogleTool(t, m, "update_labels", `{"message_id":"m1","mark_read":true,"archive":true,"add_labels":["receipts"]}`); err != nil {
		t.Fatal(err)
	}
	call := (*log)[len(*log)-1]
	if call.path != "/gmail/v1/users/me/messages/m1/modify" {
		t.Fatalf("path %s", call.path)
	}
	add, remove := call.body["addLabelIds"].([]any), call.body["removeLabelIds"].([]any)
	if len(add) != 1 || add[0] != "Label_7" || len(remove) != 2 || remove[0] != "UNREAD" || remove[1] != "INBOX" {
		t.Fatalf("labels: %v %v", add, remove)
	}
	if _, err := invokeGoogleTool(t, m, "update_labels", `{"thread_id":"t1","archive":true}`); err != nil || (*log)[len(*log)-1].path != "/gmail/v1/users/me/threads/t1/modify" {
		t.Fatalf("thread modify: %v", err)
	}
	for _, args := range []string{`{"message_id":"m1"}`, `{"message_id":"m1","thread_id":"t1","archive":true}`, `{"message_id":"m1","add_labels":["Nope"]}`} {
		if _, err := invokeGoogleTool(t, m, "update_labels", args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}

func TestCalendarListEventsExpandsRecurrenceInTheUsersZone(t *testing.T) {
	m, _, log := googleAccount(t, googleCalendarID, googleScopes(googleCalendarID, false), false, func(c googleCallLog) (int, string) {
		switch c.path {
		case "/calendar/v3/users/me/calendarList/primary":
			return 200, `{"id":"me@example.test","timeZone":"Asia/Shanghai"}`
		case "/calendar/v3/calendars/primary/events":
			return 200, `{"timeZone":"Asia/Shanghai","items":[
				{"id":"e1","status":"confirmed","summary":"Standup","start":{"dateTime":"2026-10-23T09:30:00+08:00"},"end":{"dateTime":"2026-10-23T09:45:00+08:00"},"recurringEventId":"series","organizer":{"email":"boss@example.test"},"attendees":[{"email":"me@example.test","self":true,"responseStatus":"accepted"}],"hangoutLink":"https://meet.example/abc"},
				{"id":"e2","status":"confirmed","summary":"Holiday","start":{"date":"2026-10-23"},"end":{"date":"2026-10-24"}},
				{"id":"e3","status":"cancelled","summary":"Gone","start":{"date":"2026-10-23"},"end":{"date":"2026-10-24"}}]}`
		}
		return 404, `{}`
	})
	out, err := invokeGoogleTool(t, m, "list_events", `{"time_min":"2026-10-23","time_max":"2026-10-24"}`)
	if err != nil {
		t.Fatal(err)
	}
	events := out["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("cancelled event listed: %v", events)
	}
	standup, holiday := events[0].(map[string]any), events[1].(map[string]any)
	if standup["start"] != "2026-10-23T09:30:00+08:00" || standup["meeting_link"] != "https://meet.example/abc" || standup["all_day"] != false || holiday["all_day"] != true || holiday["start"] != "2026-10-23" {
		t.Fatalf("events: %v %v", standup, holiday)
	}
	query := (*log)[len(*log)-1].query
	if query.Get("singleEvents") != "true" || query.Get("orderBy") != "startTime" || query.Get("timeMin") != "2026-10-23T00:00:00+08:00" || query.Get("timeMax") != "2026-10-24T00:00:00+08:00" {
		t.Fatalf("query: %v", query)
	}
	for _, args := range []string{`{"time_min":"tomorrow","time_max":"2026-10-24"}`, `{"time_min":"2026-10-23","time_max":"2026-10-24","time_zone":"Mars/Base"}`, `{"time_min":"2026-10-23"}`} {
		if _, err := invokeGoogleTool(t, m, "list_events", args); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
}

func TestCalendarFreeBusyAndCalendars(t *testing.T) {
	m, _, log := googleAccount(t, googleCalendarID, googleScopes(googleCalendarID, false), false, func(c googleCallLog) (int, string) {
		switch c.path {
		case "/calendar/v3/freeBusy":
			return 200, `{"calendars":{"primary":{"busy":[{"start":"2026-10-23T10:00:00Z","end":"2026-10-23T11:00:00Z"}]}}}`
		case "/calendar/v3/users/me/calendarList":
			return 200, `{"items":[{"id":"me@example.test","summary":"Me","primary":true,"timeZone":"Asia/Shanghai","accessRole":"owner"}]}`
		}
		return 404, `{}`
	})
	free, err := invokeGoogleTool(t, m, "check_availability", `{"time_min":"2026-10-23T00:00:00Z","time_max":"2026-10-24T00:00:00Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	busy := free["calendars"].(map[string]any)["primary"].(map[string]any)["busy"].([]any)
	if len(busy) != 1 || (*log)[0].body["items"].([]any)[0].(map[string]any)["id"] != "primary" {
		t.Fatalf("busy: %v items: %v", busy, (*log)[0].body)
	}
	calendars, err := invokeGoogleTool(t, m, "list_calendars", `{}`)
	if err != nil || calendars["calendars"].([]any)[0].(map[string]any)["name"] != "Me" {
		t.Fatalf("calendars: %v %v", calendars, err)
	}
}

func TestCalendarWritesDoNotEmailGuestsByDefault(t *testing.T) {
	event := `{"id":"ev1","status":"confirmed","summary":"Lunch","start":{"dateTime":"2026-10-24T12:00:00+08:00"},"end":{"dateTime":"2026-10-24T13:00:00+08:00"},"htmlLink":"https://calendar.example/ev1","attendees":[{"email":"me@example.test","self":true,"responseStatus":"needsAction","organizer":false},{"email":"ann@example.test","responseStatus":"accepted"}]}`
	m, _, log := googleAccount(t, googleCalendarID, googleScopes(googleCalendarID, true), true, func(c googleCallLog) (int, string) {
		if strings.HasPrefix(c.path, "/calendar/v3/calendars/primary/events") {
			return 200, event
		}
		return 404, `{}`
	})
	created, err := invokeGoogleTool(t, m, "create_event", `{"summary":"Lunch","start":"2026-10-24T12:00:00","end":"2026-10-24T13:00:00","time_zone":"Asia/Shanghai","attendees":["ann@example.test"],"location":"Cafe"}`)
	if err != nil || created["id"] != "ev1" {
		t.Fatalf("create: %v %v", created, err)
	}
	call := (*log)[len(*log)-1]
	start := call.body["start"].(map[string]any)
	if call.method != "POST" || call.query.Get("sendUpdates") != "none" || start["dateTime"] != "2026-10-24T12:00:00" || start["timeZone"] != "Asia/Shanghai" || call.body["attendees"].([]any)[0].(map[string]any)["email"] != "ann@example.test" {
		t.Fatalf("create call: %+v", call)
	}
	if _, err := invokeGoogleTool(t, m, "create_event", `{"summary":"All day","start":"2026-10-25","end":"2026-10-26"}`); err != nil {
		t.Fatal(err)
	}
	if day := (*log)[len(*log)-1].body["start"].(map[string]any); day["date"] != "2026-10-25" {
		t.Fatalf("all-day start: %v", day)
	}
	// Guests change by name, and everyone else on the list stays.
	if _, err := invokeGoogleTool(t, m, "update_event", `{"event_id":"ev1","add_attendees":["cy@example.test"],"remove_attendees":["ann@example.test"],"summary":"Lunch!"}`); err != nil {
		t.Fatal(err)
	}
	patch := (*log)[len(*log)-1]
	guests := patch.body["attendees"].([]any)
	if patch.method != "PATCH" || len(guests) != 2 || guests[0].(map[string]any)["email"] != "me@example.test" || guests[1].(map[string]any)["email"] != "cy@example.test" || patch.body["summary"] != "Lunch!" {
		t.Fatalf("patch: %+v", patch)
	}
	if _, err := invokeGoogleTool(t, m, "update_event", `{"event_id":"ev1"}`); err == nil {
		t.Fatal("empty update accepted")
	}
	if _, err := invokeGoogleTool(t, m, "respond_to_event", `{"event_id":"ev1","response":"declined"}`); err != nil {
		t.Fatal(err)
	}
	reply := (*log)[len(*log)-1]
	mine := reply.body["attendees"].([]any)[0].(map[string]any)
	if mine["email"] != "me@example.test" || mine["responseStatus"] != "declined" || reply.query.Get("sendUpdates") != "none" || reply.body["attendees"].([]any)[1].(map[string]any)["responseStatus"] != "accepted" {
		t.Fatalf("reply: %+v", reply)
	}
	if out, err := invokeGoogleTool(t, m, "delete_event", `{"event_id":"ev1","send_updates":"all"}`); err != nil || out["deleted"] != true {
		t.Fatalf("delete: %v %v", out, err)
	} else if last := (*log)[len(*log)-1]; last.method != "DELETE" || last.query.Get("sendUpdates") != "all" {
		t.Fatalf("delete call: %+v", last)
	}
}

func TestGoogleRefusalsAreExplained(t *testing.T) {
	m, _, _ := googleAccount(t, gmailID, googleScopes(gmailID, false), false, func(googleCallLog) (int, string) { return 403, `{"error":{"message":"secret detail"}}` })
	_, err := invokeGoogleTool(t, m, "read_message", `{"message_id":"m"}`)
	if err == nil || !strings.Contains(err.Error(), "reconnect") || strings.Contains(err.Error(), "secret detail") {
		t.Fatalf("refusal: %v", err)
	}
}
