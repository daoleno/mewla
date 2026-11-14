package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/watcher"
)

// codex_asset with max_edge answers a phone-sized picture; without it, the
// original bytes as before (older apps, the full-size viewer).
func TestCodexAssetMaxEdgeAnswersAPhoneSizedPicture(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1080, 2400))
	r := rand.New(rand.NewSource(1))
	for y := 0; y < 2400; y++ {
		for x := 0; x < 1080; x++ {
			v := uint8(200 + r.Intn(40))
			img.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	var original bytes.Buffer
	if err := png.Encode(&original, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	manager, privateKey, deviceID := sessionFileAuthFixture(t)
	server := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	conn := dialDeviceWebSocket(t, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws", privateKey, manager.DaemonID(), deviceID)
	defer conn.Close()

	ask := func(maxEdge int) (string, []byte) {
		t.Helper()
		request := map[string]any{"type": "codex_asset", "request_id": "pic", "path": path}
		if maxEdge > 0 {
			request["max_edge"] = maxEdge
		}
		if err := conn.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			var reply map[string]any
			if err := conn.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if reply["type"] != "codex_asset" {
				continue
			}
			url, _ := reply["data_url"].(string)
			head, payload, _ := strings.Cut(url, ";base64,")
			data, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				t.Fatal(err)
			}
			return strings.TrimPrefix(head, "data:"), data
		}
	}

	contentType, data := ask(1600)
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if contentType != "image/jpeg" || err != nil || config.Width != 720 || config.Height != 1600 || len(data) >= original.Len()/2 {
		t.Fatalf("max_edge 1600: %s %dx%d, %d bytes from %d (%v)", contentType, config.Width, config.Height, len(data), original.Len(), err)
	}
	contentType, data = ask(0)
	if contentType != "image/png" || !bytes.Equal(data, original.Bytes()) {
		t.Fatalf("without max_edge: %s of %d bytes, want the original PNG", contentType, len(data))
	}
}
