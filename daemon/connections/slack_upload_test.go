package connections

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// slackUploadWire answers Slack's three upload steps like Slack does, and
// keeps what each saw.
type slackUploadWire struct {
	t       *testing.T
	scopes  string
	slots   []slotInfo
	bytes   map[string][]byte // upload path → body
	types   map[string]string // upload path → Content-Type
	lengths map[string]int64
	done    map[string]any // files.completeUploadExternal's JSON
	// uploadHost overrides the host Slack's upload step names.
	uploadHost string
	// seen are every request's URL, to prove the token never travels in one.
	seen   []string
	tokens map[string]bool // hosts the token reached
}

type slotInfo struct{ id, name, length string }

func (w *slackUploadWire) RoundTrip(r *http.Request) (*http.Response, error) {
	w.seen = append(w.seen, r.URL.String())
	if r.Header.Get("Authorization") == "Bearer "+slackTestToken {
		if w.tokens == nil {
			w.tokens = map[string]bool{}
		}
		w.tokens[r.URL.Host] = true
	}
	host := w.uploadHost
	if host == "" {
		host = "files.slack.com"
	}
	switch r.URL.Host + r.URL.Path {
	case "slack.com/api/auth.test":
		res := reply(200, `{"ok":true,"team":"Team","user":"paul","user_id":"U1"}`)
		res.Header.Set("X-OAuth-Scopes", w.scopes)
		return res, nil
	case "slack.com/api/files.getUploadURLExternal":
		if r.Header.Get("Authorization") != "Bearer "+slackTestToken {
			return reply(401, `{}`), nil
		}
		q := r.URL.Query()
		id := "F0UPLOAD" + itoa(len(w.slots)+1)
		w.slots = append(w.slots, slotInfo{id: id, name: q.Get("filename"), length: q.Get("length")})
		return reply(200, `{"ok":true,"upload_url":"https://`+host+`/upload/v1/`+id+`","file_id":"`+id+`"}`), nil
	case "slack.com/api/files.completeUploadExternal":
		if r.Header.Get("Authorization") != "Bearer "+slackTestToken || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			return reply(401, `{}`), nil
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &w.done)
		return reply(200, `{"ok":true,"files":[{"id":"F0UPLOAD1","title":"x","permalink":"https://team.slack.com/files/U1/F0UPLOAD1/shot.png"}]}`), nil
	}
	if strings.HasPrefix(r.URL.Path, "/upload/v1/") {
		raw, _ := io.ReadAll(r.Body)
		if w.bytes == nil {
			w.bytes, w.types, w.lengths = map[string][]byte{}, map[string]string{}, map[string]int64{}
		}
		w.bytes[r.URL.Path], w.types[r.URL.Path], w.lengths[r.URL.Path] = raw, r.Header.Get("Content-Type"), r.ContentLength
		return reply(200, "OK - "+itoa(len(raw))), nil
	}
	return reply(404, `{}`), nil
}

func sendFiles(t *testing.T, m *Manager, id string, body map[string]any) (Response, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"body": body})
	return m.Handle(t.Context(), Request{Action: "invoke", ID: id, Tool: "send_files", Arguments: args})
}

func uploadFixture(t *testing.T, w *slackUploadWire) (*Manager, *Account) {
	t.Helper()
	m := testManager(t, w.RoundTrip)
	a := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "slack", Name: "S", Credential: slackTestToken}}).Account
	mustHandle(t, m, Request{Action: "permissions", ID: a.ID, Group: "write", Allowed: true})
	return m, a
}

