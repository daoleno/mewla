package screen

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

// RFB encodings and pseudo-encodings this server speaks (RFC 6143 and the
// community registry, as noVNC 1.7 sends them).
const (
	encRaw                = 0
	encTight              = 7
	pseudoQualityLevel0   = -32
	pseudoQualityLevel9   = -23
	pseudoDesktopSize     = -223
	pseudoFence           = -312
	pseudoContinuousUpdts = -313
)

// Client and server message types.
const (
	msgSetPixelFormat    = 0
	msgSetEncodings      = 2
	msgFBUpdateRequest   = 3
	msgKeyEvent          = 4
	msgPointerEvent      = 5
	msgClientCutText     = 6
	msgEnableContinuous  = 150
	msgFence             = 248
	msgFramebufferUpdate = 0
	msgEndOfContinuous   = 150
	fenceRequest         = 1 << 31
	maxClipboard         = 1 << 20
	defaultQualityLevel  = 6
)

// streamAdaptive is on unless MEWLA_SCREEN_ADAPTIVE=off, a way to see a link
// as it was before adaptation, to compare.
var streamAdaptive = os.Getenv("MEWLA_SCREEN_ADAPTIVE") != "off"

// pixelFormat is RFC 6143 §7.4. Only 32-bit true colour is supported, which
// is what every current viewer asks for.
type pixelFormat struct {
	bpp, depth                      uint8
	bigEndian, trueColor            bool
	redMax, greenMax, blueMax       uint16
	redShift, greenShift, blueShift uint8
}

// serverPixelFormat is what ServerInit announces: little-endian BGRX.
var serverPixelFormat = pixelFormat{
	bpp: 32, depth: 24, trueColor: true,
	redMax: 255, greenMax: 255, blueMax: 255,
	redShift: 16, greenShift: 8, blueShift: 0,
}

func (p pixelFormat) bytes() []byte {
	b := make([]byte, 16)
	b[0], b[1] = p.bpp, p.depth
	if p.bigEndian {
		b[2] = 1
	}
	if p.trueColor {
		b[3] = 1
	}
	binary.BigEndian.PutUint16(b[4:], p.redMax)
	binary.BigEndian.PutUint16(b[6:], p.greenMax)
	binary.BigEndian.PutUint16(b[8:], p.blueMax)
	b[10], b[11], b[12] = p.redShift, p.greenShift, p.blueShift
	return b
}

func parsePixelFormat(b []byte) pixelFormat {
	return pixelFormat{
		bpp: b[0], depth: b[1], bigEndian: b[2] != 0, trueColor: b[3] != 0,
		redMax: binary.BigEndian.Uint16(b[4:]), greenMax: binary.BigEndian.Uint16(b[6:]), blueMax: binary.BigEndian.Uint16(b[8:]),
		redShift: b[10], greenShift: b[11], blueShift: b[12],
	}
}

// pixel packs one RGB colour in this format.
func (p pixelFormat) pixel(r, g, b uint8) uint32 {
	scale := func(v uint8, max uint16) uint32 { return uint32(v) * uint32(max) / 255 }
	return scale(r, p.redMax)<<p.redShift | scale(g, p.greenMax)<<p.greenShift | scale(b, p.blueMax)<<p.blueShift
}

// rfbState is what the viewer has asked for; the reader writes it and the
// writer reads it.
type rfbState struct {
	mu          sync.Mutex
	format      pixelFormat
	tight       bool
	quality     int
	desktopSize bool
	fence       bool
	continuous  bool
	// requested is an outstanding FramebufferUpdateRequest; full means it
	// asked for the whole screen.
	requested bool
	full      bool
	sent      uint64
	acked     uint64
	wake      chan struct{}
	// sendLog remembers each fenced update until it is acknowledged, for the
	// age of its acknowledgement; ctl turns those ages into the quality and
	// the in-flight limit.
	sendLog map[uint64]sentRecord
	ctl     *streamController
	metrics *streamMetrics
}

type sentRecord struct {
	at     time.Time
	bytes  int
	refine bool
}

