package modelprofiles

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type capturedImageRequest struct {
	path, contentType string
	contentLength     int64
	body              []byte
}

// imageModelGateway serves a gateway whose selected Codex connection carries
// imageModel, recording what reaches the upstream.
func imageModelGateway(t *testing.T, imageModel string) (*Gateway, *capturedImageRequest, *atomic.Int32) {
	t.Helper()
	got := &capturedImageRequest{}
	calls := &atomic.Int32{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got.path = r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.contentLength = r.ContentLength
		got.body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"aW1n"}]}`)
	}))
	t.Cleanup(upstream.Close)
	g := NewGateway("127.0.0.1:0", NewMemoryCredentialStore(), WithGatewayRequestResolver(func(string, string) (GatewayUpstream, error) {
		return GatewayUpstream{ProfileID: "p", BaseURL: upstream.URL + "/v1", Protocol: ProtocolOpenAIResponses, ImageModel: imageModel}, nil
	}))
	if err := g.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g, got, calls
}

func postImageRequest(t *testing.T, g *Gateway, path, contentType string, body []byte, header map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+g.ActualAddr()+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// largeImage stands in for a multi-MB reference image, including bytes that
// look like JSON and multipart syntax.
func largeImage() []byte {
	return bytes.Repeat([]byte("\x89PNG\r\n\"model\":{[--x]}\x00"), 1<<16)
}

func multipartImageBody(t *testing.T, fields [][2]string, image []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, field := range fields {
		if err := mw.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("image[]", "ref.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(image)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// TestGatewayRewritesCodexImageModelOnGenerations: Codex names gpt-image-2;
// the connection's image_model replaces only that value.
func TestGatewayRewritesCodexImageModelOnGenerations(t *testing.T) {
	g, got, _ := imageModelGateway(t, "gpt-image-2.5-sunburst")
	in := []byte(`{"prompt":"a \"model\": cat","model" : "gpt-image-2","size":"1024x1024","n":1}`)
	want := []byte(`{"prompt":"a \"model\": cat","model" : "gpt-image-2.5-sunburst","size":"1024x1024","n":1}`)
	if code := postImageRequest(t, g, "/v1/images/generations", "application/json", in, nil); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.path != "/v1/images/generations" || !bytes.Equal(got.body, want) || got.contentLength != int64(len(want)) {
		t.Fatalf("upstream path=%q len=%d body=%s", got.path, got.contentLength, got.body)
	}
}

// TestGatewayRewritesCodexImageModelOnEdits covers both edit bodies the images
// API accepts: JSON with data-URL references and multipart/form-data with the
// image as a file part. Every byte but the model survives.
func TestGatewayRewritesCodexImageModelOnEdits(t *testing.T) {
	image := largeImage()
	t.Run("json", func(t *testing.T) {
		g, got, _ := imageModelGateway(t, "gpt-image-2.5-sunburst")
		ref := `{"image_url":"data:image/png;base64,` + strings.Repeat("QUJD", 1<<18) + `"}`
		in := []byte(`{"images":[` + ref + `],"prompt":"make it blue","model":"gpt-image-2"}`)
		want := []byte(`{"images":[` + ref + `],"prompt":"make it blue","model":"gpt-image-2.5-sunburst"}`)
		if code := postImageRequest(t, g, "/v1/images/edits", "application/json", in, nil); code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if !bytes.Equal(got.body, want) || got.contentLength != int64(len(want)) {
			t.Fatalf("upstream len=%d, want %d", len(got.body), len(want))
		}
	})
	t.Run("multipart", func(t *testing.T) {
		g, got, _ := imageModelGateway(t, "gpt-image-2.5-sunburst")
		in, contentType := multipartImageBody(t, [][2]string{{"prompt", "make it blue"}, {"model", "gpt-image-2"}, {"model_note", "keep"}}, image)
		want, _ := multipartImageBody(t, [][2]string{{"prompt", "make it blue"}, {"model", "gpt-image-2.5-sunburst"}, {"model_note", "keep"}}, image)
		want = bytes.ReplaceAll(want, []byte(boundaryOf(t, want)), []byte(boundaryOf(t, in)))
		if code := postImageRequest(t, g, "/v1/images/edits", contentType, in, nil); code != http.StatusOK {
			t.Fatalf("status = %d", code)
		}
		if got.contentType != contentType || !bytes.Equal(got.body, want) || got.contentLength != int64(len(want)) {
			t.Fatalf("upstream ct=%q len=%d, want %d", got.contentType, len(got.body), len(want))
		}
		form, err := multipart.NewReader(bytes.NewReader(got.body), boundaryOf(t, in)).ReadForm(32 << 20)
		if err != nil || form.Value["model"][0] != "gpt-image-2.5-sunburst" || form.Value["model_note"][0] != "keep" || len(form.File["image[]"]) != 1 {
			t.Fatalf("upstream form = %+v err=%v", form, err)
		}
	})
}

func boundaryOf(t *testing.T, body []byte) string {
	t.Helper()
	line, _, _ := bytes.Cut(body, []byte("\r\n"))
	return strings.TrimPrefix(string(line), "--")
}

// TestGatewayAddsCodexImageModelWhenAbsent: a body without a model still goes
// to the configured image model.
func TestGatewayAddsCodexImageModelWhenAbsent(t *testing.T) {
	g, got, _ := imageModelGateway(t, "gpt-image-2.5-sunburst")
	if code := postImageRequest(t, g, "/v1/images/generations", "application/json", []byte(`{"prompt":"cat"}`), nil); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if string(got.body) != `{"model":"gpt-image-2.5-sunburst","prompt":"cat"}` {
		t.Fatalf("json body = %s", got.body)
	}
	in, contentType := multipartImageBody(t, [][2]string{{"prompt", "blue"}}, []byte("png"))
	if code := postImageRequest(t, g, "/v1/images/edits", contentType, in, nil); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	form, err := multipart.NewReader(bytes.NewReader(got.body), boundaryOf(t, in)).ReadForm(1 << 20)
	if err != nil || form.Value["model"][0] != "gpt-image-2.5-sunburst" || form.Value["prompt"][0] != "blue" {
		t.Fatalf("multipart form = %+v err=%v", form, err)
	}
}

// TestGatewayPassesCodexImageRequestsThroughWithoutImageModel: unset keeps
// today's byte-for-byte passthrough.
func TestGatewayPassesCodexImageRequestsThroughWithoutImageModel(t *testing.T) {
	g, got, _ := imageModelGateway(t, "")
	multipartBody, multipartType := multipartImageBody(t, [][2]string{{"model", "gpt-image-2"}}, largeImage())
	for _, tc := range []struct {
		path, contentType string
		body              []byte
	}{
		{"/v1/images/generations", "application/json", []byte(`{"model":"gpt-image-2","prompt":"cat"}`)},
		{"/v1/images/edits", multipartType, multipartBody},
	} {
		if code := postImageRequest(t, g, tc.path, tc.contentType, tc.body, nil); code != http.StatusOK {
			t.Fatalf("%s: status = %d", tc.path, code)
		}
		if !bytes.Equal(got.body, tc.body) {
			t.Fatalf("%s: body changed", tc.path)
		}
	}
}

// TestGatewayLeavesCodexSearchUntouchedWithImageModel: web.run search is not
// an image call even when its body names a model.
func TestGatewayLeavesCodexSearchUntouchedWithImageModel(t *testing.T) {
	g, got, _ := imageModelGateway(t, "gpt-image-2.5-sunburst")
	in := []byte(`{"model":"gpt-5-search","query":"cats"}`)
	if code := postImageRequest(t, g, "/v1/alpha/search", "application/json", in, nil); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got.path != "/v1/alpha/search" || !bytes.Equal(got.body, in) {
		t.Fatalf("search upstream path=%q body=%s", got.path, got.body)
	}
}

// TestGatewayRefusesEncodedCodexImageRequestWithImageModel: a compressed body
// cannot be spliced, so it fails honestly instead of reaching the upstream
// with the wrong model.
func TestGatewayRefusesEncodedCodexImageRequestWithImageModel(t *testing.T) {
	g, _, calls := imageModelGateway(t, "gpt-image-2.5-sunburst")
	code := postImageRequest(t, g, "/v1/images/generations", "application/json", []byte("\x28\xb5\x2f\xfd"), map[string]string{"Content-Encoding": "zstd"})
	if code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("status=%d upstream calls=%d", code, calls.Load())
	}
}

// TestProviderImageModelSetting: image_model lives on the Codex connection,
// reaches the gateway upstream, survives a connection edit, clears, and is
// refused on a Claude connection.
func TestProviderImageModelSetting(t *testing.T) {
	owner := startBuiltinVerifierOwner(t)
	creds := NewMemoryCredentialStore()
	owner.creds = creds
	owner.router.creds = creds
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-codex", Name: "codex", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://codex.example/v1", Advanced: true,
	}, "secret", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-claude", Name: "claude", Client: ClientClaude, PresetID: ProviderPresetCustom,
		BaseURL: "https://claude.example", Advanced: true,
	}, "secret", proj.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderImageModel("", "gpt-image-2.5-sunburst"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no selection error = %v", err)
	}
	if proj, err = owner.SetProviderConnection("codex", "conn-codex", proj.Revision); err != nil {
		t.Fatal(err)
	}
	if proj, err = owner.SetProviderImageModel("", "gpt-image-2.5-sunburst"); err != nil {
		t.Fatal(err)
	}
	if conn := projectedConnection(t, proj, "conn-codex"); conn.ImageModel != "gpt-image-2.5-sunburst" {
		t.Fatalf("projected connection = %+v", conn)
	}
	if up, err := owner.resolveGatewayRequest(GatewayProtocolCodexTools, ""); err != nil || up.ImageModel != "gpt-image-2.5-sunburst" {
		t.Fatalf("tool upstream = %+v err=%v", up, err)
	}

	// An app edit of the connection does not carry image_model; it is kept.
	if proj, err = owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "conn-codex", Name: "codex renamed", Client: ClientCodex, PresetID: ProviderPresetCustom,
		BaseURL: "https://codex.example/v1", Advanced: true,
	}, "", proj.Revision, false); err != nil {
		t.Fatal(err)
	}
	if stored, _ := owner.store.Get("conn-codex"); stored.ImageModel != "gpt-image-2.5-sunburst" || stored.Name != "codex renamed" {
		t.Fatalf("stored after edit = %+v", stored)
	}

	if _, err := owner.SetProviderImageModel("conn-claude", "gpt-image-2.5-sunburst"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("claude connection error = %v", err)
	}
	if _, err := owner.SetProviderImageModel("conn-codex", "bad model"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid model error = %v", err)
	}
	if _, err = owner.SetProviderImageModel("conn-codex", ""); err != nil {
		t.Fatal(err)
	}
	if up, err := owner.resolveGatewayRequest(GatewayProtocolCodexTools, ""); err != nil || up.ImageModel != "" {
		t.Fatalf("cleared tool upstream = %+v err=%v", up, err)
	}
}

func projectedConnection(t *testing.T, proj ProviderCatalogProjection, id string) ProviderConnection {
	t.Helper()
	for _, conn := range proj.Connections {
		if conn.ID == id {
			return conn
		}
	}
	t.Fatalf("connection %s not projected", id)
	return ProviderConnection{}
}