func TestSlackSendFilesUploadsEveryTypeWithItsNameAndMime(t *testing.T) {
	dir := t.TempDir()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 3000)...)
	pdf := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n")
	zip := []byte("PK\x03\x04zip-bytes")
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	w := &slackUploadWire{t: t, scopes: "channels:read,chat:write,files:write"}
	m, a := uploadFixture(t, w)
	response, err := sendFiles(t, m, a.ID, map[string]any{
		"channel": "D0123", "thread_ts": "1700000000.000100", "text": "three files",
		"files": []any{
			map[string]any{"path": write("shot.png", png), "title": "The shot", "alt_text": "a screenshot"},
			map[string]any{"path": write("report.pdf", pdf)},
			map[string]any{"path": write("data.bin", zip), "filename": "bundle.zip", "mimetype": "application/zip"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.slots) != 3 || w.slots[0].name != "shot.png" || w.slots[1].name != "report.pdf" || w.slots[2].name != "bundle.zip" || w.slots[0].length != itoa(len(png)) {
		t.Fatalf("upload slots %+v", w.slots)
	}
	for i, want := range []struct {
		body []byte
		mime string
	}{{png, "image/png"}, {pdf, "application/pdf"}, {zip, "application/zip"}} {
		path := "/upload/v1/" + w.slots[i].id
		if string(w.bytes[path]) != string(want.body) || w.types[path] != want.mime || w.lengths[path] != int64(len(want.body)) {
			t.Fatalf("file %d: %d bytes as %q (length %d), want %d as %q", i, len(w.bytes[path]), w.types[path], w.lengths[path], len(want.body), want.mime)
		}
	}
	files, _ := w.done["files"].([]any)
	first, _ := files[0].(map[string]any)
	second, _ := files[1].(map[string]any)
	if w.done["channel_id"] != "D0123" || w.done["thread_ts"] != "1700000000.000100" || w.done["initial_comment"] != "three files" || len(files) != 3 || first["id"] != "F0UPLOAD1" || first["title"] != "The shot" || second["title"] != "report.pdf" {
		t.Fatalf("completeUploadExternal got %v", w.done)
	}
	if !strings.Contains(string(response.Result), `"permalink":"https://team.slack.com/files/U1/F0UPLOAD1/shot.png"`) || !strings.Contains(string(response.Result), `"mimetype":"application/zip"`) {
		t.Fatalf("result %s", response.Result)
	}
	// The token goes to slack.com's API only: never to the upload address, never in an address.
	if w.tokens["files.slack.com"] || !w.tokens["slack.com"] {
		t.Fatalf("token reached %v", w.tokens)
	}
	for _, u := range w.seen {
		if strings.Contains(u, slackTestToken) {
			t.Fatalf("token in %s", u)
		}
	}
}

func TestSlackSendFilesGuessesTypesFromTheBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-extension")
	if err := os.WriteFile(path, []byte("\xff\xd8\xff\xe0jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &slackUploadWire{t: t, scopes: "chat:write,files:write"}
	m, a := uploadFixture(t, w)
	if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": path}}}); err != nil {
		t.Fatal(err)
	}
	if got := w.types["/upload/v1/"+w.slots[0].id]; got != "image/jpeg" {
		t.Fatalf("type %q", got)
	}
	if _, set := w.done["initial_comment"]; set {
		t.Fatalf("an empty comment was sent: %v", w.done)
	}
}

func TestSlackSendFilesRefusals(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(good, []byte("hi"), 0o600)
	empty := filepath.Join(dir, "empty.txt")
	_ = os.WriteFile(empty, nil, 0o600)
	link := filepath.Join(dir, "link.txt")
	_ = os.Symlink(good, link)
	w := &slackUploadWire{t: t, scopes: "chat:write,files:write"}
	m, a := uploadFixture(t, w)
	state := filepath.Dir(m.path)
	secret := filepath.Join(state, "integration-credentials.json")
	_ = os.WriteFile(secret, []byte(`{"integration:x":"secret"}`), 0o600)
	viaLink := filepath.Join(dir, "to-secret")
	_ = os.Symlink(secret, viaLink)

	for name, c := range map[string]struct {
		path string
		want string
	}{
		"relative":     {"a.txt", "absolute path"},
		"missing":      {filepath.Join(dir, "none.txt"), "No file at"},
		"folder":       {dir, "not a regular file"},
		"empty":        {empty, "empty"},
		"state folder": {secret, "state folder"},
		"link into it": {viaLink, "state folder"},
		"its logs":     {stateFile(t, state, "logs/daemon.log"), "state folder"},
		"its secrets":  {stateFile(t, state, "secrets/key"), "state folder"},
		"Brain's own":  {stateFile(t, state, "brain/secrets/token"), "state folder"},
		"worklog link": {linkIn(t, state, "brain/workspace/worklog/w/leak.png", secret), "state folder"},
	} {
		_, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": c.path}}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(err.Error(), "secret\"") {
			t.Fatalf("%s: leaked content: %v", name, err)
		}
	}
	// A bad type is refused before anything is opened; a link to an ordinary file is fine.
	if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": good, "mimetype": "not a type"}}}); err == nil || !strings.Contains(err.Error(), "mimetype") {
		t.Fatalf("bad mimetype: %v", err)
	}
	if len(w.slots) != 0 {
		t.Fatalf("a refused call opened %d uploads", len(w.slots))
	}
	if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": link}}}); err != nil {
		t.Fatalf("symlink to a normal file: %v", err)
	}
	// 11-27: Brain's worklog (where reports and proof images live) and a
	// session's scratch are content, not secrets.
	for _, rel := range []string{"brain/workspace/worklog/mewla-proof/shot.png", "t/abc123/out.pdf"} {
		if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": stateFile(t, state, rel)}}}); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	// Mewla's own downloads may be sent on.
	downloads := filepath.Join(state, "slack-files", a.ID)
	_ = os.MkdirAll(downloads, 0o700)
	kept := filepath.Join(downloads, "F0ABCDEF12-a.png")
	_ = os.WriteFile(kept, []byte("png"), 0o600)
	if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": kept}}}); err != nil {
		t.Fatalf("a downloaded file: %v", err)
	}
	// Over the limit.
	previous := slackUploadLimit
	slackUploadLimit = 1
	defer func() { slackUploadLimit = previous }()
	if _, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": good}}}); err == nil || !strings.Contains(err.Error(), "Slack takes files up to") {
		t.Fatalf("over the limit: %v", err)
	}
}

