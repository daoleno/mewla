package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const slackTestToken = "xoxp-test-secret-token"

// slackFixture answers Slack's Web API and files.slack.com like Slack does
// for a user token with the given scopes.
type slackFixture struct {
	t        *testing.T
	scopes   string
	file     string // files.info's file object
	body     []byte // what files.slack.com serves
	bodyType string
	status   int
	// seen are every request's URL, to prove the token never travels in one.
	seen []string
}

func (f *slackFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	f.seen = append(f.seen, r.URL.String())
	if r.Header.Get("Authorization") != "Bearer "+slackTestToken {
		return reply(401, `{}`), nil
	}
	switch r.URL.Host + r.URL.Path {
	case "slack.com/api/auth.test":
		res := reply(200, `{"ok":true,"team":"Team","user":"paul","user_id":"U1"}`)
		res.Header.Set("X-OAuth-Scopes", f.scopes)
		return res, nil
	case "slack.com/api/files.info":
		if r.URL.Query().Get("file") != "F0ABCDEF12" {
			return reply(200, `{"ok":false,"error":"file_not_found"}`), nil
		}
		return reply(200, `{"ok":true,"file":`+f.file+`}`), nil
	case "slack.com/api/conversations.replies":
		return reply(200, `{"ok":true,"messages":[{"ts":"1.0","text":"broken","files":[{"id":"F0ABCDEF12","name":"screen.png","mimetype":"image/png","size":4}]}]}`), nil
	case "files.slack.com/files-pri/T1-F0ABCDEF12/download/screen.png":
		status := f.status
		if status == 0 {
			status = 200
		}
		res := &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {f.bodyType}}, Body: io.NopCloser(bytes.NewReader(f.body))}
		return res, nil
	}
	return reply(404, `{}`), nil
}

