package screen

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

// What the still-frame probe needs to judge a recorded update stream,
// separate from the Mac-only capture so it can be tested anywhere.

type stillRect struct {
	r     image.Rectangle
	video bool
	jpeg  []byte
	fill  []byte
}

type stillUpdate struct {
	at    time.Time
	rects []stillRect
}

func (u stillUpdate) dirty() int {
	n := 0
	for _, r := range u.rects {
		n += r.r.Dx() * r.r.Dy()
	}
	return n
}

func (u stillUpdate) hasVideo() bool {
	for _, r := range u.rects {
		if r.video {
			return true
		}
	}
	return false
}

// stillAnalysis locates, in a recorded stream, the scroll and the sharp
// update that follows it.
type stillAnalysis struct {
	motionFirst, motionLast int // the run of moving updates, inclusive
	stillIdx                int // the first update after the quiet gap
	gap                     time.Duration
	motionVideo             int // moving updates that carried a video rect
}

const (
	motionDirtyShare = 0.2                    // a moving update dirties this much of the screen, or carries video
	motionMaxStep    = 150 * time.Millisecond // moving updates follow each other at least this closely
	motionMinRun     = 8                      // and there are at least this many in a row
	stillMinGap      = 200 * time.Millisecond
)

// analyzeStill finds the longest run of moving updates (large dirty area or a
// video rect, each within motionMaxStep of the last, at least motionMinRun of
// them) and the update that ends the quiet gap after it. The connect burst,
// the pointer's small updates and the sharpening update itself are not runs:
// they are too short, too small, or too far apart.
func analyzeStill(updates []stillUpdate, screenArea int) (stillAnalysis, bool) {
	best := stillAnalysis{motionFirst: -1}
	bestLen := 0
	moving := func(u stillUpdate) bool {
		return u.hasVideo() || float64(u.dirty()) >= motionDirtyShare*float64(screenArea)
	}
	for i := 0; i < len(updates); {
		if !moving(updates[i]) {
			i++
			continue
		}
		j := i
		for j+1 < len(updates) && moving(updates[j+1]) && updates[j+1].at.Sub(updates[j].at) <= motionMaxStep {
			j++
		}
		if j-i+1 > bestLen {
			bestLen, best.motionFirst, best.motionLast = j-i+1, i, j
		}
		i = j + 1
	}
	if bestLen < motionMinRun || best.motionLast+1 >= len(updates) {
		return best, false
	}
	best.stillIdx = best.motionLast + 1
	best.gap = updates[best.stillIdx].at.Sub(updates[best.motionLast].at)
	if best.gap < stillMinGap {
		return best, false
	}
	for _, u := range updates[best.motionFirst : best.motionLast+1] {
		if u.hasVideo() {
			best.motionVideo++
		}
	}
	return best, true
}

// The scroll marker: the fixture page draws, in its top left corner, a green
// square followed by sixteen red (1) or blue (0) squares of the same size,
// the page's scroll position in binary, most significant bit first. It is
// read back from any frame that shows it, so a frame says where the page was.

const markerBits = 16

// readScrollMarker finds the marker in the top rows of img and returns the
// scroll position it shows.
func readScrollMarker(img *image.RGBA) (int, bool) {
	b := img.Bounds()
	green := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return c.G > 150 && c.R < 110 && c.B < 110
	}
	for y := b.Min.Y; y < min(b.Max.Y, b.Min.Y+400); y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !green(x, y) || (x > b.Min.X && green(x-1, y)) || (y > b.Min.Y && green(x, y-1)) {
				continue
			}
			// The top-left corner of a green block. The marker is seventeen
			// squares of saturated colour along this row: its width gives
			// the pitch, whatever JPEG did to the edges.
			s0 := 0
			for x+s0 < b.Max.X && green(x+s0, y) {
				s0++
			}
			if s0 < 6 || y+s0 > b.Max.Y {
				continue
			}
			sat := func(c color.RGBA) bool { return max(c.R, c.G, c.B)-min(c.R, c.G, c.B) > 100 }
			// Through JPEG a boundary pixel can lose its saturation: allow
			// gaps of up to three pixels inside the marker.
			width := 0
			for gap, w := 0, 0; x+w < b.Max.X && gap <= 3; w++ {
				if sat(img.RGBAAt(x+w, y+s0/2)) {
					width, gap = w+1, 0
				} else {
					gap++
				}
			}
			s := (width + (markerBits+1)/2) / (markerBits + 1)
			if s < 6 || x+(markerBits+1)*s > b.Max.X {
				continue
			}
			pos, ok := 0, true
			for i := 0; i < markerBits && ok; i++ {
				c := img.RGBAAt(x+(i+1)*s+s/2, y+s/2)
				switch {
				case c.R > 150 && c.B < 100:
					pos = pos<<1 | 1
				case c.B > 150 && c.R < 100:
					pos <<= 1
				default:
					ok = false
				}
			}
			if ok {
				return pos, true
			}
		}
	}
	return 0, false
}

