package dns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recorded struct {
	method string
	path   string
	query  string
	auth   string
	body   map[string]any
}

type recorder struct {
	mu       sync.Mutex
	requests []recorded
	reply    func(r *http.Request) (int, string)
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	rec.mu.Lock()
	rec.requests = append(rec.requests, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), body})
	rec.mu.Unlock()
	status, response := http.StatusOK, ""
	if rec.reply != nil {
		status, response = rec.reply(r)
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, response)
}

func TestChalltestsrv(t *testing.T) {
	rec := &recorder{}
	server := httptest.NewServer(rec)
	defer server.Close()
	provider := &Challtestsrv{BaseURL: server.URL + "/", Client: server.Client()}
	ctx := context.Background()
	if err := provider.SetTXT(ctx, "_acme-challenge.studio-3f9a2c.link.test", "token"); err != nil {
		t.Fatal(err)
	}
	if err := provider.ClearTXT(ctx, "_acme-challenge.studio-3f9a2c.link.test.", "token"); err != nil {
		t.Fatal(err)
	}
	if len(rec.requests) != 2 {
		t.Fatalf("requests = %+v", rec.requests)
	}
	set, clear := rec.requests[0], rec.requests[1]
	if set.method != http.MethodPost || set.path != "/set-txt" ||
		set.body["host"] != "_acme-challenge.studio-3f9a2c.link.test." || set.body["value"] != "token" {
		t.Fatalf("set = %+v", set)
	}
	if clear.method != http.MethodPost || clear.path != "/clear-txt" ||
		clear.body["host"] != "_acme-challenge.studio-3f9a2c.link.test." || len(clear.body) != 1 {
		t.Fatalf("clear = %+v", clear)
	}

	rec.reply = func(*http.Request) (int, string) { return http.StatusBadRequest, "" }
	if err := provider.SetTXT(ctx, "x.test", "v"); err == nil {
		t.Fatal("set succeeded on 400")
	}
}

func TestCloudflare(t *testing.T) {
	rec := &recorder{}
	rec.reply = func(r *http.Request) (int, string) {
		switch r.Method {
		case http.MethodPost:
			return http.StatusOK, `{"success":true,"errors":[],"result":{"id":"rec-new"}}`
		case http.MethodGet:
			return http.StatusOK, `{"success":true,"errors":[],"result":[
				{"id":"rec-1","type":"TXT","name":"_acme-challenge.studio-3f9a2c.link.test","content":"token"},
				{"id":"rec-2","type":"TXT","name":"_acme-challenge.studio-3f9a2c.link.test","content":"other"}]}`
		case http.MethodDelete:
			return http.StatusOK, `{"success":true,"errors":[],"result":{"id":"rec-1"}}`
		}
		return http.StatusMethodNotAllowed, `{"success":false}`
	}
	server := httptest.NewServer(rec)
	defer server.Close()
	provider := &Cloudflare{ZoneID: "zone-1", Token: "cf-token", BaseURL: server.URL, Client: server.Client()}
	ctx := context.Background()
	if err := provider.SetTXT(ctx, "_acme-challenge.studio-3f9a2c.link.test", "token"); err != nil {
		t.Fatal(err)
	}
	if err := provider.ClearTXT(ctx, "_acme-challenge.studio-3f9a2c.link.test", "token"); err != nil {
		t.Fatal(err)
	}
	if len(rec.requests) != 3 {
		t.Fatalf("requests = %+v", rec.requests)
	}
	create, list, remove := rec.requests[0], rec.requests[1], rec.requests[2]
	for _, request := range rec.requests {
		if request.auth != "Bearer cf-token" {
			t.Fatalf("auth = %q", request.auth)
		}
	}
	if create.method != http.MethodPost || create.path != "/client/v4/zones/zone-1/dns_records" ||
		create.body["type"] != "TXT" || create.body["name"] != "_acme-challenge.studio-3f9a2c.link.test" ||
		create.body["content"] != "token" || create.body["ttl"] != float64(60) {
		t.Fatalf("create = %+v", create)
	}
	if list.method != http.MethodGet || list.path != "/client/v4/zones/zone-1/dns_records" ||
		!strings.Contains(list.query, "type=TXT") ||
		!strings.Contains(list.query, "name=_acme-challenge.studio-3f9a2c.link.test") ||
		!strings.Contains(list.query, "content=token") {
		t.Fatalf("list = %+v", list)
	}
	if remove.method != http.MethodDelete || remove.path != "/client/v4/zones/zone-1/dns_records/rec-1" {
		t.Fatalf("delete = %+v", remove)
	}

	rec.reply = func(*http.Request) (int, string) {
		return http.StatusForbidden, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`
	}
	err := provider.SetTXT(ctx, "x.test", "v")
	if err == nil || !strings.Contains(err.Error(), "10000") || strings.Contains(err.Error(), "cf-token") {
		t.Fatalf("error = %v", err)
	}
}