func TestSlackSendFilesNeverSendsTheTokenToAnotherHost(t *testing.T) {
	good := filepath.Join(t.TempDir(), "a.txt")
	_ = os.WriteFile(good, []byte("hi"), 0o600)
	for _, host := range []string{"evil.example", "files.slack.com:8443", "user@files.slack.com"} {
		w := &slackUploadWire{t: t, scopes: "chat:write,files:write", uploadHost: host}
		m, a := uploadFixture(t, w)
		_, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": good}}})
		if err == nil || !strings.Contains(err.Error(), "no valid upload address") || len(w.bytes) != 0 {
			t.Fatalf("%s: %v", host, err)
		}
	}
}

// An account signed in before Mewla could send files keeps every tool it has;
// only send_files asks for Reconnect, and the app learns of it.
func TestSlackWithoutFilesWriteKeepsOtherToolsAndAsksToReconnect(t *testing.T) {
	w := &slackUploadWire{t: t, scopes: "channels:read,channels:history,chat:write"}
	m, a := uploadFixture(t, w)
	if _, ok := toolNames(a)["send_files"]; ok {
		t.Fatal("send_files offered without files:write")
	}
	view := m.projection(m.records[a.ID])
	if !toolNames(&view)["send_message"] {
		t.Fatal("send_message lost")
	}
	r := m.records[a.ID]
	r.OAuth = &oauthAccount{ClientID: "c"}
	r.Account.Scopes = append(append([]string(nil), slackReadScopes...), "chat:write")
	if p := m.projection(r).Access; !p.WriteUpgrade || p.WriteConsent || p.ReadConsent {
		t.Fatalf("access %+v", p)
	}
	_, err := sendFiles(t, m, a.ID, map[string]any{"channel": "C1", "files": []any{map[string]any{"path": "/tmp/x"}}})
	if err == nil || !strings.Contains(err.Error(), "Reconnect Slack in Extensions") {
		t.Fatalf("send_files on an old sign-in: %v", err)
	}
	r.Account.Scopes = append(r.Account.Scopes, "files:write")
	if p := m.projection(r).Access; p.WriteUpgrade {
		t.Fatalf("a full grant still asks: %+v", p)
	}
	// A read-only sign-in is asked for changes the usual way, not as an upgrade.
	r.Account.Scopes = []string{"channels:read"}
	if p := m.projection(r).Access; p.WriteUpgrade || !p.WriteConsent {
		t.Fatalf("read-only access %+v", p)
	}
}

func TestSlackSignInAsksForFilesWriteOnlyWithChanges(t *testing.T) {
	spec := lookup("slack").OAuth
	if contains(spec.Scopes(false), "files:write") || !contains(spec.Scopes(true), "files:write") || !contains(spec.Scopes(true), "chat:write") {
		t.Fatalf("scopes %v / %v", spec.Scopes(false), spec.Scopes(true))
	}
}

// stateFile writes a small file at rel inside the state folder.
func stateFile(t *testing.T, state, rel string) string {
	t.Helper()
	path := filepath.Join(state, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// linkIn makes a link at rel inside the state folder that leads to target.
func linkIn(t *testing.T, state, rel, target string) string {
	t.Helper()
	path := filepath.Join(state, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	return path
}
