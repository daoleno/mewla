package screen

import (
	"context"
	"image"
	"testing"
)

// scrollFrame renders the fixture's scrolling page at a size, for timing the
// stages a moving screen goes through.
func scrollFrame(b *testing.B, w, h int) (*image.RGBA, *image.RGBA) {
	b.Helper()
	fake := NewFakeBackend(w, h)
	fake.Scroll = 6
	session, err := fake.Start(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	first, err := session.Frame(context.Background(), 0)
	if err != nil {
		b.Fatal(err)
	}
	a := image.NewRGBA(first.Image.Rect)
	copyRGBA(a, first.Image)
	fake.ScrollTick()
	next, err := session.Frame(context.Background(), first.Seq)
	if err != nil {
		b.Fatal(err)
	}
	c := image.NewRGBA(next.Image.Rect)
	copyRGBA(c, next.Image)
	return a, c
}

func benchEncode(b *testing.B, w, h, quality int) {
	_, cur := scrollFrame(b, w, h)
	rects := []image.Rectangle{cur.Rect}
	b.SetBytes(int64(w * h * 4))
	b.ResetTimer()
	var size int
	for i := 0; i < b.N; i++ {
		out, err := encodeUpdate(cur, rects, novncFormat, true, quality)
		if err != nil {
			b.Fatal(err)
		}
		size = len(out)
	}
	b.ReportMetric(float64(size)/1024, "KB/frame")
}

func BenchmarkEncodeFullFrame1512x982Q6(b *testing.B) { benchEncode(b, 1512, 982, 6) }
func BenchmarkEncodeFullFrame1512x982Q1(b *testing.B) { benchEncode(b, 1512, 982, 1) }
func BenchmarkEncodeFullFrame1280x800Q6(b *testing.B) { benchEncode(b, 1280, 800, 6) }

func BenchmarkDirtyRectsScrolledFrame(b *testing.B) {
	old, cur := scrollFrame(b, 1512, 982)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dirtyRects(old, cur)
	}
}

func BenchmarkCopyFrame1512x982(b *testing.B) {
	_, cur := scrollFrame(b, 1512, 982)
	dst := image.NewRGBA(cur.Rect)
	b.SetBytes(int64(len(cur.Pix)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copyRGBA(dst, cur)
	}
}