// ackedTo records the fence answer for update n; callers hold state.mu.
func (s *rfbState) ackedTo(n uint64, now time.Time) {
	rec, ok := s.sendLog[n]
	for seq := range s.sendLog {
		if seq <= n {
			delete(s.sendLog, seq)
		}
	}
	if !ok {
		return
	}
	age := now.Sub(rec.at)
	s.metrics.acked(age)
	// A sharpening of a still screen is a one-off, not the link's pace.
	if !rec.refine {
		s.ctl.observe(now, age, rec.bytes)
	}
}

func (s *rfbState) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// serveRFB runs the server half of an RFB session from ClientInit on. Frames
// come from session; pointer and key events go back to it.
func serveRFB(ctx context.Context, conn net.Conn, session Session, name string, obs *Observer) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	var clientInit [1]byte
	if _, err := io.ReadFull(conn, clientInit[:]); err != nil {
		return err
	}
	first, err := session.Frame(ctx, 0)
	if err != nil {
		return err
	}
	// The writer owns `shown`, what the viewer has; capture owns `latest`.
	frames := &frameStore{wake: make(chan struct{}, 1)}
	frames.put(first)
	bounds := first.Image.Bounds()
	if err := writeServerInit(conn, bounds.Dx(), bounds.Dy(), name); err != nil {
		return err
	}

	state := &rfbState{
		format: serverPixelFormat, quality: defaultQualityLevel, wake: make(chan struct{}, 1),
		sendLog: map[uint64]sentRecord{}, ctl: newStreamController(), metrics: &streamMetrics{},
	}
	state.ctl.fixed = !streamAdaptive
	defer func() { obs.done(state.metrics.summary()) }()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if stats, ok := state.metrics.take(); ok {
					obs.stats(stats)
				}
			}
		}
	}()
	var writeMu sync.Mutex
	write := func(b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := conn.Write(b)
		return err
	}

	go func() {
		seq := first.Seq
		for {
			frame, err := session.Frame(ctx, seq)
			if err != nil {
				cancel(err)
				return
			}
			seq = frame.Seq
			frames.put(frame)
		}
	}()
	go func() { cancel(readClient(ctx, conn, session, state, write)) }()
	err = writeUpdates(ctx, state, frames, write)
	cancel(err)
	return context.Cause(ctx)
}

func writeServerInit(w io.Writer, width, height int, name string) error {
	b := make([]byte, 4, 24+len(name))
	binary.BigEndian.PutUint16(b[0:], uint16(width))
	binary.BigEndian.PutUint16(b[2:], uint16(height))
	b = append(b, serverPixelFormat.bytes()...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(name)))
	b = append(b, name...)
	_, err := w.Write(b)
	return err
}

// frameStore keeps a copy of the newest captured frame.
type frameStore struct {
	mu     sync.Mutex
	latest *image.RGBA
	seq    uint64
	// at is when the system produced the latest frame.
	at   time.Time
	wake chan struct{}
}

func (f *frameStore) put(frame Frame) {
	f.mu.Lock()
	src := frame.Image
	if f.latest == nil || f.latest.Rect != src.Rect {
		f.latest = image.NewRGBA(src.Rect)
	}
	copyRGBA(f.latest, src)
	f.seq = frame.Seq
	f.at = frame.At
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func copyRGBA(dst, src *image.RGBA) {
	if dst.Stride == src.Stride {
		copy(dst.Pix, src.Pix)
		return
	}
	row := src.Rect.Dx() * 4
	for y := 0; y < src.Rect.Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+row], src.Pix[y*src.Stride:y*src.Stride+row])
	}
}

