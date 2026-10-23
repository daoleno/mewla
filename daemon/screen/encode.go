package screen

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
)

const (
	tileSize = 64
	// tightMaxData is the largest length Tight's 3-byte compact form holds.
	tightMaxData = 1<<22 - 1
	tightFill    = 0x80
	tightJPEG    = 0x90
)

// jpegQuality maps the viewer's quality level 0-9 to libjpeg-style quality.
var jpegQuality = [10]int{15, 25, 35, 45, 55, 65, 75, 82, 88, 94}

// dirtyRects compares two same-sized frames in tiles and merges changed
// tiles into rectangles: runs along each tile row, then runs with the same
// columns stacked across rows.
func dirtyRects(old, cur *image.RGBA) []image.Rectangle {
	b := cur.Rect
	var rows [][]image.Rectangle
	for y := b.Min.Y; y < b.Max.Y; y += tileSize {
		var row []image.Rectangle
		for x := b.Min.X; x < b.Max.X; x += tileSize {
			tile := image.Rect(x, y, x+tileSize, y+tileSize).Intersect(b)
			if tileEqual(old, cur, tile) {
				continue
			}
			if n := len(row); n > 0 && row[n-1].Max.X == tile.Min.X {
				row[n-1].Max.X = tile.Max.X
			} else {
				row = append(row, tile)
			}
		}
		rows = append(rows, row)
	}
	return mergeTileRows(rows)
}

// mergeTileRows joins runs of tiles that share their columns across rows.
func mergeTileRows(rows [][]image.Rectangle) []image.Rectangle {
	var out []image.Rectangle
	open := map[[2]int]int{} // [minX,maxX] -> index in out, for the previous row
	for _, row := range rows {
		next := map[[2]int]int{}
		for _, r := range row {
			key := [2]int{r.Min.X, r.Max.X}
			if i, ok := open[key]; ok && out[i].Max.Y == r.Min.Y {
				out[i].Max.Y = r.Max.Y
				next[key] = i
				continue
			}
			out = append(out, r)
			next[key] = len(out) - 1
		}
		open = next
	}
	return out
}

func tileEqual(a, b *image.RGBA, r image.Rectangle) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		ia, ib := a.PixOffset(r.Min.X, y), b.PixOffset(r.Min.X, y)
		n := r.Dx() * 4
		if !bytes.Equal(a.Pix[ia:ia+n], b.Pix[ib:ib+n]) {
			return false
		}
	}
	return true
}

func copyRect(dst, src *image.RGBA, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i, j := dst.PixOffset(r.Min.X, y), src.PixOffset(r.Min.X, y)
		n := r.Dx() * 4
		copy(dst.Pix[i:i+n], src.Pix[j:j+n])
	}
}

// encodeUpdate builds one FramebufferUpdate for rects of img.
func encodeUpdate(img *image.RGBA, rects []image.Rectangle, format pixelFormat, tight bool, quality int) ([]byte, error) {
	var body []byte
	count := 0
	for _, r := range rects {
		for _, part := range splitForTight(img, r, tight, quality) {
			encoded, err := encodeRect(img, part, format, tight, quality)
			if err != nil {
				return nil, err
			}
			body = append(body, encoded...)
			count++
		}
	}
	msg := make([]byte, 4, 4+len(body))
	msg[0] = msgFramebufferUpdate
	binary.BigEndian.PutUint16(msg[2:], uint16(count))
	return append(msg, body...), nil
}

// splitForTight keeps every rect within 65535 RFB rects of 16-bit size; very
// large rects are halved until a JPEG fits Tight's length field.
func splitForTight(img *image.RGBA, r image.Rectangle, tight bool, quality int) []image.Rectangle {
	if !tight || r.Dx()*r.Dy() <= 2048*2048 {
		return []image.Rectangle{r}
	}
	mid := r.Min.Y + r.Dy()/2
	top := image.Rect(r.Min.X, r.Min.Y, r.Max.X, mid)
	bottom := image.Rect(r.Min.X, mid, r.Max.X, r.Max.Y)
	return append(splitForTight(img, top, tight, quality), splitForTight(img, bottom, tight, quality)...)
}

func rectHeader(r image.Rectangle, encoding int32) []byte {
	h := make([]byte, 12)
	binary.BigEndian.PutUint16(h[0:], uint16(r.Min.X))
	binary.BigEndian.PutUint16(h[2:], uint16(r.Min.Y))
	binary.BigEndian.PutUint16(h[4:], uint16(r.Dx()))
	binary.BigEndian.PutUint16(h[6:], uint16(r.Dy()))
	binary.BigEndian.PutUint32(h[8:], uint32(encoding))
	return h
}

