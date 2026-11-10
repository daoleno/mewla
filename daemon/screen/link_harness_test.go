package screen

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// linkShape is a simulated network path between the daemon and the viewer.
type linkShape struct {
	name string
	// oneWay is the latency each way; rate is the daemon-to-viewer bandwidth
	// in bytes per second.
	oneWay time.Duration
	rate   float64
	// small drives the pointer across a still screen instead of scrolling a
	// page: updates of a few KB, as when typing, dragging or watching a
	// cursor, which only the round trip holds back.
	small bool
}

// shapedPipe joins a viewer and a server through a link that delays and
// rate-limits bytes. A byte occupies the link for its transmission time; the
// bytes waiting to cross are bounded like a socket's send buffer (they leave it
// as they are transmitted, not as they arrive), so the server is held back
// as it would be by a full buffer.
func shapedPipe(shape linkShape) (viewer, server net.Conn) {
	viewer, a := net.Pipe()
	b, server := net.Pipe()
	const buffer = 96 << 10
	relay := func(from io.Reader, to io.Writer, oneWay time.Duration, rate float64) {
		type packet struct {
			data []byte
			due  time.Time
		}
		queue := make(chan packet, 1024)
		room := make(chan struct{}, buffer/1024)
		for i := 0; i < cap(room); i++ {
			room <- struct{}{}
		}
		go func() {
			defer close(queue)
			var free time.Time
			buf := make([]byte, 1024)
			for {
				<-room
				n, err := from.Read(buf)
				if n > 0 {
					now := time.Now()
					if free.Before(now) {
						free = now
					}
					if rate > 0 {
						free = free.Add(time.Duration(float64(n) / rate * float64(time.Second)))
					}
					queue <- packet{append([]byte(nil), buf[:n]...), free.Add(oneWay)}
				} else {
					room <- struct{}{}
				}
				if err != nil {
					return
				}
			}
		}()
		// A packet leaves the buffer when its last byte is on the wire, and
		// arrives one latency later.
		wire := make(chan packet, 1<<16)
		go func() {
			defer close(wire)
			for p := range queue {
				if d := time.Until(p.due.Add(-oneWay)); d > 0 {
					time.Sleep(d)
				}
				room <- struct{}{}
				wire <- p
			}
		}()
		go func() {
			for p := range wire {
				if d := time.Until(p.due); d > 0 {
					time.Sleep(d)
				}
				if _, err := to.Write(p.data); err != nil {
					return
				}
			}
			if c, ok := to.(io.Closer); ok {
				_ = c.Close()
			}
		}()
	}
	relay(a, b, shape.oneWay, 0)          // viewer to server: latency only
	relay(b, a, shape.oneWay, shape.rate) // server to viewer: latency and bandwidth
	return viewer, server
}

type linkResult struct {
	// encodeMs is the median encode time of the last second, videoPct the
	// share of its updates that were video, cpuPct this process's CPU time
	// over the run as a share of one core.
	encodeMs float64
	videoPct int
	cpuPct   float64
	fps      float64
	kbps     float64
	ackP50   float64
	quality  int
	flight   int
	sharp    bool
}