func slackFileJSON(size int) string {
	return `{"id":"F0ABCDEF12","name":"../../screen shot.png","title":"Screen shot","mimetype":"image/png","filetype":"png","size":` + itoa(size) + `,"mode":"hosted","url_private":"https://files.slack.com/files-pri/T1-F0ABCDEF12/screen.png","url_private_download":"https://files.slack.com/files-pri/T1-F0ABCDEF12/download/screen.png","permalink":"https://team.slack.com/files/U1/F0ABCDEF12/screen.png"}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func addSlack(t *testing.T, f *slackFixture) (*Manager, *Account) {
	t.Helper()
	m := testManager(t, f.RoundTrip)
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "slack", Name: "S", Credential: slackTestToken}}).Account
	return m, a
}

func toolNames(a *Account) map[string]bool {
	out := map[string]bool{}
	for _, tool := range a.Tools {
		out[tool.Name] = tool.Allowed
	}
	return out
}

func TestSlackToolsFollowGrantedScopes(t *testing.T) {
	_, all := addSlack(t, &slackFixture{t: t, scopes: strings.Join(slackReadScopes, ",")})
	names := toolNames(all)
	for _, want := range []string{"get_me", "list_channels", "channel_history", "thread_replies", "search_messages", "get_file"} {
		if allowed, ok := names[want]; !ok || !allowed {
			t.Fatalf("%s missing or not allowed for a full read grant: %v", want, names)
		}
	}
	_, old := addSlack(t, &slackFixture{t: t, scopes: "channels:read,channels:history,search:read"})
	names = toolNames(old)
	if _, ok := names["get_file"]; ok {
		t.Fatalf("get_file offered without files:read: %v", names)
	}
	if _, ok := names["thread_replies"]; !ok {
		t.Fatalf("thread_replies needs only a history scope: %v", names)
	}
	_, private := addSlack(t, &slackFixture{t: t, scopes: "im:history"})
	if _, ok := toolNames(private)["thread_replies"]; !ok {
		t.Fatal("a DM history grant reads threads")
	}
}

func TestSlackSignInAsksForFilesAndPrivateConversations(t *testing.T) {
	scopes := lookup("slack").OAuth.Scopes(false)
	for _, want := range []string{"files:read", "groups:history", "im:history", "mpim:history", "channels:history", "search:read"} {
		if !contains(scopes, want) {
			t.Fatalf("read sign-in lacks %s: %v", want, scopes)
		}
	}
	if contains(scopes, "chat:write") || !contains(lookup("slack").OAuth.Scopes(true), "chat:write") {
		t.Fatal("chat:write is asked for only with changes")
	}
}

func getFile(t *testing.T, m *Manager, id, file string) (Response, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"query": map[string]any{"file": file}})
	return m.Handle(context.Background(), Request{Action: "invoke", ID: id, Tool: "get_file", Arguments: args})
}

func TestSlackGetFileDownloadsIntoTheAccountFolder(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage-bytes")
	f := &slackFixture{t: t, scopes: strings.Join(slackReadScopes, ","), file: slackFileJSON(len(png)), body: png, bodyType: "image/png"}
	m, a := addSlack(t, f)
	for _, ref := range []string{"F0ABCDEF12", "https://files.slack.com/files-pri/T1-F0ABCDEF12/screen.png", "https://team.slack.com/files/U1/F0ABCDEF12/screen.png"} {
		response, err := getFile(t, m, a.ID, ref)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		var out struct {
			Path, ID, Name, Mimetype string
			Size                     int
		}
		if err := json.Unmarshal(response.Result, &out); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(filepath.Dir(m.path), "slack-files", a.ID)
		if filepath.Dir(out.Path) != dir || filepath.Base(out.Path) != "F0ABCDEF12-screen_shot.png" {
			t.Fatalf("file saved at %s, want under %s", out.Path, dir)
		}
		got, err := os.ReadFile(out.Path)
		if err != nil || !bytes.Equal(got, png) || out.Size != len(png) || out.Mimetype != "image/png" {
			t.Fatalf("downloaded %q (%v), result %+v", got, err, out)
		}
		info, _ := os.Stat(out.Path)
		folder, _ := os.Stat(dir)
		if info.Mode().Perm() != 0o600 || folder.Mode().Perm() != 0o700 {
			t.Fatalf("file %v, folder %v: want private", info.Mode(), folder.Mode())
		}
		if bytes.Contains(response.Result, []byte(slackTestToken)) {
			t.Fatal("token in tool output")
		}
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(m.path), "slack-files", a.ID))
	if len(entries) != 1 {
		t.Fatalf("a download leaves exactly its file, got %d entries", len(entries))
	}
	for _, seen := range f.seen {
		if strings.Contains(seen, slackTestToken) {
			t.Fatalf("token in a request URL: %s", seen)
		}
	}
	history, _ := json.Marshal(m.records[a.ID].Account.History)
	if bytes.Contains(history, []byte(slackTestToken)) {
		t.Fatal("token in account history")
	}
}

func TestSlackGetFileRefusals(t *testing.T) {
	old := slackFileLimit
	defer func() { slackFileLimit = old }()
	slackFileLimit = 8
	cases := map[string]struct {
		fixture slackFixture
		file    string
		want    string
	}{
		"declared size over the cap":  {slackFixture{file: slackFileJSON(9), body: []byte("123456789"), bodyType: "image/png"}, "F0ABCDEF12", "up to"},
		"body over the cap":           {slackFixture{file: slackFileJSON(4), body: []byte("123456789"), bodyType: "image/png"}, "F0ABCDEF12", "larger than"},
		"sign-in page, not the file":  {slackFixture{file: slackFileJSON(4), body: []byte("<html>sign in</html>"), bodyType: "text/html; charset=utf-8"}, "F0ABCDEF12", "Reconnect Slack in Extensions"},
		"forbidden download":          {slackFixture{file: slackFileJSON(4), status: 403, bodyType: "text/plain"}, "F0ABCDEF12", "Reconnect Slack in Extensions"},
		"unknown file":                {slackFixture{file: slackFileJSON(4)}, "F0ZZZZZZZZ", "file_not_found"},
		"not a file reference":        {slackFixture{file: slackFileJSON(4)}, "https://evil.example/F0ABCDEF12", "Pass a Slack file id"},
		"external file":               {slackFixture{file: `{"id":"F0ABCDEF12","name":"doc","mode":"external","is_external":true,"filetype":"gdoc","url_private":"https://docs.google.com/x"}`}, "F0ABCDEF12", "outside Slack"},
		"address off files.slack.com": {slackFixture{file: `{"id":"F0ABCDEF12","name":"x.png","size":4,"mode":"hosted","url_private_download":"https://evil.example/x.png"}`}, "F0ABCDEF12", "no downloadable address"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := c.fixture
			f.t, f.scopes = t, strings.Join(slackReadScopes, ",")
			m, a := addSlack(t, &f)
			_, err := getFile(t, m, a.ID, c.file)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if strings.Contains(err.Error(), slackTestToken) {
				t.Fatal("token in error")
			}
			entries, _ := os.ReadDir(filepath.Join(filepath.Dir(m.path), "slack-files", a.ID))
			if len(entries) != 0 {
				t.Fatalf("a refused download left %d files", len(entries))
			}
		})
	}
}

// An account signed in before Mewla read files says so plainly, both to the
// app (read_consent → Reconnect) and to an agent calling the newer tools.
func TestOldSlackSignInAsksForReconnect(t *testing.T) {
	f := &slackFixture{t: t, scopes: "channels:read,channels:history,search:read,chat:write"}
	m, a := addSlack(t, f)
	r := m.records[a.ID]
	r.OAuth = &oauthAccount{ClientID: "c"}
	r.Account.Scopes = []string{"channels:read", "channels:history", "search:read", "chat:write"}
	// The tool list such an account was saved with, before thread_replies.
	var kept []Tool
	for _, tool := range r.Account.Tools {
		if tool.Name != "thread_replies" {
			kept = append(kept, tool)
		}
	}
	r.Account.Tools = kept
	if p := m.projection(r); !p.Access.ReadConsent {
		t.Fatal("old sign-in does not ask for Reconnect")
	}
	for _, tool := range []string{"thread_replies", "get_file"} {
		_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: a.ID, Tool: tool, Arguments: json.RawMessage(`{"query":{"channel":"C1","ts":"1.0","file":"F0ABCDEF12"}}`)})
		if err == nil || !strings.Contains(err.Error(), "Reconnect Slack in Extensions") {
			t.Fatalf("%s on an old sign-in: %v", tool, err)
		}
	}
	r.Account.Scopes = append(append([]string(nil), slackReadScopes...), "chat:write")
	if p := m.projection(r); p.Access.ReadConsent {
		t.Fatal("a full grant still asks for Reconnect")
	}
}

func TestSlackThreadReplies(t *testing.T) {
	m, a := addSlack(t, &slackFixture{t: t, scopes: strings.Join(slackReadScopes, ",")})
	response := mustHandle(t, m, Request{Action: "invoke", ID: a.ID, Tool: "thread_replies", Arguments: json.RawMessage(`{"query":{"channel":"C1","ts":"1.0"}}`)})
	if !bytes.Contains(response.Result, []byte(`"F0ABCDEF12"`)) {
		t.Fatalf("thread files dropped: %s", response.Result)
	}
}

func TestSlackFileReferenceAndName(t *testing.T) {
	for in, want := range map[string]string{
		"F0ABCDEF12": "F0ABCDEF12",
		"https://files.slack.com/files-pri/T0123-F0ABCDEF12/download/a.png": "F0ABCDEF12",
		"https://acme.slack.com/files/U1/F0ABCDEF12/a.png":                  "F0ABCDEF12",
	} {
		if got, err := slackFileID(in); err != nil || got != want {
			t.Fatalf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "x", "http://files.slack.com/files-pri/T1-F0ABCDEF12/a", "https://slack.com.evil.example/F0ABCDEF12"} {
		if _, err := slackFileID(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	for in, want := range map[string]string{"../../etc/passwd": "passwd", "截图 1.png": "截图_1.png", "..": "file", `a\b.png`: "b.png"} {
		if got := slackFileName(in); got != want {
			t.Fatalf("%q → %q, want %q", in, got, want)
		}
	}
}
