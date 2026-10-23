package screen

import (
	"sort"
	"sync"
	"time"
)

// A viewer's link is measured per update, never per pixel: every number here
// is a duration, a byte count or a share of the screen, so a session's
// telemetry carries nothing from the screen itself and stays a fixed size.

// histEdges are the upper bounds, in milliseconds, of a histogram's buckets.
var histEdges = [...]float64{
	0.5, 1, 2, 3, 4, 6, 8, 10, 12, 15, 20, 25, 30, 40, 50, 65, 80, 100,
	125, 150, 200, 250, 300, 400, 500, 650, 800, 1000, 1500, 2000, 3000, 5000,
}

// histogram counts millisecond samples in fixed buckets, so a session's
// median and 95th percentile cost memory that doesn't grow with its length.
type histogram struct {
	counts [len(histEdges) + 1]int
	n      int
	max    float64
}

func (h *histogram) add(ms float64) {
	i := sort.SearchFloat64s(histEdges[:], ms)
	h.counts[i]++
	h.n++
	h.max = max(h.max, ms)
}

// quantile is the upper bound of the bucket holding the q-th sample, or the
// largest sample for the last bucket; 0 when empty.
func (h *histogram) quantile(q float64) float64 {
	if h.n == 0 {
		return 0
	}
	want := int(q*float64(h.n)) + 1
	if want > h.n {
		want = h.n
	}
	seen := 0
	for i, c := range h.counts {
		seen += c
		if seen >= want {
			if i == len(histEdges) {
				return h.max
			}
			return min(histEdges[i], h.max)
		}
	}
	return h.max
}

// StreamStats is what one second of a viewer's stream cost, stage by stage:
// the screen's age when it was encoded, the encode, and the wait for the
// viewer's acknowledgement. Times are medians in milliseconds.
type StreamStats struct {
	Updates int `json:"updates"`
	Bytes   int `json:"bytes"`
	// DirtyPct is the share of the screen the updates redrew.
	DirtyPct int `json:"dirty_pct"`
	// CaptureSeq is the newest capture sent.
	CaptureSeq uint64  `json:"capture_seq"`
	CaptureMs  float64 `json:"capture_ms"`
	EncodeMs   float64 `json:"encode_ms"`
	EncodeMax  float64 `json:"encode_max_ms"`
	// AckMs is from sending an update to the viewer's fence answer, which
	// noVNC gives after it has drawn the update.
	AckMs  float64 `json:"ack_ms"`
	AckMax float64 `json:"ack_max_ms"`
	// Quality is the JPEG level in use and InFlight the updates allowed
	// unacknowledged.
	Quality  int `json:"quality"`
	InFlight int `json:"in_flight"`
}

// StreamSummary is one finished session, for one log line.
type StreamSummary struct {
	Updates   int
	Refines   int
	Bytes     int64
	EncodeP50 float64
	EncodeP95 float64
	AckP50    float64
	AckP95    float64
	// LowestQuality is the lowest JPEG level adaptation reached, against
	// the viewer's own.
	LowestQuality int
	ViewerQuality int
	MaxInFlight   int
}

type streamWindow struct {
	updates, bytes int
	dirty, total   int64
	capture        histogram
	encode         histogram
	ack            histogram
}

// streamMetrics collects a session's stages. All methods are safe to call
// from the encoder and the reader at once.
type streamMetrics struct {
	mu      sync.Mutex
	win     streamWindow
	all     streamWindow
	refines int
	seq     uint64
	quality int
	flight  int
	lowest  int
	viewer  int
	maxFlt  int
}

type sentUpdate struct {
	Seq         uint64
	Bytes       int
	Dirty       int
	Total       int
	CaptureAge  time.Duration
	Encode      time.Duration
	Quality     int
	ViewerLevel int
	InFlight    int
	Refine      bool
}

func (m *streamMetrics) sent(u sentUpdate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range []*streamWindow{&m.win, &m.all} {
		w.updates++
		w.bytes += u.Bytes
		w.dirty += int64(u.Dirty)
		w.total += int64(u.Total)
		w.encode.add(ms(u.Encode))
		if u.CaptureAge > 0 {
			w.capture.add(ms(u.CaptureAge))
		}
	}
	if u.Refine {
		m.refines++
	}
	m.seq = u.Seq
	m.quality, m.flight, m.viewer = u.Quality, u.InFlight, u.ViewerLevel
	if m.lowest == 0 || u.Quality < m.lowest {
		m.lowest = u.Quality
	}
	m.maxFlt = max(m.maxFlt, u.InFlight)
}

func (m *streamMetrics) acked(age time.Duration) {
	m.mu.Lock()
	m.win.ack.add(ms(age))
	m.all.ack.add(ms(age))
	m.mu.Unlock()
}

// take returns the stats since the last call and starts a new window.
func (m *streamMetrics) take() (StreamStats, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.win
	m.win = streamWindow{}
	if w.updates == 0 && w.ack.n == 0 {
		return StreamStats{}, false
	}
	stats := StreamStats{
		Updates: w.updates, Bytes: w.bytes, CaptureSeq: m.seq,
		CaptureMs: w.capture.quantile(0.5),
		EncodeMs:  w.encode.quantile(0.5), EncodeMax: w.encode.max,
		AckMs: w.ack.quantile(0.5), AckMax: w.ack.max,
		Quality: m.quality, InFlight: m.flight,
	}
	if w.total > 0 {
		stats.DirtyPct = int(w.dirty * 100 / w.total)
	}
	return stats, true
}