// measureLink streams a scrolling page over shape for a while and reports
// what a viewer saw: updates drawn per second, how late its answers were, the
// quality and window the daemon settled on, and whether the page ended sharp.
func measureLink(t *testing.T, shape linkShape, adaptive bool, video videoFactory) linkResult {
	t.Helper()
	fake := NewFakeBackend(1280, 800)
	if !shape.small {
		fake.Scroll = 6
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, _ := fake.Start(ctx)
	return measureSession(t, ctx, session, func(step int) {
		if shape.small {
			_ = session.Pointer(100+step*7%900, 100+step*5%600, 0)
		} else {
			fake.ScrollTick()
		}
	}, shape, adaptive, video)
}

// measureSession is measureLink over any session: drive is called 30 times a
// second to make the screen move.
func measureSession(t *testing.T, ctx context.Context, session Session, drive func(step int), shape linkShape, adaptive bool, video videoFactory) linkResult {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		for step := 0; ; step++ {
			time.Sleep(time.Second / 30)
			if ctx.Err() != nil {
				return
			}
			drive(step)
		}
	}()
	conn, server := shapedPipe(shape)
	var mu sync.Mutex
	var last StreamStats
	var summary StreamSummary
	obs := &Observer{
		Stats: func(s StreamStats) {
			mu.Lock()
			last = s
			mu.Unlock()
			if os.Getenv("MEWLA_SCREEN_LINK_TRACE") != "" {
				fmt.Printf("  trace: %d updates (%d video) %d KB ack %.0f ms win %d\n", s.Updates, s.Video, s.Bytes>>10, s.AckMs, s.InFlight)
			}
		},
		Done: func(s StreamSummary) { mu.Lock(); summary = s; mu.Unlock() },
	}
	was := streamAdaptive
	streamAdaptive = adaptive
	defer func() { streamAdaptive = was }()
	done := make(chan struct{})
	go func() { _ = serveRFBVideo(ctx, server, session, "Fixture", obs, video); close(done) }()

	// A light client: it reads what the server sends and answers fences after
	// the time noVNC would take to draw, without decoding the pictures.
	say := func(b []byte) {
		if _, err := conn.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	say([]byte{1})
	head := make([]byte, 24)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint32(head[20:]))); err != nil {
		t.Fatal(err)
	}
	say(append([]byte{msgSetPixelFormat, 0, 0, 0}, novncFormat.bytes()...))
	encodings := []int32{encTight, encRaw, pseudoQualityLevel0 + 6, pseudoDesktopSize, pseudoFence, pseudoContinuousUpdts}
	if video != nil {
		encodings = append([]int32{encH264}, encodings...)
	}
	set := []byte{msgSetEncodings, 0, 0, byte(len(encodings))}
	for _, e := range encodings {
		set = binary.BigEndian.AppendUint32(set, uint32(e))
	}
	say(set)
	skim := &skimmer{conn: conn}
	skim.message() // continuous updates announced
	say([]byte{msgEnableContinuous, 1, 0, 0, 0, 0, 5, 0, 3, 32})

	warm, window := 2500*time.Millisecond, 5*time.Second
	began := time.Now()
	cpuBefore := cpuTime()
	var updates, bytesIn int
	var measuredFrom time.Time
	for time.Since(began) < warm+window {
		kind, fence, n := skim.message()
		if kind == msgFence {
			time.Sleep(8 * time.Millisecond) // noVNC decodes and draws before it answers
			say(append([]byte{msgFence, 0, 0, 0, 0, 0, 0, 0, byte(len(fence))}, fence...))
			continue
		}
		if time.Since(began) >= warm {
			if measuredFrom.IsZero() {
				measuredFrom = time.Now()
			}
			updates++
			bytesIn += n
		}
	}
	seconds := time.Since(measuredFrom).Seconds()
	cpu := (cpuTime() - cpuBefore).Seconds() / time.Since(began).Seconds() * 100
	mu.Lock()
	res := linkResult{
		encodeMs: last.EncodeMs, cpuPct: cpu,
		fps: float64(updates) / seconds, kbps: float64(bytesIn) * 8 / 1000 / seconds,
		ackP50: last.AckMs, quality: last.Quality, flight: last.InFlight,
	}
	if last.Updates > 0 {
		res.videoPct = last.Video * 100 / last.Updates
	}
	mu.Unlock()
	conn.Close()
	<-done
	_ = summary
	return res
}

// skimmer reads server messages and counts their bytes, skipping pixel data.
type skimmer struct {
	conn net.Conn
	// onRect, when set, sees every rectangle of an update: its place, its
	// encoding, a JPEG body or a fill colour.
	onRect func(r image.Rectangle, enc int32, jpegBody []byte, fill []byte)
}

func (k *skimmer) read(n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(k.conn, b); err != nil {
		panic(err)
	}
	return b
}

// message reads one message, returning its type, its fence payload and its size.
func (k *skimmer) message() (kind byte, fence string, size int) {
	kind = k.read(1)[0]
	size = 1
	switch kind {
	case msgFramebufferUpdate:
		count := int(binary.BigEndian.Uint16(k.read(3)[1:]))
		size += 3
		for i := 0; i < count; i++ {
			h := k.read(12)
			size += 12
			rect := image.Rect(int(binary.BigEndian.Uint16(h[0:])), int(binary.BigEndian.Uint16(h[2:])),
				int(binary.BigEndian.Uint16(h[0:]))+int(binary.BigEndian.Uint16(h[4:])), int(binary.BigEndian.Uint16(h[2:]))+int(binary.BigEndian.Uint16(h[6:])))
			if int32(binary.BigEndian.Uint32(h[8:])) == encH264 {
				head := k.read(8)
				n := int(binary.BigEndian.Uint32(head))
				k.read(n)
				size += 8 + n
				if k.onRect != nil {
					k.onRect(rect, encH264, nil, nil)
				}
				continue
			}
			if int32(binary.BigEndian.Uint32(h[8:])) != encTight {
				continue
			}
			ctl := k.read(1)[0]
			size++
			if ctl == tightFill {
				c := k.read(3)
				size += 3
				if k.onRect != nil {
					k.onRect(rect, encTight, nil, c)
				}
				continue
			}
			n := 0
			for shift := 0; shift < 21; shift += 7 {
				b := k.read(1)[0]
				size++
				if shift == 14 {
					n |= int(b) << 14
					break
				}
				n |= int(b&0x7f) << shift
				if b&0x80 == 0 {
					break
				}
			}
			body := k.read(n)
			size += n
			if k.onRect != nil {
				k.onRect(rect, encTight, body, nil)
			}
		}
	case msgFence:
		h := k.read(8)
		fence = string(k.read(int(h[7])))
		size += 8 + len(fence)
	}
	return kind, fence, size
}