func readClient(ctx context.Context, r io.Reader, session Session, state *rfbState, write func([]byte) error) error {
	var head [1]byte
	buf := make([]byte, 64)
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return err
		}
		switch head[0] {
		case msgSetPixelFormat:
			if _, err := io.ReadFull(r, buf[:19]); err != nil {
				return err
			}
			format := parsePixelFormat(buf[3:19])
			if format.bpp != 32 || !format.trueColor {
				return fmt.Errorf("viewer asked for an unsupported pixel format (%d bpp)", format.bpp)
			}
			state.mu.Lock()
			state.format = format
			state.full = true
			state.mu.Unlock()
		case msgSetEncodings:
			if _, err := io.ReadFull(r, buf[:3]); err != nil {
				return err
			}
			count := int(binary.BigEndian.Uint16(buf[1:3]))
			raw := make([]byte, 4*count)
			if _, err := io.ReadFull(r, raw); err != nil {
				return err
			}
			announce := applyEncodings(state, raw)
			if announce {
				// Telling the viewer we support continuous updates; it then
				// enables them with EnableContinuousUpdates.
				if err := write([]byte{msgEndOfContinuous}); err != nil {
					return err
				}
			}
		case msgFBUpdateRequest:
			if _, err := io.ReadFull(r, buf[:9]); err != nil {
				return err
			}
			state.mu.Lock()
			state.requested = true
			if buf[0] == 0 {
				state.full = true
			}
			state.mu.Unlock()
			state.signal()
		case msgKeyEvent:
			if _, err := io.ReadFull(r, buf[:7]); err != nil {
				return err
			}
			if err := session.Key(binary.BigEndian.Uint32(buf[3:7]), buf[0] != 0); err != nil {
				return err
			}
		case msgPointerEvent:
			if _, err := io.ReadFull(r, buf[:5]); err != nil {
				return err
			}
			x, y := int(binary.BigEndian.Uint16(buf[1:3])), int(binary.BigEndian.Uint16(buf[3:5]))
			if err := session.Pointer(x, y, buf[0]); err != nil {
				return err
			}
		case msgClientCutText:
			if _, err := io.ReadFull(r, buf[:7]); err != nil {
				return err
			}
			length := int32(binary.BigEndian.Uint32(buf[3:7]))
			if length < 0 {
				length = -length
			}
			if length > maxClipboard {
				return errors.New("viewer clipboard text is too large")
			}
			if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
				return err
			}
		case msgEnableContinuous:
			if _, err := io.ReadFull(r, buf[:9]); err != nil {
				return err
			}
			state.mu.Lock()
			state.continuous = buf[0] != 0
			state.full = true
			state.mu.Unlock()
			state.signal()
		case msgFence:
			if _, err := io.ReadFull(r, buf[:8]); err != nil {
				return err
			}
			length := int(buf[7])
			payload := make([]byte, length)
			if _, err := io.ReadFull(r, payload); err != nil {
				return err
			}
			flags := binary.BigEndian.Uint32(buf[3:7])
			if flags&fenceRequest != 0 {
				// Answer a viewer's own fence request with the same payload.
				reply := append([]byte{msgFence, 0, 0, 0}, binary.BigEndian.AppendUint32(nil, flags&3)...)
				reply = append(append(reply, byte(length)), payload...)
				if err := write(reply); err != nil {
					return err
				}
				continue
			}
			if n, err := strconv.ParseUint(string(payload), 10, 64); err == nil {
				state.mu.Lock()
				if n > state.acked && n <= state.sent {
					state.acked = n
					state.ackedTo(n, time.Now())
				}
				state.mu.Unlock()
				state.signal()
			}
		default:
			return fmt.Errorf("viewer sent unsupported RFB message %d", head[0])
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// applyEncodings records the viewer's encodings and reports whether to
// announce continuous updates. They need fences too: fences are how the
// server knows the viewer has drawn a frame before it sends more.
func applyEncodings(state *rfbState, raw []byte) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.tight, state.desktopSize, state.fence = false, false, false
	continuous := false
	for i := 0; i+4 <= len(raw); i += 4 {
		switch enc := int32(binary.BigEndian.Uint32(raw[i:])); {
		case enc == encTight:
			state.tight = true
		case enc == pseudoDesktopSize:
			state.desktopSize = true
		case enc == pseudoFence:
			state.fence = true
		case enc == pseudoContinuousUpdts:
			continuous = true
		case enc >= pseudoQualityLevel0 && enc <= pseudoQualityLevel9:
			state.quality = int(enc - pseudoQualityLevel0)
		}
	}
	return continuous && state.fence
}

