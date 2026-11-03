package screen

import "encoding/binary"

// avccToAnnexB turns one encoded frame, as VideoToolbox hands it over (each
// NAL unit behind a big-endian length of nalLength bytes), into an Annex B
// stream (each behind a start code). A key frame is led by its parameter sets
// so a decoder can start on it. A malformed frame yields nil.
func avccToAnnexB(frame []byte, nalLength int, paramSets [][]byte, key bool) []byte {
	if nalLength < 1 || nalLength > 4 {
		return nil
	}
	start := []byte{0, 0, 0, 1}
	out := make([]byte, 0, len(frame)+64)
	if key {
		for _, set := range paramSets {
			out = append(append(out, start...), set...)
		}
	}
	for i := 0; i < len(frame); {
		if i+nalLength > len(frame) {
			return nil
		}
		var buf [4]byte
		copy(buf[4-nalLength:], frame[i:i+nalLength])
		size := int(binary.BigEndian.Uint32(buf[:]))
		i += nalLength
		if size <= 0 || i+size > len(frame) {
			return nil
		}
		out = append(append(out, start...), frame[i:i+size]...)
		i += size
	}
	return out
}
