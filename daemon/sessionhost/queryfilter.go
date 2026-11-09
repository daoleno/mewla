package sessionhost

import (
	"bytes"
	"strconv"
)

// queryFilter takes terminal queries out of a Session's output on its way to
// a viewer. The host's terminal answers every query the program sends; a
// viewer that answered too (a real terminal under mewla attach) would put a
// second answer in front of the program as if it were typed. Everything that
// is not a query passes through unchanged and in order. A sequence split
// across writes is held until it is complete; one too long to be a query is
// passed through as soon as that is clear.
type queryFilter struct {
	state filterState
	seq   []byte // the sequence being read, from its ESC
}

type filterState int

const (
	filterGround    filterState = iota
	filterEsc                   // after ESC
	filterCSI                   // ESC [ ...
	filterString                // ESC ] (OSC), ESC P (DCS) or ESC _ (APC), held
	filterStringEsc             // an ESC inside a held string
	filterPass                  // a string being passed through to its end
	filterPassEsc
	filterDrop // a string query being dropped to its end
	filterDropEsc
)

const (
	esc = 0x1b
	bel = 0x07
	can = 0x18
	sub = 0x1a

	// maxQueryCSI and maxQueryString bound what the filter holds: queries
	// are short, and anything longer goes through as it comes.
	maxQueryCSI    = 64
	maxQueryString = 256
)

// write filters p and returns what the viewer gets.
func (f *queryFilter) write(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		out = f.step(out, b)
	}
	return out
}

func (f *queryFilter) step(out []byte, b byte) []byte {
	switch f.state {
	case filterGround:
		if b == esc {
			f.state, f.seq = filterEsc, append(f.seq[:0], b)
			return out
		}
		return append(out, b)

	case filterEsc:
		f.seq = append(f.seq, b)
		switch b {
		case '[':
			f.state = filterCSI
		case ']', 'P', '_':
			f.state = filterString
		case 'Z': // DECID: the old form of a primary device attributes query
			f.state = filterGround
		case esc:
			// ESC ESC: the first one stands alone.
			out = append(out, esc)
			f.seq = f.seq[:1]
		default:
			out = append(out, f.seq...)
			f.state = filterGround
		}
		return out

	case filterCSI:
		switch {
		case b >= 0x40 && b <= 0x7e: // final byte
			f.seq = append(f.seq, b)
			f.state = filterGround
			if isQueryCSI(f.seq[2:len(f.seq)-1], b) {
				return out
			}
			return append(out, f.seq...)
		case b == esc:
			// An interrupted sequence: the viewer drops it the same way.
			out = append(out, f.seq...)
			f.state, f.seq = filterEsc, append(f.seq[:0], b)
			return out
		case b == can || b == sub:
			f.state = filterGround
			return append(append(out, f.seq...), b)
		case b < 0x20:
			// C0 controls inside a sequence act at once; so they do here.
			return append(out, b)
		}
		f.seq = append(f.seq, b)
		if len(f.seq) > maxQueryCSI {
			// Too long for a query: let the rest through as it comes.
			out = append(out, f.seq...)
			f.state, f.seq = filterGround, f.seq[:0]
			// The final byte still has to arrive; ground passes it.
		}
		return out

	case filterString:
		switch b {
		case bel:
			f.seq = append(f.seq, b)
			return f.endString(out)
		case esc:
			f.state = filterStringEsc
			return out
		case can, sub:
			f.state = filterGround
			return append(append(out, f.seq...), b)
		}
		f.seq = append(f.seq, b)
		if verdict := stringVerdict(f.seq); verdict != undecided {
			if verdict == query {
				f.state = filterDrop
				return out
			}
			f.state = filterPass
			return append(out, f.seq...)
		}
		if len(f.seq) > maxQueryString {
			f.state = filterPass
			return append(out, f.seq...)
		}
		return out

	case filterStringEsc:
		if b == '\\' {
			f.seq = append(f.seq, esc, b)
			return f.endString(out)
		}
		// ESC that is not ST ends the string and starts a new sequence.
		out = append(out, f.seq...)
		f.state, f.seq = filterEsc, append(f.seq[:0], esc)
		return f.step(out, b)

	case filterPass:
		switch b {
		case bel, can, sub:
			f.state = filterGround
		case esc:
			f.state = filterPassEsc
			return out
		}
		return append(out, b)

	case filterPassEsc:
		if b == '\\' {
			f.state = filterGround
			return append(out, esc, b)
		}
		// ESC that is not ST ends the string and starts a new sequence.
		f.state, f.seq = filterEsc, append(f.seq[:0], esc)
		return f.step(out, b)

	case filterDrop:
		switch b {
		case bel, can, sub:
			f.state = filterGround
		case esc:
			f.state = filterDropEsc
		}
		return out

	case filterDropEsc:
		if b == '\\' {
			f.state = filterGround
			return out
		}
		f.state, f.seq = filterEsc, append(f.seq[:0], esc)
		return f.step(out, b)
	}
	return out
}

