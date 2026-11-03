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

// A sign-in a browser started from the web UI goes back to that web UI's own
// callback, named in the state, in the same tab.
func TestWebSignInReturnsToTheOriginNamedInItsState(t *testing.T) {
	for origin, want := range map[string]string{
		"https://mewla.example":    "https://mewla.example/plugins/oauth/callback",
		"http://127.0.0.1:9877":    "http://127.0.0.1:9877/plugins/oauth/callback",
		"http://[::1]:9877":        "http://[::1]:9877/plugins/oauth/callback",
		"https://mewla.example:44": "https://mewla.example:44/plugins/oauth/callback",
	} {
		state := WebState(origin, "s1")
		w := serve("GET", url.Values{"code": {"abc"}, "state": {state}, "scope": {"x"}, "next": {"//evil"}}.Encode())
		location, _ := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusFound || location.Scheme+"://"+location.Host+location.Path != want || len(location.Query()) != 2 || location.Query().Get("code") != "abc" || location.Query().Get("state") != state {
			t.Fatalf("%s: %d %s", origin, w.Code, w.Header().Get("Location"))
		}
	}
	denied := serve("GET", url.Values{"error": {"access_denied"}, "state": {WebState("https://mewla.example", "s2")}}.Encode())
	if location, _ := url.Parse(denied.Header().Get("Location")); denied.Code != http.StatusFound || location.Host != "mewla.example" || location.Query().Get("error") != "access_denied" {
		t.Fatalf("denied %d %s", denied.Code, denied.Header().Get("Location"))
	}
	// The app's sign-in keeps its redirect.
	if w := serve("GET", "code=abc&state=s1"); w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), Target) {
		t.Fatal("an app sign-in no longer redirects to the app")
	}
}

// Only an https or loopback origin, with nothing after it, is a place to send
// a browser; anything else in a web state is refused, never followed.
func TestWebSignInRefusesOtherOrigins(t *testing.T) {
	encode := func(origin string) string {
		return "web." + base64.RawURLEncoding.EncodeToString([]byte(origin)) + ".s"
	}
	for _, state := range []string{
		encode("http://mewla.example"),
		encode("http://192.168.1.20:9877"),
		encode("https://mewla.example/path"),
		encode("https://mewla.example/?x=1"),
		encode("https://user@mewla.example"),
		encode("javascript:alert(1)"),
		encode("//evil.example"),
		encode(""),
		"web.%%%.s",
		"web.nodot",
		"web.",
	} {
		w := serve("GET", url.Values{"code": {"abc"}, "state": {state}}.Encode())
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
			t.Fatalf("%q: %d %s", state, w.Code, w.Header().Get("Location"))
		}
	}
}
