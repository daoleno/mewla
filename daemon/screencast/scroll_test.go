package screencast

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// A pixel scroll (message 250) goes to Scroll, whole and signed, and never to
// Input as an RFB message; the messages around it still reach Input in order.
func TestPixelScrollGoesToScrollNotInput(t *testing.T) {
	var inputs [][]byte
	var scrolls [][4]int
	s := &Session{cfg: Config{
		Input:  func(msg []byte) error { inputs = append(inputs, append([]byte(nil), msg...)); return nil },
		Scroll: func(x, y, dx, dy int) error { scrolls = append(scrolls, [4]int{x, y, dx, dy}); return nil },
	}}
	scroll := make([]byte, 10)
	scroll[0] = 250
	binary.BigEndian.PutUint16(scroll[2:], 640)
	binary.BigEndian.PutUint16(scroll[4:], 400)
	dx, dy := int16(-3), int16(57)
	binary.BigEndian.PutUint16(scroll[6:], uint16(dx))
	binary.BigEndian.PutUint16(scroll[8:], uint16(dy))
	data := append([]byte{0, 0, 0, 1}, []byte{5, 0, 2, 128, 1, 144}...)
	data = append(data, scroll...)
	data = append(data, 4, 1, 0, 0, 0, 0, 0, 0x61)
	s.reliable(data)
	if len(scrolls) != 1 || scrolls[0] != [4]int{640, 400, -3, 57} {
		t.Fatalf("scrolls = %v", scrolls)
	}
	if len(inputs) != 2 || inputs[0][0] != 5 || inputs[1][0] != 4 {
		t.Fatalf("inputs = %v", inputs)
	}
}

// Without Scroll (a computer that only has notches) a stray 250 is dropped,
// and what follows it still arrives.
func TestPixelScrollWithoutScrollIsDropped(t *testing.T) {
	var inputs [][]byte
	s := &Session{cfg: Config{Input: func(msg []byte) error { inputs = append(inputs, msg); return nil }}}
	data := append([]byte{0, 0, 0, 1}, append(make([]byte, 10), 4, 1, 0, 0, 0, 0, 0, 0x61)...)
	data[4] = 250
	s.reliable(data)
	if len(inputs) != 1 || inputs[0][0] != 4 {
		t.Fatalf("inputs = %v", inputs)
	}
}

// The offer tells the viewer what the computer can do.
func TestOfferSaysFineScrollAndCursor(t *testing.T) {
	b, _ := json.Marshal(Signal{Type: "rtc", SDPType: "offer", FineScroll: true, Cursor: true})
	if !strings.Contains(string(b), `"fine_scroll":true`) || !strings.Contains(string(b), `"cursor":true`) {
		t.Fatalf("offer = %s", b)
	}
	b, _ = json.Marshal(Signal{Type: "rtc", SDPType: "offer"})
	if strings.Contains(string(b), "fine_scroll") || strings.Contains(string(b), "cursor") {
		t.Fatalf("offer = %s", b)
	}
}