// This measures the adaptive policy over simulated links, so that what it
// does is justified by numbers, not argued: on a link bytes or round trips
// hold back, a scrolling page should reach the viewer more often than with a
// fixed quality and two updates in flight, and on a fast link nothing may
// change. The links are synthetic; a real Mac and phone are measured with the
// connection badge. Run with MEWLA_SCREEN_LINK_BENCH=1 and -v to read the table.
func TestAdaptiveStreamingOverSimulatedLinks(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_LINK_BENCH") == "" {
		t.Skip("a minute of real time: set MEWLA_SCREEN_LINK_BENCH=1 to measure")
	}
	shapes := []linkShape{
		{"LAN 100 Mb/s, 2 ms", 2 * time.Millisecond, 100e6 / 8, false},
		{"relay 20 Mb/s, 150 ms RTT", 75 * time.Millisecond, 20e6 / 8, false},
		{"mobile 4 Mb/s, 80 ms RTT", 40 * time.Millisecond, 4e6 / 8, false},
		{"weak 1.5 Mb/s, 200 ms RTT", 100 * time.Millisecond, 1.5e6 / 8, false},
		{"relay 5 Mb/s, 650 ms RTT, small updates", 325 * time.Millisecond, 5e6 / 8, true},
	}
	type pair struct{ fixed, adaptive linkResult }
	results := make([]pair, len(shapes))
	for i, shape := range shapes {
		results[i].fixed = measureLink(t, shape, false, nil)
		results[i].adaptive = measureLink(t, shape, true, nil)
	}
	var table strings.Builder
	fmt.Fprintf(&table, "\n%-28s %-34s %-34s\n", "link", "fixed (q6, 2 in flight)", "adaptive")
	for i, shape := range shapes {
		f, a := results[i].fixed, results[i].adaptive
		fmt.Fprintf(&table, "%-28s %5.1f fps %6.0f kb/s ack %4.0f ms %-5s %5.1f fps %6.0f kb/s ack %4.0f ms q%d win %d\n",
			shape.name, f.fps, f.kbps, f.ackP50, "", a.fps, a.kbps, a.ackP50, a.quality, a.flight)
	}
	t.Log(table.String())
	if a, f := results[0].adaptive, results[0].fixed; a.fps < 0.85*f.fps || a.quality != 6 {
		t.Errorf("a fast link changed: %+v against %+v", a, f)
	}
	for i := 1; i < len(shapes); i++ {
		if a, f := results[i].adaptive, results[i].fixed; a.fps < f.fps {
			t.Errorf("%s: adaptive %.1f fps is below fixed %.1f fps", shapes[i].name, a.fps, f.fps)
		}
	}
}

// Video against tiles over the same links, with x264 standing in for the
// Mac's hardware encoder: what a viewer that offers H.264 gets while a page
// scrolls. Run with MEWLA_SCREEN_LINK_BENCH=1 and -v.
func TestVideoOverSimulatedLinks(t *testing.T) {
	if os.Getenv("MEWLA_SCREEN_LINK_BENCH") == "" {
		t.Skip("a minute of real time: set MEWLA_SCREEN_LINK_BENCH=1 to measure")
	}
	if !ffmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	shapes := []linkShape{
		{"LAN 100 Mb/s, 2 ms", 2 * time.Millisecond, 100e6 / 8, false},
		{"relay 20 Mb/s, 150 ms RTT", 75 * time.Millisecond, 20e6 / 8, false},
		{"mobile 4 Mb/s, 80 ms RTT", 40 * time.Millisecond, 4e6 / 8, false},
		{"weak 1.5 Mb/s, 200 ms RTT", 100 * time.Millisecond, 1.5e6 / 8, false},
		{"relay 5 Mb/s, 650 ms RTT", 325 * time.Millisecond, 5e6 / 8, false},
	}
	var table strings.Builder
	fmt.Fprintf(&table, "\n%-28s %-34s %-34s\n", "link (page scrolling)", "tiles (adaptive)", "video")
	for _, shape := range shapes {
		tiles := measureLink(t, shape, true, nil)
		video := measureLink(t, shape, true, newFFmpegEncoder)
		fmt.Fprintf(&table, "%-28s %5.1f fps %6.0f kb/s ack %4.0f ms q%d   %5.1f fps %6.0f kb/s ack %4.0f ms win %d\n",
			shape.name, tiles.fps, tiles.kbps, tiles.ackP50, tiles.quality, video.fps, video.kbps, video.ackP50, video.flight)
	}
	t.Log(table.String())
}
