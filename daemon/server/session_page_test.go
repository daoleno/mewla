package server

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/classifier"
)

type sessionPageFixture struct {
	server     *Server
	manager    *auth.Manager
	privateKey ed25519.PrivateKey
	deviceID   string
	worker     *classifier.Worker
	live       bool
}

func newSessionPageFixture(t *testing.T, cwd string) *sessionPageFixture {
	t.Helper()
	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	f := &sessionPageFixture{
		manager:    manager,
		privateKey: privateKey,
		deviceID:   deviceID,
		worker:     &classifier.Worker{ID: "main:@page", Cwd: cwd, ProcessID: 77, StartedAt: time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)},
		live:       true,
	}
	f.server = New(manager, nil, nil, nil, nil, nil, nil)
	f.server.sessionFileWorkerLoader = func(id string) *classifier.Worker {
		if !f.live || id != f.worker.ID {
			return nil
		}
		copy := *f.worker
		return &copy
	}
	return f
}

// grant asks for a page grant for reference and returns the response.
func (f *sessionPageFixture) grant(t *testing.T, reference string) *httptest.ResponseRecorder {
	t.Helper()
	resolved, err := openSessionFile(f.worker.Cwd, reference)
	generation := ""
	if err == nil {
		generation = resolved.generation
		_ = resolved.file.Close()
	}
	body, _ := json.Marshal(map[string]any{
		"worker_id":  f.worker.ID,
		"process_id": f.worker.ProcessID,
		"started_at": f.worker.StartedAt.UnixMilli(),
		"path":       reference,
		"generation": generation,
	})
	request := httptest.NewRequest(http.MethodPost, "/session-page-grant", bytes.NewReader(body))
	request.Header.Set("Authorization", sessionFileAuthorizationHeader(t, f.privateKey, f.manager.DaemonID(), f.deviceID))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, request)
	return response
}

