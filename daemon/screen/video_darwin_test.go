//go:build darwin && !screenfake

package screen

import (
	"image"
	"os"
	"sort"
	"testing"
	"time"
)

// Runs the real hardware encoder on a moving picture and reports what it
// costs: set MEWLA_SCREEN_VT_BENCH=1 and -v. Without the variable it checks
// only that frames come out as decodable Annex B.
func TestVideoToolboxEncodesScreenFrames(t *testing.T) {
	const w, h = 1512, 982
	enc, err := newVTEncoder(w, h, 3000)
	if err != nil {
		t.Skipf("no hardware encoder here: %v", err)
	}
	defer enc.Close()
	frames := 90
	if os.Getenv("MEWLA_SCREEN_VT_BENCH") != "" {
		frames = 600
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	paint := func(step int) {
		// A page of bars scrolling up, with a gradient band, like the fake backend.
		for y := 0; y < h; y++ {
			row := img.Pix[y*img.Stride : y*img.Stride+w*4]
			line := (y + step*6) % 64
			for x := 0; x < w; x++ {
				v := byte(250)
				if line < 12 && (x/37+(y+step*6)/64)%3 != 0 {
					v = 30
				}
				row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = v, v, byte(int(v)*(x%255)/255), 255
			}
		}
	}
	var sizes []int
	var took []time.Duration
	keys := 0
	for i := 0; i < frames; i++ {
		paint(i)
		began := time.Now()
		data, key, err := enc.Encode(img, i == 0)
		took = append(took, time.Since(began))
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if (i == 0) != key && i == 0 {
			t.Fatalf("the first frame is not a key frame")
		}
		if key {
			keys++
			if !hasNAL(data, 7) || !hasNAL(data, 8) || !hasNAL(data, 5) {
				t.Fatalf("key frame %d lacks SPS, PPS or IDR", i)
			}
		} else if !hasNAL(data, 1) {
			t.Fatalf("delta frame %d has no slice", i)
		}
		sizes = append(sizes, len(data))
		if i == 30 {
			enc.SetBitrate(1500)
		}
	}
	sort.Slice(took, func(a, b int) bool { return took[a] < took[b] })
	total := 0
	for _, n := range sizes {
		total += n
	}
	t.Logf("%d frames, %d key, %d KB, encode+copy p50 %v p95 %v", frames, keys, total>>10, took[len(took)/2], took[len(took)*95/100])
}
