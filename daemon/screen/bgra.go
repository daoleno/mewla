package screen

import "image"

// copyBGRA copies a BGRA pixel buffer whose rows are bytesPerRow apart
// (often padded past width*4) into dst, an RGBA image of the same size.
func copyBGRA(dst *image.RGBA, src []byte, bytesPerRow int) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := 0; y < h; y++ {
		in := src[y*bytesPerRow : y*bytesPerRow+w*4]
		out := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
		for i := 0; i < len(in); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = in[i+2], in[i+1], in[i], 255
		}
	}
}
