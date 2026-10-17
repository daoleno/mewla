package pluginreturn

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func serve(method, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	Handler(w, httptest.NewRequest(method, "https://connect.mewla.ai"+Path+"?"+query, nil))
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
