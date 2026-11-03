package pluginreturn

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func serve(method, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(w, httptest.NewRequest(method, Origin+Path+"?"+query, nil))
	return w
}

func TestForwardsOnlyTheAuthorizationResultToTheApp(t *testing.T) {
	w := serve("GET", "code=abc&state=s1&iss=https%3A%2F%2Fmcp.cloudflare.com&scope=x&redirect=https%3A%2F%2Fevil.example&next=//evil")
	if w.Code != http.StatusFound || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	location, _ := url.Parse(w.Header().Get("Location"))
	if location.Scheme != "mewla" || location.Host != "plugins" || location.Path != "" {
		t.Fatalf("location %s", location)
	}
	if q := location.Query(); len(q) != 3 || q.Get("code") != "abc" || q.Get("state") != "s1" || q.Get("iss") != "https://mcp.cloudflare.com" {
		t.Fatalf("forwarded %v", q)
	}
	denied, _ := url.Parse(serve("GET", "error=access_denied&state=s2&error_description=no").Header().Get("Location"))
	if q := denied.Query(); len(q) != 2 || q.Get("error") != "access_denied" || q.Get("state") != "s2" {
		t.Fatalf("denial forwarded %v", q)
	}
}

// Slack returns code and state, or error and state, with no iss.
func TestForwardsSlackReturns(t *testing.T) {
	for query, want := range map[string]url.Values{
		"code=12347206286420.123.abcdef&state=s1":                              {"code": {"12347206286420.123.abcdef"}, "state": {"s1"}},
		"error=invalid_team_for_non_distributed_app&state=s2":                  {"error": {"invalid_team_for_non_distributed_app"}, "state": {"s2"}},
		"error=access_denied&state=s3&error_description=x&team=T1&enterprise=": {"error": {"access_denied"}, "state": {"s3"}},
	} {
		location, _ := url.Parse(serve("GET", query).Header().Get("Location"))
		if location.Scheme != "mewla" || location.Host != "plugins" || location.Query().Encode() != want.Encode() {
			t.Fatalf("%q forwarded %s", query, location)
		}
	}
}

func TestRejectsMalformedReturns(t *testing.T) {
	for _, query := range []string{
		"",
		"code=abc",
		"state=s",
		"code=a&code=b&state=s",
		"code=a&state=s&state=t",
		"code=a&error=access_denied&state=s",
		"error=%3Cscript%3E&state=s",
		"code=a&state=s&iss=javascript%3Aalert(1)",
		"code=a&state=s&iss=http%3A%2F%2Fmcp.cloudflare.com",
		"code=a&state=%zz",
	} {
		if w := serve("GET", query); w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
			t.Fatalf("%q: %d %s", query, w.Code, w.Header().Get("Location"))
		}
	}
	if w := serve("POST", "code=a&state=s"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
}

// A web sign-in from this computer's own loopback address goes back to that
// web UI's callback in the same tab.
func TestWebSignInOnLoopbackReturnsToItsOrigin(t *testing.T) {
	for origin, want := range map[string]string{
		"http://127.0.0.1:9877":  "http://127.0.0.1:9877/plugins/oauth/callback",
		"http://[::1]:9877":      "http://[::1]:9877/plugins/oauth/callback",
		"http://localhost:3000":  "http://localhost:3000/plugins/oauth/callback",
		"http://127.0.0.1:65535": "http://127.0.0.1:65535/plugins/oauth/callback",
	} {
		state := WebState(origin, "s1")
		w := serve("GET", url.Values{"code": {"abc"}, "state": {state}, "scope": {"x"}, "next": {"//evil"}}.Encode())
		location, _ := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusFound || location.Scheme+"://"+location.Host+location.Path != want || len(location.Query()) != 2 || location.Query().Get("code") != "abc" || location.Query().Get("state") != state {
			t.Fatalf("%s: %d %s", origin, w.Code, w.Header().Get("Location"))
		}
	}
	denied := serve("GET", url.Values{"error": {"access_denied"}, "state": {WebState("http://127.0.0.1:9877", "s2")}}.Encode())
	if location, _ := url.Parse(denied.Header().Get("Location")); denied.Code != http.StatusFound || location.Host != "127.0.0.1:9877" || location.Query().Get("error") != "access_denied" {
		t.Fatalf("denied %d %s", denied.Code, denied.Header().Get("Location"))
	}
	// The app's sign-in keeps its redirect.
	if w := serve("GET", "code=abc&state=s1"); w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), Target) {
		t.Fatal("an app sign-in no longer redirects to the app")
	}
}

