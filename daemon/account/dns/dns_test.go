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

func TestChalltestsrvAddress(t *testing.T) {
	rec := &recorder{}
	server := httptest.NewServer(rec)
	defer server.Close()
	provider := &Challtestsrv{BaseURL: server.URL, Client: server.Client()}
	ctx := context.Background()
	if err := provider.SetA(ctx, "studio.m.test", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	if err := provider.ClearA(ctx, "studio.m.test"); err != nil {
		t.Fatal(err)
	}
	set, clear := rec.requests[0], rec.requests[1]
	if set.path != "/add-a" || set.body["host"] != "studio.m.test." || set.body["address"] != "203.0.113.7" {
		t.Fatalf("set = %+v", set)
	}
	if clear.path != "/clear-a" || clear.body["host"] != "studio.m.test." {
		t.Fatalf("clear = %+v", clear)
	}
}

// An override is exactly one A record of the machine's own name: a new one is
// created, an existing one is updated in place, extras are removed, and a
// wildcard record of the same zone is never touched.
func TestCloudflareAddressRecords(t *testing.T) {
	rec := &recorder{}
	existing := `[]`
	rec.reply = func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return http.StatusOK, `{"success":true,"errors":[],"result":` + existing + `}`
		}
		return http.StatusOK, `{"success":true,"errors":[],"result":{"id":"rec-x"}}`
	}
	server := httptest.NewServer(rec)
	defer server.Close()
	provider := &Cloudflare{ZoneID: "zone-1", Token: "cf-token", BaseURL: server.URL, Client: server.Client()}
	ctx := context.Background()

	if err := provider.SetA(ctx, "Studio.M.Test.", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	list, create := rec.requests[0], rec.requests[1]
	if list.method != http.MethodGet || !strings.Contains(list.query, "type=A") || !strings.Contains(list.query, "name=studio.m.test") {
		t.Fatalf("list = %+v", list)
	}
	if create.method != http.MethodPost || create.body["type"] != "A" || create.body["name"] != "studio.m.test" ||
		create.body["content"] != "203.0.113.7" || create.body["ttl"] != float64(60) {
		t.Fatalf("create = %+v", create)
	}

	rec.requests = nil
	existing = `[{"id":"rec-1","type":"A","name":"studio.m.test","content":"198.51.100.1"},
		{"id":"rec-2","type":"A","name":"studio.m.test","content":"198.51.100.2"},
		{"id":"rec-w","type":"A","name":"*.m.test","content":"198.51.100.3"}]`
	if err := provider.SetA(ctx, "studio.m.test", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	update, extra := rec.requests[1], rec.requests[2]
	if update.method != http.MethodPut || update.path != "/client/v4/zones/zone-1/dns_records/rec-1" || update.body["content"] != "203.0.113.7" {
		t.Fatalf("update = %+v", update)
	}
	if extra.method != http.MethodDelete || extra.path != "/client/v4/zones/zone-1/dns_records/rec-2" || len(rec.requests) != 3 {
		t.Fatalf("requests = %+v", rec.requests)
	}

	rec.requests = nil
	if err := provider.ClearA(ctx, "studio.m.test"); err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, request := range rec.requests[1:] {
		deleted = append(deleted, request.path)
	}
	if len(deleted) != 2 || strings.Contains(strings.Join(deleted, " "), "rec-w") {
		t.Fatalf("clear deleted %v, want rec-1 and rec-2 and never the wildcard", deleted)
	}

	rec.requests, existing = nil, `[]`
	if err := provider.ClearA(ctx, "nobody.m.test"); err != nil || len(rec.requests) != 1 {
		t.Fatalf("clearing a name with no record: err=%v requests=%+v", err, rec.requests)
	}
}