func encodeRect(img *image.RGBA, r image.Rectangle, format pixelFormat, tight bool, quality int) ([]byte, error) {
	if !tight {
		return encodeRaw(img, r, format), nil
	}
	out := rectHeader(r, encTight)
	if c, ok := solidColor(img, r); ok {
		px := format.pixel(c[0], c[1], c[2])
		// TPIXEL: the pixel's three colour bytes in its own byte order.
		var tp [4]byte
		if format.bigEndian {
			binary.BigEndian.PutUint32(tp[:], px)
			return append(append(out, tightFill), tp[1:]...), nil
		}
		binary.LittleEndian.PutUint32(tp[:], px)
		return append(append(out, tightFill), tp[:3]...), nil
	}
	var buf bytes.Buffer
	sub := img.SubImage(r).(*image.RGBA)
	if err := jpeg.Encode(&buf, sub, &jpeg.Options{Quality: jpegQuality[min(max(quality, 0), 9)]}); err != nil {
		return nil, err
	}
	if buf.Len() > tightMaxData {
		return encodeRaw(img, r, format), nil
	}
	out = append(out, tightJPEG)
	out = appendCompactLength(out, buf.Len())
	return append(out, buf.Bytes()...), nil
}

// appendCompactLength is Tight's 1-3 byte little-endian 7-bit length.
func appendCompactLength(b []byte, n int) []byte {
	b = append(b, byte(n&0x7f))
	if n <= 0x7f {
		return b
	}
	b[len(b)-1] |= 0x80
	b = append(b, byte(n>>7&0x7f))
	if n <= 0x3fff {
		return b
	}
	b[len(b)-1] |= 0x80
	return append(b, byte(n>>14))
}

func solidColor(img *image.RGBA, r image.Rectangle) ([3]uint8, bool) {
	i := img.PixOffset(r.Min.X, r.Min.Y)
	first := img.Pix[i : i+4]
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.PixOffset(r.Min.X, y)
		for x := 0; x < r.Dx(); x++ {
			p := img.Pix[row+x*4 : row+x*4+3]
			if p[0] != first[0] || p[1] != first[1] || p[2] != first[2] {
				return [3]uint8{}, false
			}
		}
	}
	return [3]uint8{first[0], first[1], first[2]}, true
}

func encodeRaw(img *image.RGBA, r image.Rectangle, format pixelFormat) []byte {
	out := rectHeader(r, encRaw)
	out = append(out, make([]byte, r.Dx()*r.Dy()*4)...)
	px := out[12:]
	k := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.PixOffset(r.Min.X, y)
		for x := 0; x < r.Dx(); x++ {
			p := img.Pix[row+x*4:]
			v := format.pixel(p[0], p[1], p[2])
			if format.bigEndian {
				binary.BigEndian.PutUint32(px[k:], v)
			} else {
				binary.LittleEndian.PutUint32(px[k:], v)
			}
			k += 4
		}
	}
	return out
}

// tileSet marks which tiles of a frame hold something worth sending again.
type tileSet struct {
	bounds     image.Rectangle
	cols, rows int
	set        []bool
	count      int
}

func newTileSet(bounds image.Rectangle) tileSet {
	cols, rows := (bounds.Dx()+tileSize-1)/tileSize, (bounds.Dy()+tileSize-1)/tileSize
	return tileSet{bounds: bounds, cols: cols, rows: rows, set: make([]bool, cols*rows)}
}

func (t *tileSet) any() bool { return t.count > 0 }

// each calls fn for every tile that r touches; whole reports that r covers
// the tile entirely.
func (t *tileSet) each(r image.Rectangle, fn func(i int, whole bool)) {
	r = r.Intersect(t.bounds)
	if r.Empty() {
		return
	}
	for ty := (r.Min.Y - t.bounds.Min.Y) / tileSize; ty <= (r.Max.Y-1-t.bounds.Min.Y)/tileSize; ty++ {
		for tx := (r.Min.X - t.bounds.Min.X) / tileSize; tx <= (r.Max.X-1-t.bounds.Min.X)/tileSize; tx++ {
			tile := image.Rect(t.bounds.Min.X+tx*tileSize, t.bounds.Min.Y+ty*tileSize,
				t.bounds.Min.X+(tx+1)*tileSize, t.bounds.Min.Y+(ty+1)*tileSize).Intersect(t.bounds)
			fn(ty*t.cols+tx, r.Intersect(tile) == tile)
		}
	}
}

func (t *tileSet) add(r image.Rectangle) {
	t.each(r, func(i int, _ bool) {
		if !t.set[i] {
			t.set[i] = true
			t.count++
		}
	})
}

// remove clears the tiles r covers entirely; one it covers in part still
// holds something stale.
func (t *tileSet) remove(r image.Rectangle) {
	t.each(r, func(i int, whole bool) {
		if whole && t.set[i] {
			t.set[i] = false
			t.count--
		}
	})
}

func (t *tileSet) rects() []image.Rectangle {
	rows := make([][]image.Rectangle, 0, t.rows)
	for ty := 0; ty < t.rows; ty++ {
		var row []image.Rectangle
		for tx := 0; tx < t.cols; tx++ {
			if !t.set[ty*t.cols+tx] {
				continue
			}
			tile := image.Rect(t.bounds.Min.X+tx*tileSize, t.bounds.Min.Y+ty*tileSize,
				t.bounds.Min.X+(tx+1)*tileSize, t.bounds.Min.Y+(ty+1)*tileSize).Intersect(t.bounds)
			if n := len(row); n > 0 && row[n-1].Max.X == tile.Min.X {
				row[n-1].Max.X = tile.Max.X
			} else {
				row = append(row, tile)
			}
		}
		rows = append(rows, row)
	}
	return mergeTileRows(rows)
}