// The phishing scenario: an attacker starts a sign-in on their own server and
// sends the victim a link whose state names that server. The relay must not
// hand the victim's code to it, by redirect or by script; the code appears
// only on the page the victim is looking at. The same goes for origins that
// merely look like loopback.
func TestWebSignInNeverHandsACodeToAnotherOrigin(t *testing.T) {
	encode := func(origin string) string {
		return "web." + base64.RawURLEncoding.EncodeToString([]byte(origin)) + ".s"
	}
	for _, state := range []string{
		WebState("https://attacker.example", "s"),
		WebState("https://mewla.example:44", "s"),
		WebState("http://192.168.1.20:9877", "s"),
		encode("http://127.0.0.1.evil.com:9877"),
		encode("http://localhost.evil:9877"),
		encode("http://127.0.0.1@evil.example:9877"),
		encode("http://user@127.0.0.1:9877"),
		encode("http://2130706433:9877"),
		encode("http://0x7f.0.0.1:9877"),
		encode("http://0177.0.0.1:9877"),
		encode("http://127.1:9877"),
		encode("http://127.0.0.1"),
		encode("http://127.0.0.1:0"),
		encode("http://127.0.0.1:65536"),
		encode("http://127.0.0.1:99999"),
		encode("http://127.0.0.1:9877/path"),
		encode("http://127.0.0.1:9877?x=1"),
		encode("https://127.0.0.1:9877"),
		encode("http://[::1]"),
		encode("javascript:alert(1)"),
		encode("//evil.example"),
		encode(""),
		"web.%%%.s",
		"web.nodot",
		"web.",
	} {
		w := serve("GET", url.Values{"code": {"victim-code"}, "state": {state}}.Encode())
		body := w.Body.String()
		csp := w.Header().Get("Content-Security-Policy")
		if w.Code != http.StatusOK || w.Header().Get("Location") != "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%q: %d location %q", state, w.Code, w.Header().Get("Location"))
		}
		if !strings.Contains(body, "victim-code") || !strings.Contains(body, "Only paste this into your own Mewla.") || strings.Contains(body, "<script") || !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") || strings.Contains(csp, "connect-src") {
			t.Fatalf("%q: page %s csp %q", state, body, csp)
		}
	}
	page := serve("GET", url.Values{"code": {"abc"}, "state": {"web.x.s"}, "scope": {"evil"}}.Encode()).Body.String()
	if !strings.Contains(page, "https://connect.mewla.ai"+Path+"?code=abc&amp;state=web.x.s<") || strings.Contains(page, "evil") {
		t.Fatalf("page %s", page)
	}
	denied := serve("GET", url.Values{"error": {"access_denied"}, "state": {WebState("https://attacker.example", "s")}}.Encode()).Body.String()
	if !strings.Contains(denied, "Access was not granted") {
		t.Fatalf("denied page %s", denied)
	}
}

func TestIsLoopbackOrigin(t *testing.T) {
	for origin, want := range map[string]bool{
		"http://127.0.0.1:9877": true, "http://[::1]:1": true, "http://localhost:65535": true,
		"http://127.0.0.1": false, "http://127.0.0.1:65536": false, "http://127.0.0.1:09877": false,
		"http://127.0.0.2:9877": false, "https://localhost:9877": false, "http://localhost.:9877": false,
	} {
		if IsLoopbackOrigin(origin) != want {
			t.Errorf("%s: want %v", origin, want)
		}
	}
}