func (m *streamMetrics) summary() StreamSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := &m.all
	return StreamSummary{
		Updates: a.updates, Refines: m.refines, Bytes: int64(a.bytes),
		EncodeP50: a.encode.quantile(0.5), EncodeP95: a.encode.quantile(0.95),
		AckP50: a.ack.quantile(0.5), AckP95: a.ack.quantile(0.95),
		LowestQuality: m.lowest, ViewerQuality: m.viewer, MaxInFlight: m.maxFlt,
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Adaptive streaming. The viewer acknowledges each update with a fence once
// noVNC has drawn it, so the age of an acknowledgement says how far behind
// the viewer is. Two things can make it high, and they need opposite fixes:
// a long round trip (send more before the first answer: allow more updates
// in flight) or bytes that don't fit the link (send fewer: lower the JPEG
// quality while the screen moves). Nothing in-band tells them apart, since
// every probe queues behind the bytes it measures, so the controller asks:
// when updates arrive late it allows one more in flight and keeps it only if
// the viewer then gets more updates per second. If it doesn't, bytes are the
// limit, and quality falls instead.
const (
	// qualityFloor is the lowest level adaptation goes to (JPEG 25), and
	// maxDrop the most levels it takes off the highest.
	qualityFloor = 1
	maxDrop      = 9 - qualityFloor
	// inFlightBase and inFlightMax bound the unacknowledged updates.
	inFlightBase = 2
	inFlightMax  = 4
	// behindAck is an acknowledgement age that feels late.
	behindAck = 100 * time.Millisecond
	// bigUpdate is the average size above which bytes are the suspect.
	bigUpdate = 24 << 10
	// stepGrowth is roughly how much larger one quality level is than the
	// one below it.
	stepGrowth = 1.2
	dropEvery  = 300 * time.Millisecond
	// recoverAfter is how long the link must stay calm before one level of
	// quality comes back.
	recoverAfter = 1500 * time.Millisecond
	// probeWindow is how long a rate is measured before and after a wider
	// window, probeGain the gain that keeps it, and probeRest how long a
	// failed probe waits before another.
	probeWindow = time.Second
	probeGain   = 1.15
	probeRest   = 5 * time.Second
	// stillAfter is how long the screen must stay unchanged before what
	// was sent at lower quality is sent again at the viewer's.
	stillAfter = 250 * time.Millisecond
)

// streamController turns acknowledgements into a quality drop and an
// in-flight limit. It is deterministic in the times it is given.
type streamController struct {
	// fixed turns adaptation off, for comparing a link with and without it.
	fixed bool
	// drop is how many levels below the viewer's the quality is held.
	drop     int
	inFlight int
	ack      float64 // smoothed ack age, ms
	bytes    float64 // smoothed update size
	seen     bool
	// bound means a wider window did not help: bytes are the limit.
	bound                bool
	droppedAt, calmSince time.Time
	// The current measuring window, and a probe of one more in flight.
	winStart     time.Time
	winAcks      int
	probing      bool
	probeBase    float64
	noProbeUntil time.Time
}

func newStreamController() *streamController { return &streamController{inFlight: inFlightBase} }

// observe takes one acknowledged update: its age and its size.
func (c *streamController) observe(now time.Time, age time.Duration, bytes int) {
	if !c.seen {
		c.seen = true
		c.ack, c.bytes = ms(age), float64(bytes)
		c.winStart = now
	} else {
		c.ack += 0.3 * (ms(age) - c.ack)
		c.bytes += 0.3 * (float64(bytes) - c.bytes)
	}
	if c.fixed {
		return
	}
	c.winAcks++
	if elapsed := now.Sub(c.winStart); elapsed >= probeWindow {
		c.endWindow(now, float64(c.winAcks)/elapsed.Seconds())
		c.winStart, c.winAcks = now, 0
	}
	ack := time.Duration(c.ack * float64(time.Millisecond))
	if ack > behindAck {
		c.calmSince = time.Time{}
		// Bytes are the limit once more window has not helped, and only
		// if they are many.
		if c.bytes > bigUpdate && c.bound && now.Sub(c.droppedAt) >= dropEvery && c.drop < maxDrop {
			c.drop++
			c.droppedAt = now
		}
		return
	}
	// A sharper update is about stepGrowth times larger and so takes about
	// that much longer.
	if c.drop > 0 && float64(ack)*stepGrowth > float64(behindAck) {
		c.calmSince = time.Time{}
		return
	}
	if c.calmSince.IsZero() {
		c.calmSince = now
	}
	if now.Sub(c.calmSince) >= recoverAfter {
		c.calmSince = now
		switch {
		case c.drop > 0:
			c.drop--
		case c.inFlight > inFlightBase:
			c.inFlight--
		default:
			c.bound = false
		}
	}
}

// endWindow closes a measuring window of fps updates a second: it judges a
// probe in progress, or starts one while updates are late.
func (c *streamController) endWindow(now time.Time, fps float64) {
	if c.probing {
		c.probing = false
		if fps < c.probeBase*probeGain {
			c.inFlight--
			c.bound = true
			c.noProbeUntil = now.Add(probeRest)
		}
		return
	}
	late := c.ack > ms(behindAck)
	if late && c.inFlight < inFlightMax && !now.Before(c.noProbeUntil) && c.winAcks >= 2 {
		c.probing, c.probeBase = true, fps
		c.inFlight++
		return
	}
}

// quality is the JPEG level to use when the viewer asked for `viewer`.
func (c *streamController) quality(viewer int) int {
	if viewer <= qualityFloor {
		return viewer
	}
	return max(qualityFloor, viewer-c.drop)
}

// limit is how many updates may be unacknowledged.
func (c *streamController) limit() int { return c.inFlight }
