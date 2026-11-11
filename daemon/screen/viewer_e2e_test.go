package screen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The end-to-end check without a phone: the app's real viewer (noVNC and its
// H.264 path, the WebView script of client/ui/more/screen/viewerSource.ts)
// runs in headless Chrome, served from localhost (a secure context, so
// WebCodecs exists), against the real RFB server on a Session. The page plays
// the NSH WebView host (window.nsh.postMessage out, message events in),
// reports what the viewer's canvas shows, and the test compares it with a
// direct capture.

// wsNetConn presents a WebSocket as the net.Conn the RFB server serves.
type wsNetConn struct {
	*websocket.Conn
	rmu, wmu sync.Mutex
	r        io.Reader
}

func (c *wsNetConn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for {
		if c.r == nil {
			kind, r, err := c.Conn.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				continue
			}
			c.r = r
		}
		n, err := c.r.Read(p)
		if err == io.EOF {
			c.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *wsNetConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.Conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsNetConn) command(text string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, []byte(text))
}

func (c *wsNetConn) SetDeadline(t time.Time) error {
	if err := c.Conn.SetReadDeadline(t); err != nil {
		return err
	}
	return c.Conn.SetWriteDeadline(t)
}

const e2ePage = `<!doctype html><meta charset=utf-8><body style="margin:0;background:#777;position:absolute;left:0;top:0;width:1300px;height:900px">
<script type=module>
const note = (text) => fetch('/log', { method: 'POST', body: text });
window.addEventListener('error', (e) => note('error: ' + e.message));
window.addEventListener('unhandledrejection', (e) => note('rejection: ' + e.reason));
let ws = { readyState: 0, send() {} };
const b64 = (bytes) => { let s = ''; for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000)); return btoa(s); };
// The NSH WebView host: the viewer answers through window.nsh.postMessage and
// takes batches as message events, both JSON strings.
window.nsh = { postMessage: (json) => {
  const m = JSON.parse(json);
  if (m.type !== 'stats' && m.type !== 'send') note('viewer: ' + m.type);
  if (m.type === 'send') { const bin = atob(m.data); const u = new Uint8Array(bin.length); for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i); if (ws.readyState === 1) ws.send(u); }
  else if (m.type === 'connected') note('connected ' + m.width + 'x' + m.height);
  else if (m.type === 'disconnected') note('disconnected clean=' + m.clean);
} };
const viewer = { handle: (batch) => window.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(batch) })) };
import('/runtime.js').then(() => {
  note('secure=' + isSecureContext + ' VideoDecoder=' + typeof VideoDecoder);
  // As the app does: the viewer is opened first, and the server's bytes are
  // relayed only once its channel exists (it waits up to 800 ms for the
  // H.264 probe).
  viewer.handle([{ type: 'open' }]);
  setTimeout(() => {
    ws = new WebSocket('ws://' + location.host + '/ws');
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => note('ws open');
    ws.onclose = (e) => note('ws close ' + e.code);
    let n = 0;
    ws.onmessage = (e) => {
      n++;
      if (typeof e.data === 'string') { if (e.data === 'full') snap('full'); return; }
      viewer.handle([{ type: 'rfb', data: [b64(new Uint8Array(e.data))], at: Date.now() }]);
    };
  }, 1500);
  const snap = (kind) => {
    const canvas = document.querySelector('canvas');
    if (!canvas || !canvas.width) return;
    let source = canvas;
    if (kind === 'crop') { source = document.createElement('canvas'); source.width = 400; source.height = 60; source.getContext('2d').drawImage(canvas, 0, 0, 400, 60, 0, 0, 400, 60); }
    source.toBlob((blob) => fetch('/snap?kind=' + kind, { method: 'POST', body: blob }), 'image/png');
  };
  setInterval(() => snap('crop'), 250);
});
</script>`

type e2eSnap struct {
	at     time.Time
	marker pageMarker
	ok     bool
}

type e2eResult struct {
	pageLog        []string
	crops          []e2eSnap
	videoUpdates   int
	updates        int
	distinctMoving int // distinct scroll positions the decoded canvas showed while the page was scrolling
	full           *image.RGBA
	direct         *image.RGBA
	fullMarker     pageMarker
	directMarker   pageMarker
	diff, psnr     float64
	edge           float64
}

