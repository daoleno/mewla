package screen

import (
	"encoding/binary"
	"errors"
	"image"
	"os"
	"sync"
	"time"
)

// Video is the second way a viewer's picture travels. Still screens, text and
// small changes go as Tight JPEG tiles, which are sharp and cost nothing when
// nothing moves. A screen that moves a lot (a page scrolling, a video playing,
// a window dragged) costs 100-300 KB a frame that way, more than a phone link
// carries; the same motion is an H.264 stream of a few KB a frame. When the
// viewer offers H.264 (RFB encoding 50, "Open H.264", which noVNC speaks over
// WebCodecs) and this computer has an encoder, motion is sent as video and
// the sharp tiles come back once the screen is still. The viewer's input and
// fences are unchanged; an agent's own screen use never goes through here.

// encH264 is the RFB encoding number of "Open H.264".
const encH264 = 50

// H.264 rect flags, as noVNC reads them.
const (
	h264ResetContext     = 1
	h264ResetAllContexts = 2
)

const (
	// videoEnterPct is the share of the screen two updates in a row must
	// redraw before motion goes as video; videoLeavePct the share below which
	// a screen counts as quiet again.
	videoEnterPct = 25
	videoLeavePct = 5
	// videoQuiet is how long a screen must stay below videoLeavePct before
	// the stream goes back to tiles.
	videoQuiet = 400 * time.Millisecond
	// videoFPS is what the encoder is told to expect.
	videoFPS = 30
	// Bitrate bounds, in kbit/s, and where a session starts.
	videoMinKbps   = 500
	videoMaxKbps   = 8000
	videoStartKbps = 3000
)

// videoEncoder compresses full-screen frames into one H.264 stream (Annex B,
// no B-frames, every key frame led by its SPS and PPS), so that each call's
// output is a picture a viewer can show the moment it has the stream's first
// key frame.
type videoEncoder interface {
	// Encode compresses img. A true key forces a key frame; the second
	// result says whether the output is one.
	Encode(img *image.RGBA, key bool) ([]byte, bool, error)
	// SetBitrate asks for about kbps kbit/s from now on.
	SetBitrate(kbps int)
	Close()
}

// errVideoDropped means the encoder chose to skip a frame, which leaves its
// stream as it was; the caller sends tiles for that update instead.
var errVideoDropped = errors.New("the encoder dropped the frame")

// videoFactory makes an encoder for a screen of this size.
type videoFactory func(width, height, kbps int) (videoEncoder, error)

// defaultVideo is this build's encoder, or nil when it has none. Platforms set
// it in an init function; MEWLA_SCREEN_VIDEO=off removes it, to see a link as
// it was before video, to compare.
var defaultVideo videoFactory

// videoByDefault is whether a viewer that offers H.264 gets video without
// MEWLA_SCREEN_VIDEO=on. It stays false until the encoder of every platform
// that has one has run on a real machine.
const videoByDefault = false

func videoEnabled() bool {
	switch os.Getenv("MEWLA_SCREEN_VIDEO") {
	case "off":
		return false
	case "on":
		return true
	}
	return videoByDefault
}

// videoPath decides, update by update, whether motion goes as video, and
// owns the encoder. It belongs to the one goroutine that writes updates.
type videoPath struct {
	factory videoFactory
	enc     videoEncoder
	failed  bool
	// active is true while updates are frames of the stream.
	active bool
	// needKey makes the next frame a key frame that resets the viewer's
	// decoder: the first of a session, and after a resize.
	needKey bool
	big     int
	quiet   time.Time
	// rate is written by the acknowledgement reader and read by the writer.
	rate    *videoRate
	applied int
	width   int
	height  int
}

func newVideoPath(factory videoFactory) *videoPath {
	if factory == nil || !videoEnabled() {
		return nil
	}
	return &videoPath{factory: factory, needKey: true, rate: newVideoRate(), applied: videoStartKbps}
}

// choose reports whether this update goes as video. dirty and total are
// pixels; resized means the screen changed size.
func (v *videoPath) choose(now time.Time, dirty, total int, resized bool) bool {
	if v == nil || v.failed || total == 0 {
		return false
	}
	if resized {
		v.stop()
	}
	if !v.active {
		if dirty*100 >= total*videoEnterPct {
			v.big++
		} else {
			v.big = 0
		}
		if v.big < 2 {
			return false
		}
		v.active, v.quiet = true, time.Time{}
		return true
	}
	if dirty*100 < total*videoLeavePct {
		if v.quiet.IsZero() {
			v.quiet = now
		}
		if now.Sub(v.quiet) >= videoQuiet {
			v.settle()
			return false
		}
	} else {
		v.quiet = time.Time{}
	}
	return true
}