// writeUpdates sends a FramebufferUpdate whenever the viewer may take one
// and the screen changed. With continuous updates it pushes without waiting
// for requests, keeping a few frames unacknowledged by fences, so a slow
// link drops intermediate frames instead of queueing them: the newest screen
// is always what goes next. How many, and at what JPEG quality, follows how
// late the viewer's answers are (streamController). What went out at a lower
// quality while the screen moved is sent again at the viewer's once it is
// still.
func writeUpdates(ctx context.Context, state *rfbState, frames *frameStore, write func([]byte) error) error {
	var shown *image.RGBA
	var shownSeq uint64
	var degraded tileSet
	for {
		state.mu.Lock()
		may := state.requested || state.continuous
		if state.fence && state.continuous && state.sent-state.acked >= uint64(state.ctl.limit()) {
			may = false
		}
		full := state.full
		format, tight, viewerQuality, desktopSize, fence := state.format, state.tight, state.quality, state.desktopSize, state.fence
		quality := state.ctl.quality(viewerQuality)
		state.mu.Unlock()

		frames.mu.Lock()
		changed := frames.seq != shownSeq
		captured, captureSeq := frames.at, frames.seq
		var rects []image.Rectangle
		resized, refine := false, false
		var stillIn time.Duration
		if may && (changed || full) {
			latest := frames.latest
			if shown == nil || shown.Rect != latest.Rect {
				resized = shown != nil
				shown = image.NewRGBA(latest.Rect)
				degraded = newTileSet(latest.Rect)
				full = true
			}
			if full {
				rects = []image.Rectangle{latest.Rect}
			} else {
				rects = dirtyRects(shown, latest)
			}
			for _, r := range rects {
				copyRect(shown, latest, r)
			}
			shownSeq = frames.seq
		} else if may && !changed && degraded.any() {
			// Nothing changed: shown is the screen, and a still screen is
			// worth sending sharp.
			if stillIn = stillAfter - time.Since(captured); stillIn <= 0 || captured.IsZero() {
				rects, refine, stillIn = degraded.rects(), true, 0
			}
		}
		frames.mu.Unlock()

		if may && (len(rects) > 0 || resized) {
			sendQuality := quality
			if refine {
				sendQuality = viewerQuality
			}
			var msg []byte
			if resized && desktopSize {
				msg = appendDesktopSize(msg, shown.Rect.Dx(), shown.Rect.Dy())
			}
			began := time.Now()
			update, err := encodeUpdate(shown, rects, format, tight, sendQuality)
			if err != nil {
				return err
			}
			encoded := time.Since(began)
			msg = append(msg, update...)
			dirty := 0
			for _, r := range rects {
				dirty += r.Dx() * r.Dy()
				if tight && sendQuality < viewerQuality {
					degraded.add(r)
				} else {
					degraded.remove(r)
				}
			}
			state.mu.Lock()
			state.requested = false
			state.full = false
			state.sent++
			seq := state.sent
			flight := state.ctl.limit()
			if fence {
				state.sendLog[seq] = sentRecord{at: time.Now(), bytes: len(msg), refine: refine}
				for old := range state.sendLog {
					if old+64 < seq {
						delete(state.sendLog, old)
					}
				}
			}
			state.mu.Unlock()
			var captureAge time.Duration
			if !captured.IsZero() && !refine {
				captureAge = max(0, began.Sub(captured))
			}
			state.metrics.sent(sentUpdate{
				Seq: captureSeq, Bytes: len(msg), Dirty: dirty, Total: shown.Rect.Dx() * shown.Rect.Dy(),
				CaptureAge: captureAge, Encode: encoded, Quality: sendQuality, ViewerLevel: viewerQuality,
				InFlight: flight, Refine: refine,
			})
			if fence {
				payload := strconv.FormatUint(seq, 10)
				msg = append(msg, msgFence, 0, 0, 0)
				msg = binary.BigEndian.AppendUint32(msg, fenceRequest)
				msg = append(append(msg, byte(len(payload))), payload...)
			}
			if err := write(msg); err != nil {
				return err
			}
			continue
		}
		if may && full {
			state.mu.Lock()
			state.full = false
			state.mu.Unlock()
		}
		var still <-chan time.Time
		var timer *time.Timer
		if stillIn > 0 {
			timer = time.NewTimer(stillIn)
			still = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return ctx.Err()
		case <-state.wake:
		case <-frames.wake:
		case <-still:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func appendDesktopSize(msg []byte, width, height int) []byte {
	msg = append(msg, msgFramebufferUpdate, 0)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	return append(msg, rectHeader(image.Rect(0, 0, width, height), pseudoDesktopSize)...)
}