// clientViewerSource is the script the client's Screen WebView runs: the
// SCREEN_VIEWER_SOURCE string of client/ui/more/screen/viewerSource.ts (a JSON
// string literal, written by its runtime/generate.mjs).
func clientViewerSource(t *testing.T) string {
	t.Helper()
	_, runtimeFile, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(runtimeFile), "..", "..", "client", "ui", "more", "screen", "viewerSource.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "export const SCREEN_VIEWER_SOURCE = "
	for _, line := range strings.Split(string(data), "\n") {
		if literal, ok := strings.CutPrefix(line, prefix); ok {
			var source string
			if err := json.Unmarshal([]byte(strings.TrimSuffix(literal, ";")), &source); err != nil {
				t.Fatalf("%s: SCREEN_VIEWER_SOURCE: %v", path, err)
			}
			return source
		}
	}
	t.Fatalf("%s has no SCREEN_VIEWER_SOURCE", path)
	return ""
}

// runViewerE2E serves session to the real viewer in Chrome until the page
// has scrolled and stopped, then compares the viewer's canvas with a direct
// capture. The session must show the marker (awaitFixture is called first).
func runViewerE2E(t *testing.T, ctx context.Context, session Session, video videoFactory, chrome, out string) e2eResult {
	t.Helper()
	var res e2eResult
	viewerJS := clientViewerSource(t)
	if _, _, err := awaitFixture(ctx, session, 12*time.Second); err != nil {
		t.Fatalf("not measurable: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var fullPNG []byte
	var conn *wsNetConn
	connected := make(chan struct{})
	var obsMu sync.Mutex
	var totalUpdates, totalVideo int
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, e2ePage)
	})
	mux.HandleFunc("/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = io.WriteString(w, viewerJS)
	})
	mux.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		res.pageLog = append(res.pageLog, string(b))
		mu.Unlock()
	})
	mux.HandleFunc("/snap", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.URL.Query().Get("kind") == "full" {
			mu.Lock()
			fullPNG = b
			mu.Unlock()
			return
		}
		dec, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return
		}
		img := image.NewRGBA(dec.Bounds())
		for y := 0; y < img.Rect.Dy(); y++ {
			for x := 0; x < img.Rect.Dx(); x++ {
				img.Set(x, y, dec.At(x, y))
			}
		}
		m, ok := readMarker(img)
		mu.Lock()
		res.crops = append(res.crops, e2eSnap{time.Now(), m, ok})
		mu.Unlock()
	})
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := &wsNetConn{Conn: ws}
		mu.Lock()
		conn = c
		mu.Unlock()
		close(connected)
		obs := &Observer{Stats: func(s StreamStats) { obsMu.Lock(); totalUpdates += s.Updates; totalVideo += s.Video; obsMu.Unlock() }}
		was := streamAdaptive
		streamAdaptive = true
		defer func() { streamAdaptive = was }()
		if err := AcceptClient(c); err != nil {
			return
		}
		_ = serveRFBVideo(ctx, c, session, "e2e", obs, video)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(mux)
	srv.Listener.Close()
	srv.Listener = listener
	srv.Start()
	defer srv.Close()

	profile := t.TempDir()
	args := []string{"--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir=" + profile,
		"--window-size=1300,900", "--disable-background-timer-throttling", "--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows", "--autoplay-policy=no-user-gesture-required"}
	if os.Getenv("MEWLA_CHROME_NO_SANDBOX") != "" {
		args = append(args, "--no-sandbox")
	}
	browser := exec.Command(chrome, append(args, srv.URL+"/")...)
	if err := browser.Start(); err != nil {
		t.Skipf("cannot start %s: %v", chrome, err)
	}
	defer func() { _ = browser.Process.Kill(); _, _ = browser.Process.Wait() }()

	select {
	case <-connected:
	case <-time.After(20 * time.Second):
		t.Fatalf("the viewer never connected; page log: %v", res.pageLog)
	}
	// Wait for the page to finish scrolling, as the viewer's canvas shows it.
	deadline := time.Now().Add(40 * time.Second)
	var doneAt time.Time
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		if n := len(res.crops); n > 0 && res.crops[n-1].ok && res.crops[n-1].marker.Phase == phaseDone {
			doneAt = res.crops[n-1].at
		}
		mu.Unlock()
		if !doneAt.IsZero() {
			break
		}
	}
	if doneAt.IsZero() {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("the viewer's canvas never showed the page done (%d snapshots); page log: %v", len(res.crops), res.pageLog)
	}
	time.Sleep(1500 * time.Millisecond) // let the sharp picture arrive
	mu.Lock()
	c := conn
	mu.Unlock()
	if err := c.command("full"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		mu.Lock()
		have := fullPNG != nil
		mu.Unlock()
		if have {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if fullPNG == nil {
		t.Fatalf("no full snapshot from the viewer; page log: %v", res.pageLog)
	}
	dec, err := png.Decode(bytes.NewReader(fullPNG))
	if err != nil {
		t.Fatal(err)
	}
	res.full = image.NewRGBA(dec.Bounds())
	for y := 0; y < res.full.Rect.Dy(); y++ {
		for x := 0; x < res.full.Rect.Dx(); x++ {
			res.full.Set(x, y, dec.At(x, y))
		}
	}
	direct, err := session.Frame(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	res.direct = direct.Image
	res.fullMarker, _ = readMarker(res.full)
	res.directMarker, _ = readMarker(res.direct)
	obsMu.Lock()
	res.videoUpdates, res.updates = totalVideo, totalUpdates
	obsMu.Unlock()
	seen := map[int]bool{}
	for _, s := range res.crops {
		if s.ok && s.marker.Phase == phaseScrolling {
			seen[s.marker.Pos] = true
		}
	}
	res.distinctMoving = len(seen)
	if res.full.Bounds() == res.direct.Bounds() {
		res.diff, res.psnr, res.edge = textMetrics(res.full, res.direct, nil, textCrop(res.direct.Bounds()))
	}
	if out != "" {
		for name, img := range map[string]*image.RGBA{"viewer": res.full, "direct": res.direct} {
			var buf bytes.Buffer
			if png.Encode(&buf, img.SubImage(textCrop(img.Bounds()))) == nil {
				_ = os.WriteFile(filepath.Join(out, "e2e-crop-"+name+".png"), buf.Bytes(), 0o600)
			}
		}
	}
	return res
}

// reportE2E checks what the viewer showed and returns a line for the table.
func reportE2E(t *testing.T, name string, video bool, r e2eResult) string {
	t.Helper()
	t.Logf("%s: page log %v", name, r.pageLog)
	var problems []string
	if r.full.Bounds() != r.direct.Bounds() {
		problems = append(problems, fmt.Sprintf("viewer canvas %v but direct capture %v", r.full.Bounds(), r.direct.Bounds()))
	}
	if r.fullMarker != r.directMarker {
		problems = append(problems, fmt.Sprintf("viewer shows [%v], direct capture [%v]", r.fullMarker, r.directMarker))
	}
	if r.directMarker.Phase != phaseDone || r.directMarker.Stalled {
		problems = append(problems, fmt.Sprintf("the page did not finish cleanly: %v", r.directMarker))
	}
	if r.distinctMoving < 5 {
		problems = append(problems, fmt.Sprintf("the viewer's canvas showed only %d distinct scroll positions while the page scrolled", r.distinctMoving))
	}
	if video && r.videoUpdates < 5 {
		problems = append(problems, fmt.Sprintf("video mode, but only %d of %d updates were video", r.videoUpdates, r.updates))
	}
	if len(problems) > 0 {
		return fmt.Sprintf("%-6s INVALID: %s", name, strings.Join(problems, "; "))
	}
	return fmt.Sprintf("%-6s decoded %d/%d updates as video, %d scroll positions seen on the viewer canvas; final scroll %d; vs direct capture: diff %.2f PSNR %.1f dB edge %.2f",
		name, r.videoUpdates, r.updates, r.distinctMoving, r.fullMarker.Pos, r.diff, r.psnr, r.edge)
}

// On a synthetic page, anywhere there is Chrome and ffmpeg: the viewer
// decodes the H.264 stream and its sharp picture matches a direct capture.
func TestViewerDecodesVideoEndToEnd(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_LINK_BENCH") == "" {
		t.Skip("takes a minute: set MEWLA_SCREEN_LINK_BENCH=1")
	}
	chrome := os.Getenv("MEWLA_CHROME")
	if chrome == "" {
		chrome, _ = exec.LookPath("google-chrome")
	}
	if chrome == "" {
		t.Skip("no Chrome")
	}
	if !ffmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	for _, mode := range []struct {
		name  string
		video videoFactory
	}{{"video", newFFmpegEncoder}} {
		session := newMarkerSession(1280, 800, 8*time.Second, 3*time.Second)
		r := runViewerE2E(t, context.Background(), session, mode.video, chrome, "")
		line := reportE2E(t, mode.name, mode.video != nil, r)
		t.Log(line)
		if strings.Contains(line, "INVALID") {
			t.Error(line)
		}
		if r.psnr < 25 {
			t.Errorf("PSNR %.1f", r.psnr)
		}
	}
}
