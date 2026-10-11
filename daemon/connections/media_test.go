package connections

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const pngBytes = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR-test-image-bytes"

func TestFalAPIKeyAccountNeverEchoesKey(t *testing.T) {
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "api.fal.ai" && r.URL.Path == "/v1/models/pricing":
			if r.Header.Get("Authorization") != "Key fal-good" {
				return reply(401, `{"detail":"bad key"}`), nil
			}
			return reply(200, `{"prices":[{"endpoint_id":"fal-ai/flux/schnell","unit_price":0.003}]}`), nil
		case r.URL.Host == "api.fal.ai" && r.URL.Path == "/v1/models":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("free search sent a key")
			}
			return reply(200, `{"models":[{"endpoint_id":"bytedance/seedance-2.5/text-to-video","metadata":{"display_name":"Seedance 2.5","category":"text-to-video"}}],"has_more":false}`), nil
		}
		return reply(404, `{}`), nil
	})
	// Without a key: free search works, a billed tool names where the key goes.
	keyless := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "fal", Name: "fal"}}).Account
	if keyless.Status != "authorization_required" || keyless.AuthMethod != "api_key" || keyless.Access.Write != "off" {
		t.Fatalf("keyless account: %+v", keyless)
	}
	found := mustHandle(t, m, Request{Action: "invoke", ID: keyless.ID, Tool: "search_models", Arguments: json.RawMessage(`{"query":"seedance"}`)})
	if !strings.Contains(string(found.Result), "bytedance/seedance-2.5/text-to-video") {
		t.Fatalf("search: %s", found.Result)
	}
	_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: keyless.ID, Tool: "run", Arguments: json.RawMessage(`{"model":"fal-ai/flux/schnell","input":{}}`)})
	if err == nil || !strings.Contains(err.Error(), "fal has no API key yet") || !strings.Contains(err.Error(), "Enter key") {
		t.Fatalf("missing key error: %v", err)
	}
	if got := mustHandle(t, m, Request{Action: "get", ID: keyless.ID}).Account; got.Status != "authorization_required" {
		t.Fatalf("free search marked a keyless account connected: %s", got.Status)
	}
	// A rejected key changes nothing; an accepted one is stored, never shown.
	if _, err := m.Handle(context.Background(), Request{Action: "set_key", ID: keyless.ID, Input: &Input{Credential: "fal-bad"}}); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("rejected key: %v", err)
	}
	account := mustHandle(t, m, Request{Action: "set_key", ID: keyless.ID, Input: &Input{Credential: " fal-good\t"}}).Account
	if account.Status != "connected" || account.Identity != "API key" {
		t.Fatalf("set key: %+v", account)
	}
	list := mustHandle(t, m, Request{Action: "list"})
	raw, _ := json.Marshal([]any{list, account})
	catalog, _ := os.ReadFile(m.path)
	if strings.Contains(string(raw), "fal-good") || strings.Contains(string(catalog), "fal-good") {
		t.Fatal("API key echoed")
	}
	if stored, _, _ := m.vault.Get("integration:" + account.ID); stored != "fal-good" {
		t.Fatal("API key not stored")
	}
}

func TestMediaToolSchemasRejectBadInput(t *testing.T) {
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, `{"prices":[]}`), nil
	})
	account := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "fal", Name: "fal", Credential: "fal-good"}}).Account
	mustHandle(t, m, Request{Action: "permissions", ID: account.ID, Group: "write", Allowed: true})
	for _, args := range []string{
		`{"model":"../etc/passwd","input":{}}`,
		`{"model":"fal-ai/flux/schnell"}`,
		`{"model":"fal-ai/flux/schnell","input":{},"extra":1}`,
	} {
		if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "run", Arguments: json.RawMessage(args)}); err == nil || !strings.Contains(err.Error(), "schema") {
			t.Fatalf("%s: %v", args, err)
		}
	}
	groups := map[string]string{}
	for _, tool := range mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account.Tools {
		groups[tool.Name] = tool.Group
	}
	want := map[string]string{"search_models": "read", "describe_model": "read", "status": "read", "result": "read", "run": "write", "submit": "write", "cancel": "write", "upload_file": "write"}
	for name, group := range want {
		if groups[name] != group {
			t.Fatalf("%s group %q", name, groups[name])
		}
	}
}

func imageManager(t *testing.T, generate func(*http.Request) (*http.Response, error)) (*Manager, *Account) {
	t.Helper()
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer img-key" {
			return reply(401, `{}`), nil
		}
		if r.URL.Path == "/v1/models" {
			return reply(200, `{"data":[]}`), nil
		}
		return generate(r)
	})
	account := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "image", Name: "image-01", Endpoint: "https://img.example/v1/", Model: "img-model", Credential: "img-key"}}).Account
	if account.Status != "connected" || account.Model != "img-model" || account.Identity != "img.example · img-model" || account.Access.Write != "off" {
		t.Fatalf("image account: %+v", account)
	}
	mustHandle(t, m, Request{Action: "policy", ID: account.ID, Tool: "generate", Allowed: true})
	return m, account
}

func TestImageOutputIsAFileNotBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(pngBytes))
	m, account := imageManager(t, func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/images/generations" || body["model"] != "img-model" || body["size"] != "1024x1024" {
			t.Errorf("request: %s %v", r.URL.Path, body)
		}
		return reply(200, `{"created":1,"data":[{"b64_json":"`+encoded+`","revised_prompt":"a cat"}],"usage":{"total_tokens":10}}`), nil
	})
	args := json.RawMessage(`{"prompt":"a cat","size":"1024x1024","output_dir":"out"}`)
	// Only the local CLI may name a folder.
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "generate", Arguments: args, Cwd: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "mewla CLI") {
		t.Fatalf("remote output_dir: %v", err)
	}
	cwd := t.TempDir()
	result := mustLocal(t, m, Request{Action: "invoke", ID: account.ID, Tool: "generate", Arguments: args, Cwd: cwd})
	if strings.Contains(string(result), encoded) || strings.Contains(string(result), "b64_json") {
		t.Fatalf("base64 in result: %s", result)
	}
	var out struct {
		Images []struct {
			Path, Mime string
			Bytes      int64
		}
		OutputDir string `json:"output_dir"`
		Usage     map[string]any
	}
	if err := json.Unmarshal(result, &out); err != nil || len(out.Images) != 1 || out.OutputDir != filepath.Join(cwd, "out") || out.Usage == nil {
		t.Fatalf("result: %s", result)
	}
	image := out.Images[0]
	written, err := os.ReadFile(image.Path)
	if err != nil || string(written) != pngBytes || image.Mime != "image/png" || image.Bytes != int64(len(pngBytes)) || filepath.Dir(image.Path) != out.OutputDir || filepath.Ext(image.Path) != ".png" {
		t.Fatalf("file %s: %v %+v", image.Path, err, image)
	}
	// Without a folder, files go to the daemon's media folder.
	result = mustLocal(t, m, Request{Action: "invoke", ID: account.ID, Tool: "generate", Arguments: json.RawMessage(`{"prompt":"a cat","size":"1024x1024"}`)})
	if !strings.Contains(string(result), filepath.Join(filepath.Dir(m.path), "media", "image")) {
		t.Fatalf("default folder: %s", result)
	}
	// Edits need a granted tool, like any change.
	if _, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "edit", Arguments: json.RawMessage(`{"prompt":"x","images":["a.png"]}`)}); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("edit without grant: %v", err)
	}
}

func mustLocal(t *testing.T, m *Manager, q Request) json.RawMessage {
	t.Helper()
	response, err := m.Handle(WithLocalCaller(context.Background()), q)
	if err != nil {
		t.Fatal(err)
	}
	return response.Result
}

func TestMediaCallReleasesManagerLock(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	m, account := imageManager(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return reply(400, `{"error":{"message":"prompt rejected by policy"}}`), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := m.Handle(context.Background(), Request{Action: "invoke", ID: account.ID, Tool: "generate", Arguments: json.RawMessage(`{"prompt":"x"}`)})
		done <- err
	}()
	<-started
	listed := make(chan struct{})
	go func() { mustHandle(t, m, Request{Action: "list"}); close(listed) }()
	select {
	case <-listed:
	case <-time.After(2 * time.Second):
		t.Fatal("list waited for a media call")
	}
	close(release)
	err := <-done
	var vendor vendorError
	if !errors.As(err, &vendor) || !strings.Contains(err.Error(), "prompt rejected by policy") {
		t.Fatalf("vendor error: %v", err)
	}
	got := mustHandle(t, m, Request{Action: "get", ID: account.ID}).Account
	if got.Status != "connected" || got.History[0].Message != "Image generation returned HTTP 400" {
		t.Fatalf("after a rejected input: %s %+v", got.Status, got.History[0])
	}
}

func TestFalDescribeModelReadsOpenAPISchemas(t *testing.T) {
	model := "bytedance/seedance-2.5/text-to-video"
	doc := func(recursive bool) string {
		input := `{"type":"object","properties":{"prompt":{"type":"string"},"duration":{"$ref":"#/components/schemas/Duration"}},"required":["prompt"]}`
		if recursive {
			input = `{"type":"object","properties":{"prompt":{"type":"string"},"next":{"$ref":"#/components/schemas/Input"}}}`
		}
		return `{"openapi":"3.0.4","components":{"schemas":{"Input":` + input + `,"Duration":{"type":"integer","enum":[5,10]},"Output":{"type":"object","properties":{"video":{"type":"object"}}}}},
		"paths":{"/` + model + `":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Input"}}}}}},
		"/bytedance/seedance-2.5/requests/{request_id}":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Output"}}}}}}}}}`
	}
	recursive := false
	m := testManager(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("expand") != "openapi-3.0" || r.Header.Get("Authorization") != "" {
			return reply(404, `{}`), nil
		}
		return reply(200, `{"models":[{"endpoint_id":"`+model+`","metadata":{"display_name":"Seedance 2.5"},"openapi":`+doc(recursive)+`}]}`), nil
	})
	account := mustHandle(t, m, Request{Action: "add", Input: &Input{Integration: "fal", Name: "fal"}}).Account
	describe := func() map[string]any {
		var out map[string]any
		raw := mustHandle(t, m, Request{Action: "invoke", ID: account.ID, Tool: "describe_model", Arguments: json.RawMessage(`{"model":"` + model + `"}`)}).Result
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	inlined := describe()["input_schema"].(map[string]any)
	if inlined["properties"].(map[string]any)["duration"].(map[string]any)["type"] != "integer" {
		t.Fatalf("inlined input: %v", inlined)
	}
	recursive = true
	// A recursive schema keeps its references and ships their definitions.
	kept := describe()["input_schema"].(map[string]any)
	if kept["schema"].(map[string]any)["$ref"] != "#/components/schemas/Input" || kept["components"] == nil {
		t.Fatalf("recursive input: %v", kept)
	}
}