// endString settles a string sequence that ended while held.
func (f *queryFilter) endString(out []byte) []byte {
	f.state = filterGround
	if isQueryString(f.seq) {
		return out
	}
	return append(out, f.seq...)
}

// isQueryCSI reports whether CSI params final asks the terminal to answer.
// params holds the parameter and intermediate bytes.
func isQueryCSI(params []byte, final byte) bool {
	prefix := byte(0)
	if len(params) > 0 && params[0] >= '<' && params[0] <= '?' {
		prefix, params = params[0], params[1:]
	}
	intermediate := byte(0)
	if i := bytes.IndexFunc(params, func(r rune) bool { return r >= 0x20 && r <= 0x2f }); i >= 0 {
		intermediate = params[i]
		params = params[:i]
	}
	switch final {
	case 'c': // primary, secondary and tertiary device attributes
		return intermediate == 0 && (prefix == 0 || prefix == '>' || prefix == '=')
	case 'n': // device status reports, cursor position, color scheme
		return intermediate == 0 && (prefix == 0 || prefix == '?')
	case 'p': // DECRQM, a mode's state
		return intermediate == '$' && (prefix == 0 || prefix == '?')
	case 'q': // XTVERSION
		return intermediate == 0 && prefix == '>'
	case 'u': // the Kitty keyboard flags
		return intermediate == 0 && prefix == '?'
	case 'm': // XTQMODKEYS
		return intermediate == 0 && prefix == '?'
	case 'S': // XTSMGRAPHICS
		return intermediate == 0 && prefix == '?'
	case 'x': // DECREQTPARM
		return intermediate == 0 && prefix == 0
	case 'w': // DECRQPSR
		return intermediate == '$' && prefix == 0
	case 'y': // DECRQCRA, a checksum of the screen
		return intermediate == '*' && prefix == 0
	case 't': // XTWINOPS reports
		if intermediate != 0 || prefix != 0 {
			return false
		}
		first, _, _ := bytes.Cut(params, []byte(";"))
		switch string(first) {
		case "11", "13", "14", "15", "16", "18", "19", "20", "21":
			return true
		}
	}
	return false
}

type verdict int

const (
	undecided verdict = iota
	query
	notQuery
)

// stringVerdict decides a string sequence from its start where it can, so a
// long one (a clipboard write, an image) is never held.
func stringVerdict(seq []byte) verdict {
	switch seq[1] {
	case 'P': // DCS: XTGETTCAP is DCS + q, DECRQSS is DCS $ q
		if len(seq) < 4 {
			return undecided
		}
		if (seq[2] == '+' || seq[2] == '$') && seq[3] == 'q' {
			return query
		}
		return notQuery
	case '_': // APC: Kitty graphics, which the host does not offer
		if len(seq) < 3 {
			return undecided
		}
		if seq[2] == 'G' {
			return query
		}
		return notQuery
	case ']': // OSC: decided by its number once it is known
		body := seq[2:]
		semi := bytes.IndexByte(body, ';')
		if semi < 0 {
			if len(body) > 8 {
				return notQuery
			}
			return undecided
		}
		number, err := strconv.Atoi(string(body[:semi]))
		if err != nil {
			return notQuery
		}
		switch {
		case number == 4, number == 5, number >= 10 && number <= 19, number == 21, number == 52:
			// Color, special color, dynamic color, Kitty color and clipboard
			// queries: held to the end, where "?" tells.
			return undecided
		}
		return notQuery
	}
	return notQuery
}

// isQueryString reports whether a complete OSC, DCS or APC asks for an answer.
func isQueryString(seq []byte) bool {
	body := bytes.TrimSuffix(bytes.TrimSuffix(seq[2:], []byte{bel}), []byte{esc, '\\'})
	switch seq[1] {
	case 'P':
		return len(body) >= 2 && (body[0] == '+' || body[0] == '$') && body[1] == 'q'
	case '_':
		return len(body) >= 1 && body[0] == 'G'
	case ']':
		fields := bytes.Split(body, []byte(";"))
		if len(fields) < 2 {
			return false
		}
		number, err := strconv.Atoi(string(fields[0]))
		if err != nil {
			return false
		}
		if number == 21 {
			return bytes.Contains(body, []byte("=?"))
		}
		if number == 4 || number == 5 || (number >= 10 && number <= 19) || number == 52 {
			for _, field := range fields[1:] {
				if string(field) == "?" {
					return true
				}
			}
		}
	}
	return false
}