// pagePath grants reference and returns the page's URL path.
func (f *sessionPageFixture) pagePath(t *testing.T, reference string) string {
	t.Helper()
	response := f.grant(t, reference)
	if response.Code != http.StatusOK {
		t.Fatalf("grant %s: status=%d body=%q", reference, response.Code, response.Body.String())
	}
	var answer struct {
		Version     int    `json:"version"`
		Path        string `json:"path"`
		ExpiresAtMS int64  `json:"expires_at_ms"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Version != 1 || !strings.HasPrefix(answer.Path, sessionPagePrefix) || answer.ExpiresAtMS <= time.Now().UnixMilli() {
		t.Fatalf("grant answer = %#v", answer)
	}
	return answer.Path
}

func (f *sessionPageFixture) get(path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4000"+path, nil)
	request.Host = "127.0.0.1:4000"
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, request)
	return response
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// folderOf: the page path's grant prefix, to ask for other files under the same grant.
func folderOf(pagePath string) string {
	return pagePath[:strings.LastIndex(pagePath, "/")+1]
}

func assertSandboxed(t *testing.T, label string, response *httptest.ResponseRecorder) {
	t.Helper()
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.HasPrefix(policy, "sandbox allow-scripts ") || strings.Contains(policy, "allow-same-origin") || strings.Contains(policy, "allow-top-navigation") {
		t.Fatalf("%s: status %d, policy %q is not an opaque sandbox", label, response.Code, policy)
	}
	if response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("%s: headers = %v", label, response.Header())
	}
}

func TestSessionPageServesThePageAndItsFolderSandboxed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	writeFiles(t, cwd, map[string]string{
		"out/report.html":    `<!doctype html><script src="./chart.js"></script>`,
		"out/chart.js":       "console.log(1)",
		"out/data.json":      `{"ok":true}`,
		"out/style.css":      "body{}",
		"out/img/x.png":      "\x89PNG\r\n\x1a\n",
		"out/page two.html":  `<meta charset="gbk">`,
		"out/.env":           "SECRET=1",
		"out/.git/config":    "[core]",
		"outside.html":       "<p>outside</p>",
		"other/secrets.html": "<p>other</p>",
	})
	f := newSessionPageFixture(t, cwd)
	page := f.pagePath(t, "out/report.html")
	folder := folderOf(page)

	entry := f.get(page)
	if entry.Code != http.StatusOK || entry.Body.String() != `<!doctype html><script src="./chart.js"></script>` {
		t.Fatalf("entry: status=%d body=%q", entry.Code, entry.Body.String())
	}
	if got := entry.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("entry content type = %q", got)
	}
	if entry.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("the page's opaque origin cannot read its own files")
	}
	policy := entry.Header().Get("Content-Security-Policy")
	scope := "http://127.0.0.1:4000" + folder + " https://127.0.0.1:4000" + folder
	for _, want := range []string{"default-src " + scope + " https: ", "connect-src " + scope + " https: wss:", "frame-ancestors http://127.0.0.1:4000 https://127.0.0.1:4000", "form-action 'none'"} {
		if !strings.Contains(policy, want) {
			t.Fatalf("policy %q lacks %q", policy, want)
		}
	}
	assertSandboxed(t, "entry", entry)

	for name, contentType := range map[string]string{"chart.js": "text/javascript; charset=utf-8", "data.json": "application/json", "style.css": "text/css; charset=utf-8", "img/x.png": "image/png", "page%20two.html": "text/html"} {
		response := f.get(folder + name)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != contentType {
			t.Fatalf("%s: status=%d type=%q", name, response.Code, response.Header().Get("Content-Type"))
		}
		assertSandboxed(t, name, response)
	}

	// Nothing above the folder, no hidden file, no other system's separator, no empty part.
	for _, name := range []string{"../outside.html", "%2e%2e/outside.html", "img/..%2f..%2foutside.html", "..%5coutside.html", ".env", ".git/config", "", "img/", "%2Fetc%2Fpasswd"} {
		response := f.get(folder + name)
		// The mux sends a path with ".." to its clean form, which no longer carries the grant.
		if response.Code >= 300 && response.Code < 400 {
			response = f.get(response.Header().Get("Location"))
		}
		if response.Code == http.StatusOK {
			t.Fatalf("%q was served: %q", name, response.Body.String())
		}
		assertSandboxed(t, "refusal "+name, response)
	}
	// A file of another folder is reachable only through its own grant.
	other := f.pagePath(t, "other/secrets.html")
	if f.get(folder+"secrets.html").Code == http.StatusOK || f.get(folderOf(other)+"report.html").Code == http.StatusOK {
		t.Fatal("a grant reached another folder")
	}
}

func TestSessionPageRefusesASymlinkOutOfItsFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	writeFiles(t, cwd, map[string]string{"out/report.html": "<p>r</p>", "secret.txt": "secret", "out/inner/real.txt": "inner"})
	if err := os.Symlink(filepath.Join(cwd, "secret.txt"), filepath.Join(cwd, "out", "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cwd, filepath.Join(cwd, "out", "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(cwd, "out", "inner", "real.txt"), filepath.Join(cwd, "out", "alias.txt")); err != nil {
		t.Fatal(err)
	}
	f := newSessionPageFixture(t, cwd)
	folder := folderOf(f.pagePath(t, "out/report.html"))
	for _, name := range []string{"escape.txt", "up/secret.txt"} {
		if response := f.get(folder + name); response.Code == http.StatusOK {
			t.Fatalf("%s escaped the folder: %q", name, response.Body.String())
		}
	}
	if response := f.get(folder + "alias.txt"); response.Code != http.StatusOK || response.Body.String() != "inner" {
		t.Fatalf("a link inside the folder: status=%d", response.Code)
	}
}

func TestSessionPageGrantLapsesAndOutlivesTheSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	writeFiles(t, cwd, map[string]string{"report.html": "<p>r</p>", "notes.txt": "n"})
	f := newSessionPageFixture(t, cwd)
	page := f.pagePath(t, "report.html")

	// The Session ended: the grant it gave still opens the page and its files.
	f.live = false
	if response := f.get(page); response.Code != http.StatusOK {
		t.Fatalf("after the Session: status=%d", response.Code)
	}
	// The ended run still grants its own files (session_runs.go); a run never seen live does not.
	if response := f.grant(t, "report.html"); response.Code != http.StatusOK {
		t.Fatalf("a grant for the ended run: status=%d", response.Code)
	}
	f.worker.ProcessID += 9
	if response := f.grant(t, "report.html"); response.Code != http.StatusConflict {
		t.Fatalf("a grant for a run never seen live: status=%d", response.Code)
	}
	f.worker.ProcessID -= 9
	f.live = true

	// A changed signature, or a grant moved to another folder, is refused.
	token := strings.TrimPrefix(folderOf(page), sessionPagePrefix)
	token = strings.TrimSuffix(token, "/")
	encoded, signature, _ := strings.Cut(token, ".")
	tampered := sessionPagePrefix + encoded + "." + strings.Repeat("0", len(signature)) + "/report.html"
	if response := f.get(tampered); response.Code != http.StatusUnauthorized {
		t.Fatalf("tampered: status=%d", response.Code)
	}
	forged, _ := f.server.verifySessionPageGrant(token)
	forged.Root = "/"
	payload, _ := json.Marshal(forged)
	if response := f.get(sessionPagePrefix + base64.RawURLEncoding.EncodeToString(payload) + "." + signature + "/etc/passwd"); response.Code != http.StatusUnauthorized {
		t.Fatal("a forged root was served")
	}

	// Past its lifetime the link is refused.
	f.server.sessionFileCapabilityClock = func() time.Time { return time.Now().Add(sessionPageGrantTTL + time.Minute) }
	expired := f.get(page)
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired: status=%d", expired.Code)
	}
	assertSandboxed(t, "expired", expired)
	f.server.sessionFileCapabilityClock = nil

	// A device no longer paired loses its open pages.
	if _, err := f.manager.RevokeDevice(f.deviceID); err != nil {
		t.Fatal(err)
	}
	if response := f.get(page); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked device: status=%d", response.Code)
	}
}

func TestSessionPageGrantIsForHTMLOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cwd := t.TempDir()
	writeFiles(t, cwd, map[string]string{"notes.txt": "n", "readme.md": "# r", "fake.html": "\x00\x01binary"})
	f := newSessionPageFixture(t, cwd)
	for _, name := range []string{"notes.txt", "readme.md", "fake.html"} {
		if response := f.grant(t, name); response.Code == http.StatusOK {
			t.Fatalf("%s got a page grant", name)
		}
	}
	if response := f.get(sessionPagePrefix + "garbage/report.html"); response.Code != http.StatusUnauthorized {
		t.Fatalf("garbage grant: status=%d", response.Code)
	}
}

func TestSessionPageServesNoFolderOfTheHomeOrStateRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	home, _ = filepath.EvalSymlinks(home)
	state := filepath.Join(home, ".mewla")
	writeFiles(t, home, map[string]string{
		"report.html":                           "<p>home</p>",
		"notes.txt":                             "private",
		".mewla/report.html":                    "<p>state</p>",
		".mewla/devices.json":                   "{}",
		".mewla/brain/report.html":              "<p>brain</p>",
		".mewla/brain/secrets.txt":              "s",
		".mewla/brain/workspace/out/chart.html": "<p>chart</p>",
		".mewla/brain/workspace/out/data.json":  "{}",
		".mewla/worktrees/repo/r.html":          "<p>w</p>",
		".mewla/worktrees/repo/d.csv":           "a,b",
		"projects/q3/report.html":               "<p>q3</p>",
		"projects/q3/data.csv":                  "a,b",
	})
	f := newSessionPageFixture(t, home)
	cases := []struct {
		page, sibling string
		siblings      bool
	}{
		{"report.html", "notes.txt", false},
		{".mewla/report.html", "devices.json", false},
		{".mewla/brain/report.html", "secrets.txt", false},
		{".mewla/brain/workspace/out/chart.html", "data.json", true},
		{".mewla/worktrees/repo/r.html", "d.csv", true},
		{"projects/q3/report.html", "data.csv", true},
	}
	for _, c := range cases {
		page := f.pagePath(t, c.page)
		if response := f.get(page); response.Code != http.StatusOK {
			t.Fatalf("%s: the page itself: status=%d", c.page, response.Code)
		}
		if served := f.get(folderOf(page)+c.sibling).Code == http.StatusOK; served != c.siblings {
			t.Fatalf("%s: sibling %s served=%v, want %v", c.page, c.sibling, served, c.siblings)
		}
	}
	if sessionPageServesFolder("/") || sessionPageServesFolder(filepath.Dir(home)) || sessionPageServesFolder(state) {
		t.Fatal("the filesystem root, a folder holding the home or the state root serve their files")
	}
}

func TestPrivateListenerServesSessionPages(t *testing.T) {
	if !privateAllowedPath("/session-page/abc.def/report.html") || !privateAllowedPath("/session-page-grant") {
		t.Fatal("the private listener refuses Session pages")
	}
	if privateAllowedPath("/session-pages") || privateAllowedPath("/pair") {
		t.Fatal("the private listener serves more than its routes")
	}
}
