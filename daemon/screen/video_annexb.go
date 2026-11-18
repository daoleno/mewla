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

// hasNAL reports whether an Annex B stream holds a NAL unit of this type.
func hasNAL(au []byte, kind byte) bool {
	for i := 0; i+4 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && (au[i+2] == 1 || (au[i+2] == 0 && au[i+3] == 1)) {
			at := i + 3
			if au[i+2] == 0 {
				at = i + 4
			}
			if at < len(au) && au[at]&0x1f == kind {
				return true
			}
			i = at - 1
		}
	}
	return false
}