// settle returns to tiles: the screen is still, or about to be sharpened.
func (v *videoPath) settle() {
	if v == nil {
		return
	}
	v.active, v.big, v.quiet = false, 0, time.Time{}
}

// stop drops the encoder; the next video frame makes a new one and starts the
// viewer's decoder afresh.
func (v *videoPath) stop() {
	if v.enc != nil {
		v.enc.Close()
		v.enc = nil
	}
	v.settle()
	v.needKey = true
}

// encode makes the rect payload for one full-screen frame. On a failure the
// path turns itself off for the session and the caller sends tiles instead.
func (v *videoPath) encode(img *image.RGBA) (payload []byte, key bool, ok bool) {
	if v.enc == nil || v.width != img.Rect.Dx() || v.height != img.Rect.Dy() {
		if v.enc != nil {
			v.enc.Close()
			v.enc = nil
		}
		kbps := v.rate.current()
		enc, err := v.factory(img.Rect.Dx(), img.Rect.Dy(), kbps)
		if err != nil {
			v.failed = true
			v.active = false
			return nil, false, false
		}
		v.enc, v.width, v.height, v.applied = enc, img.Rect.Dx(), img.Rect.Dy(), kbps
		v.needKey = true
	}
	if kbps := v.rate.current(); kbps != v.applied {
		v.enc.SetBitrate(kbps)
		v.applied = kbps
	}
	data, isKey, err := v.enc.Encode(img, v.needKey)
	if errors.Is(err, errVideoDropped) {
		return nil, false, false
	}
	if err != nil || len(data) == 0 {
		v.failed = true
		v.active = false
		v.enc.Close()
		v.enc = nil
		return nil, false, false
	}
	flags := uint32(0)
	if v.needKey {
		flags = h264ResetContext
		v.needKey = false
	}
	out := make([]byte, 0, 8+len(data))
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	out = binary.BigEndian.AppendUint32(out, flags)
	out = append(out, data...)
	return out, isKey, true
}

// observe takes one acknowledged video update; the writer applies the rate
// it arrives at before its next frame.
func (v *videoPath) observe(now time.Time, age time.Duration, bytes int) {
	if v != nil {
		v.rate.observe(now, age, bytes)
	}
}

func (v *videoPath) close() {
	if v != nil && v.enc != nil {
		v.enc.Close()
		v.enc = nil
	}
}

// videoRate turns acknowledgements into a bitrate. Video bytes queue behind
// each other on a slow link, so an update's acknowledgement age is its own
// transit plus the queue ahead of it: an age well above the link's calm
// round trip means the stream is faster than the link, and the rate it was
// really delivered at (bytes acknowledged in a stretch) is the most it can
// carry. While acknowledgements stay prompt the rate creeps back up.
type videoRate struct {
	mu   sync.Mutex
	kbps int
	// base is the smallest smoothed age seen: the link's round trip.
	base  float64
	age   float64
	seen  bool
	start time.Time
	bytes int
	calm  time.Time
	cool  time.Time
}

func newVideoRate() *videoRate { return &videoRate{kbps: videoStartKbps} }

func (r *videoRate) current() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kbps
}

const (
	// videoLate is how far above the round trip an age may be before the
	// stream is faster than the link.
	videoLate = 120.0
	// videoWindow is how long delivery is measured.
	videoWindow = 750 * time.Millisecond
	// videoRise is how long acknowledgements must stay prompt before the
	// rate rises by a step.
	videoRise = 2 * time.Second
)

func (r *videoRate) observe(now time.Time, age time.Duration, bytes int) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := ms(age)
	if !r.seen {
		r.seen, r.age, r.base, r.start = true, a, a, now
	} else {
		r.age += 0.3 * (a - r.age)
		if r.age < r.base {
			r.base = r.age
		} else {
			// The round trip drifts; forget slowly so a once-lucky
			// reading does not make every age look late.
			r.base += 0.01 * (r.age - r.base)
		}
	}
	r.bytes += bytes
	before := r.kbps
	if elapsed := now.Sub(r.start); elapsed >= videoWindow {
		delivered := int(float64(r.bytes) * 8 / 1000 / elapsed.Seconds())
		if r.age-r.base > videoLate && now.After(r.cool) {
			// Late: aim under what actually got through.
			r.kbps = max(videoMinKbps, min(r.kbps, delivered*8/10))
			r.calm, r.cool = time.Time{}, now.Add(videoWindow)
		}
		r.start, r.bytes = now, 0
	}
	if r.age-r.base <= videoLate {
		if r.calm.IsZero() {
			r.calm = now
		}
		if now.Sub(r.calm) >= videoRise {
			r.kbps = min(videoMaxKbps, r.kbps*12/10)
			r.calm = now
		}
	}
	return r.kbps, r.kbps != before
}
