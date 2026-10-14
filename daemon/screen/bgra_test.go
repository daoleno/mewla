package screen

import (
	"image"
	"testing"
)

func TestCopyBGRASkipsRowPadding(t *testing.T) {
	// 2x2 BGRA with 4 bytes of padding per row.
	src := []byte{
		1, 2, 3, 0, 4, 5, 6, 0, 0xee, 0xee, 0xee, 0xee,
		7, 8, 9, 0, 10, 11, 12, 0, 0xee, 0xee, 0xee, 0xee,
	}
	dst := image.NewRGBA(image.Rect(0, 0, 2, 2))
	copyBGRA(dst, src, 12)
	want := []byte{3, 2, 1, 255, 6, 5, 4, 255, 9, 8, 7, 255, 12, 11, 10, 255}
	if string(dst.Pix) != string(want) {
		t.Fatalf("pix = %v", dst.Pix)
	}
}