// drawScrollMarker paints the marker the page draws, with squares of size s
// at (x0, y0).
func drawScrollMarker(img *image.RGBA, x0, y0, s, pos int) {
	fill := func(i int, c color.RGBA) {
		for y := y0; y < y0+s; y++ {
			for x := x0 + i*s; x < x0+(i+1)*s; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	fill(0, color.RGBA{0, 255, 0, 255})
	for i := 0; i < markerBits; i++ {
		if pos>>(markerBits-1-i)&1 == 1 {
			fill(i+1, color.RGBA{255, 0, 0, 255})
		} else {
			fill(i+1, color.RGBA{0, 0, 255, 255})
		}
	}
}

func syntheticStream(start time.Time, build func(add func(ms int, rects ...stillRect))) []stillUpdate {
	var out []stillUpdate
	build(func(ms int, rects ...stillRect) {
		out = append(out, stillUpdate{at: start.Add(time.Duration(ms) * time.Millisecond), rects: rects})
	})
	return out
}

func TestAnalyzeStill(t *testing.T) {
	screen := image.Rect(0, 0, 1000, 600)
	full := stillRect{r: screen, jpeg: []byte{1}}
	vid := stillRect{r: screen, video: true}
	small := stillRect{r: image.Rect(10, 10, 40, 40), jpeg: []byte{1}}
	start := time.Unix(1000, 0)
	area := screen.Dx() * screen.Dy()

	scroll := func(add func(int, ...stillRect), from, n int, r stillRect) int {
		ms := from
		for i := 0; i < n; i++ {
			add(ms, r)
			ms += 33
		}
		return ms - 33
	}

	t.Run("tiles then a sharp update", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			add(0, full)   // connect: the whole screen
			add(260, full) // and its sharpening: a run of two
			last := scroll(add, 3000, 120, full)
			add(last+280, full) // the sharp update
			add(last+2000, small)
		})
		a, ok := analyzeStill(u, area)
		if !ok || a.motionFirst != 2 || a.motionLast != 121 || a.stillIdx != 122 || a.gap != 280*time.Millisecond {
			t.Fatalf("%+v %v", a, ok)
		}
		if a.motionVideo != 0 {
			t.Fatalf("video count %d", a.motionVideo)
		}
	})
	t.Run("video then a sharp update", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			add(0, full)
			last := scroll(add, 3000, 100, vid)
			add(last+270, full)
		})
		a, ok := analyzeStill(u, area)
		if !ok || a.motionFirst != 1 || a.motionLast != 100 || a.stillIdx != 101 || a.motionVideo != 100 {
			t.Fatalf("%+v %v", a, ok)
		}
	})
	t.Run("a pointer's small updates are not motion", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			scroll(add, 0, 300, small)
		})
		if a, ok := analyzeStill(u, area); ok {
			t.Fatalf("%+v", a)
		}
	})
	t.Run("a short burst is not a scroll", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			last := scroll(add, 0, 5, full)
			add(last+300, full)
		})
		if _, ok := analyzeStill(u, area); ok {
			t.Fatal("five updates taken for a scroll")
		}
	})
	t.Run("a scroll that never goes quiet has no still", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			scroll(add, 0, 200, vid)
		})
		if _, ok := analyzeStill(u, area); ok {
			t.Fatal("no still update exists")
		}
	})
	t.Run("the next update comes too soon", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			last := scroll(add, 0, 50, vid)
			add(last+160, small)
		})
		if _, ok := analyzeStill(u, area); ok {
			t.Fatal("a 160 ms gap taken for stillness")
		}
	})
	t.Run("the longest run is the scroll", func(t *testing.T) {
		u := syntheticStream(start, func(add func(int, ...stillRect)) {
			last := scroll(add, 0, 10, full)
			last = scroll(add, last+1000, 90, vid)
			add(last+300, full)
		})
		a, ok := analyzeStill(u, area)
		if !ok || a.motionFirst != 10 || a.motionLast != 99 {
			t.Fatalf("%+v %v", a, ok)
		}
	})
}

func TestScrollMarker(t *testing.T) {
	for _, tc := range []struct{ s, pos, x, y int }{{20, 0, 0, 0}, {20, 3600, 37, 5}, {20, 65535, 100, 90}, {12, 0x5a5a, 0, 41}} {
		img := image.NewRGBA(image.Rect(0, 0, 900, 300))
		for i := range img.Pix {
			img.Pix[i] = 235 // a pale page
		}
		drawScrollMarker(img, tc.x, tc.y, tc.s, tc.pos)
		if got, ok := readScrollMarker(img); !ok || got != tc.pos {
			t.Errorf("%+v read %d %v", tc, got, ok)
		}
		// Through a JPEG, as a viewer receives it.
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
			t.Fatal(err)
		}
		dec, err := jpeg.Decode(&buf)
		if err != nil {
			t.Fatal(err)
		}
		rgba := image.NewRGBA(dec.Bounds())
		for y := 0; y < 300; y++ {
			for x := 0; x < 900; x++ {
				rgba.Set(x, y, dec.At(x, y))
			}
		}
		if got, ok := readScrollMarker(rgba); !ok || got != tc.pos {
			t.Errorf("jpeg %+v read %d %v", tc, got, ok)
		}
	}
	if _, ok := readScrollMarker(image.NewRGBA(image.Rect(0, 0, 300, 300))); ok {
		t.Error("a marker found on a black screen")
	}
}
