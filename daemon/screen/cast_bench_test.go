package screen

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daoleno/mewla/daemon/screencast"
	"github.com/gorilla/websocket"
)

// TestCastToChrome measures pushed video end to end on this computer: this
// computer's encoder (VideoToolbox on a Mac, ffmpeg elsewhere), WebRTC over
// loopback, and a headless Chrome of its own (its own profile, no window)
// playing it in a <video> as the viewer does. A picture that turns black or
// white on each key the page sends gives input → picture shown; motion that
// changes every pixel gives frames shown a second. Then, where Screen is
// ready, the real capture of the screen is played for a few seconds (nothing
// is typed or clicked on it). MEWLA_SCREEN_CAST_BENCH=1 and -v; MEWLA_CHROME
// names the browser.
func TestCastToChrome(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_CAST_BENCH") == "" {
		t.Skip("starts a headless Chrome: set MEWLA_SCREEN_CAST_BENCH=1")
	}
	chrome := benchChrome(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var phase struct {
		sync.Mutex
		source screencast.Source
		input  func([]byte) error
	}
	results := make(chan map[string]any, 1)
	var stats struct {
		sync.Mutex
		encoder string
		encode  []float64
		max     float64
	}
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(castBenchPage))
	})
	mux.HandleFunc("/result", func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		_ = json.NewDecoder(r.Body).Decode(&v)
		select {
		case results <- v:
		default:
		}
	})
	mux.HandleFunc("/signal", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var writeMu sync.Mutex
		phase.Lock()
		source, input := phase.source, phase.input
		phase.Unlock()
		session, err := screencast.Start(r.Context(), screencast.Config{
			Source:     source,
			Input:      input,
			NewEncoder: CastEncoder(),
			Signal: func(v any) {
				writeMu.Lock()
				defer writeMu.Unlock()
				_ = conn.WriteJSON(v)
			},
			Stats: func(st screencast.Stats) {
				stats.Lock()
				defer stats.Unlock()
				stats.encoder = st.Encoder
				if st.Updates > 0 {
					stats.encode = append(stats.encode, st.EncodeMs)
					stats.max = max(stats.max, st.EncodeMax)
				}
			},
			LoopbackOnly: true,
		})
		if err != nil {
			t.Errorf("start: %v", err)
			return
		}
		defer session.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := session.HandleSignal(data); err != nil {
				t.Logf("signal: %v", err)
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	run := func(name string, source screencast.Source, input func([]byte) error, query string) map[string]any {
		phase.Lock()
		phase.source, phase.input = source, input
		phase.Unlock()
		stats.Lock()
		stats.encode, stats.max = nil, 0
		stats.Unlock()
		profile := t.TempDir()
		cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--no-first-run", "--no-default-browser-check",
			"--user-data-dir="+profile, "--autoplay-policy=no-user-gesture-required", "--window-size=1400,900",
			server.URL+"/?"+query)
		if err := cmd.Start(); err != nil {
			t.Fatalf("%s: %v", chrome, err)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}()
		select {
		case v := <-results:
			if e, ok := v["error"]; ok {
				t.Fatalf("%s: the page says %v", name, e)
			}
			return v
		case <-time.After(90 * time.Second):
			t.Fatalf("%s: no result from the page", name)
		}
		return nil
	}

	flip := newFlipSource(1280, 800)
	got := run("synthetic", flip, flip.input, "trials=30")
	stats.Lock()
	encoder, encode, worst := stats.encoder, append([]float64(nil), stats.encode...), stats.max
	stats.Unlock()
	sort.Float64s(encode)
	var table strings.Builder
	fmt.Fprintf(&table, "\n%s/%s, encoder %s, headless Chrome over loopback\n", runtime.GOOS, runtime.GOARCH, encoder)
	fmt.Fprintf(&table, "key → picture shown (1280x800, whole picture flips): p50 %v p95 %v ms (%v trials)\n", got["p50"], got["p95"], got["n"])
	fmt.Fprintf(&table, "full-screen motion: %v frames shown a second (source 60)\n", got["fps"])
	if len(encode) > 0 {
		fmt.Fprintf(&table, "encode per picture: median of per-second means %.1f ms, worst %.1f ms\n", encode[len(encode)/2], worst)
	}

	backend := newBackend()
	if capability := backend.Capability(ctx); capability.Supported && capability.Ready() {
		session, err := backend.Start(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cast := &Cast{session: session, release: func() {}}
		defer cast.Close()
		real := run("screen", cast, nil, "trials=0&watch=5")
		fmt.Fprintf(&table, "this screen's own capture: %vx%v shown, %v frames shown in 5 s (as the screen changed), first picture after %v ms\n",
			real["width"], real["height"], real["frames"], real["first"])
	} else {
		fmt.Fprintf(&table, "this screen's own capture: not ready here (%+v)\n", capability)
	}
	t.Log(table.String())
}

func benchChrome(t *testing.T) string {
	if path := os.Getenv("MEWLA_CHROME"); path != "" {
		return path
	}
	candidates := map[string][]string{
		"darwin": {"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		"windows": {`C:\Program Files\Google\Chrome\Application\chrome.exe`, `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, `C:\Program Files\Microsoft\Edge\Application\msedge.exe`},
	}[runtime.GOOS]
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no Chrome: set MEWLA_CHROME")
	return ""
}

// flipSource is a whole picture that turns black or white on each key down
// (KeyEvent), and on "m" changes every pixel at 60 a second until "s".
type flipSource struct {
	w, h    int
	mu      sync.Mutex
	white   bool
	moving  bool
	seq     uint64
	changed chan struct{}
}

func newFlipSource(w, h int) *flipSource {
	return &flipSource{w: w, h: h, seq: 1, changed: make(chan struct{}, 1)}
}

func (f *flipSource) input(msg []byte) error {
	if len(msg) != 8 || msg[0] != 4 || msg[1] == 0 {
		return nil
	}
	f.mu.Lock()
	switch binary.BigEndian.Uint32(msg[4:]) {
	case 'm':
		f.moving = true
	case 's':
		f.moving = false
	default:
		f.white = !f.white
	}
	f.seq++
	f.mu.Unlock()
	select {
	case f.changed <- struct{}{}:
	default:
	}
	return nil
}

func (f *flipSource) Next(ctx context.Context, after uint64) (screencast.Picture, error) {
	for {
		f.mu.Lock()
		if f.moving && f.seq <= after {
			f.mu.Unlock()
			select {
			case <-time.After(16 * time.Millisecond):
			case <-ctx.Done():
				return screencast.Picture{}, ctx.Err()
			}
			f.mu.Lock()
			f.seq++
		}
		if f.seq > after {
			seq, white, moving := f.seq, f.white, f.moving
			f.mu.Unlock()
			return f.picture(seq, white, moving), nil
		}
		f.mu.Unlock()
		select {
		case <-f.changed:
		case <-ctx.Done():
			return screencast.Picture{}, ctx.Err()
		}
	}
}

func (f *flipSource) picture(seq uint64, white, moving bool) screencast.Picture {
	pix := make([]byte, f.w*f.h*4)
	for y := range f.h {
		row := pix[y*f.w*4 : (y+1)*f.w*4]
		for x := range f.w {
			v := byte(0)
			if white {
				v = 255
			}
			if moving {
				v = byte(x + y + int(seq)*9)
			}
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = v, v^byte(seq), v, 255
		}
	}
	return screencast.Picture{Pix: pix, Width: f.w, Height: f.h, Stride: f.w * 4, RGB: true, Seq: seq}
}

// castBenchPage is a viewer that measures itself and posts what it saw.
const castBenchPage = `<!doctype html><html><body style="margin:0;background:#333">
<video id="v" muted autoplay playsinline></video><canvas id="c" width="8" height="8"></canvas>
<script>
const q = new URLSearchParams(location.search);
const trials = +q.get("trials"), watchFor = +(q.get("watch") || 0);
const post = (v) => fetch("/result", { method: "POST", body: JSON.stringify(v) });
const v = document.getElementById("v"), c = document.getElementById("c").getContext("2d", { willReadFrequently: true });
const pc = new RTCPeerConnection({ bundlePolicy: "max-bundle" });
const ws = new WebSocket("ws://" + location.host + "/signal");
let input = null, seq = 0, frames = 0, start = performance.now(), first = 0, onFrame = null;
const pending = [];
pc.ondatachannel = (e) => { if (e.channel.label === "input") input = e.channel; };
pc.ontrack = (e) => {
  try { e.receiver.jitterBufferTarget = 0; } catch {}
  v.srcObject = e.streams[0] || new MediaStream([e.track]);
  v.play().catch(() => {});
};
pc.onicecandidate = (e) => ws.send(JSON.stringify({ type: "ice", candidate: e.candidate ? e.candidate.toJSON() : null }));
ws.onmessage = async (m) => {
  const msg = JSON.parse(m.data);
  if (msg.type === "rtc" && msg.sdp_type === "offer") {
    await pc.setRemoteDescription({ type: "offer", sdp: msg.sdp });
    for (const cand of pending.splice(0)) await pc.addIceCandidate(cand).catch(() => {});
    await pc.setLocalDescription(await pc.createAnswer());
    ws.send(JSON.stringify({ type: "rtc", sdp_type: "answer", sdp: pc.localDescription.sdp }));
  } else if (msg.type === "ice" && msg.candidate) {
    if (!pc.remoteDescription) pending.push(msg.candidate); else pc.addIceCandidate(msg.candidate).catch(() => {});
  }
};
const shown = () => {
  frames++;
  if (!first) first = performance.now() - start;
  if (onFrame) onFrame();
  v.requestVideoFrameCallback(shown);
};
v.requestVideoFrameCallback(shown);
const key = (k) => {
  const out = new Uint8Array(12), d = new DataView(out.buffer);
  d.setUint32(0, ++seq); out[4] = 4; out[5] = 1; d.setUint32(8, k.charCodeAt(0));
  input.send(out);
  d.setUint32(0, ++seq); out[5] = 0; input.send(out);
};
const level = () => { c.drawImage(v, v.videoWidth / 2 - 4, v.videoHeight / 2 - 4, 8, 8, 0, 0, 8, 8); return c.getImageData(4, 4, 1, 1).data[0]; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function main() {
  const until = performance.now() + 20000;
  while ((!input || input.readyState !== "open" || !frames) && performance.now() < until) await sleep(50);
  if (!frames) return post({ error: "no picture: " + pc.connectionState });
  if (watchFor) {
    frames = 0; await sleep(watchFor * 1000);
    return post({ width: v.videoWidth, height: v.videoHeight, frames, first: Math.round(first) });
  }
  await sleep(500);
  const times = [];
  for (let i = 0; i < trials; i++) {
    const before = level() > 128;
    const t0 = performance.now();
    const flipped = new Promise((resolve) => { onFrame = () => { if ((level() > 128) !== before) { onFrame = null; resolve(performance.now() - t0); } }; });
    key("f");
    const ms = await Promise.race([flipped, sleep(2000).then(() => -1)]);
    onFrame = null;
    if (ms >= 0) times.push(ms);
    await sleep(150 + Math.random() * 100);
  }
  times.sort((a, b) => a - b);
  key("m"); await sleep(1000); frames = 0; await sleep(5000); const fps = frames / 5; key("s");
  post({ n: times.length, p50: Math.round(times[Math.floor(times.length / 2)]), p95: Math.round(times[Math.floor(times.length * 0.95)]), fps: Math.round(fps * 10) / 10 });
}
main().catch((e) => post({ error: String(e) }));
</script></body></html>`
