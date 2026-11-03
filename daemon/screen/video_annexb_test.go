package screen

import (
	"bytes"
	"testing"
)

func TestAVCCBecomesAnnexBWithParameterSetsOnKeyFrames(t *testing.T) {
	sps, pps := []byte{0x67, 1, 2, 3}, []byte{0x68, 9}
	idr, slice := []byte{0x65, 7, 7, 7, 7}, []byte{0x41, 5, 5}
	frame := append([]byte{0, 0, 0, 5}, idr...)
	frame = append(frame, append([]byte{0, 0, 0, 3}, slice...)...)
	got := avccToAnnexB(frame, 4, [][]byte{sps, pps}, true)
	want := bytes.Join([][]byte{{0, 0, 0, 1}, sps, {0, 0, 0, 1}, pps, {0, 0, 0, 1}, idr, {0, 0, 0, 1}, slice}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x\nwant % x", got, want)
	}
	if got := avccToAnnexB(frame, 4, [][]byte{sps, pps}, false); bytes.Contains(got, sps) {
		t.Fatal("a delta frame carried parameter sets")
	}
	if avccToAnnexB(frame[:7], 4, nil, false) != nil {
		t.Fatal("a truncated frame was converted")
	}
	two := append([]byte{0, 3}, slice...)
	if got := avccToAnnexB(two, 2, nil, false); !bytes.Equal(got, append([]byte{0, 0, 0, 1}, slice...)) {
		t.Fatalf("2-byte lengths: % x", got)
	}
}
