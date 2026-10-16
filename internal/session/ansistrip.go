package session

type ansiStripState uint8

const (
	ansiStripText ansiStripState = iota
	ansiStripAfterESC
	ansiStripCSI
	ansiStripOSC
	ansiStripOSCEscape
	ansiStripString
	ansiStripStringEscape
)

// ANSIStreamStripper removes terminal escape sequences while retaining parser
// state between calls. PTY frame boundaries are arbitrary and frequently land
// inside OSC 133 or SGR sequences, so streaming consumers must reuse one
// instance instead of calling StripANSI independently for each frame.
type ANSIStreamStripper struct {
	state             ansiStripState
	started           bool
	utf8Continuations uint8
}

// Strip returns the visible bytes from the next terminal stream chunk. The
// returned slice is newly allocated and remains valid after later calls.
func (s *ANSIStreamStripper) Strip(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	if !s.started {
		// A late subscriber's first frame may begin inside a CSI sequence.
		b = stripLeadingCSITail(b)
		s.started = true
	}

	out := make([]byte, 0, len(b))
	for _, c := range b {
		if s.state == ansiStripText && s.utf8Continuations != 0 {
			if c&0xc0 == 0x80 {
				out = append(out, c)
				s.utf8Continuations--
				continue
			}
			// The previous UTF-8 lead byte was invalid. Its consumer will drop
			// it; process this byte normally so an ESC still starts a sequence.
			s.utf8Continuations = 0
		}
		switch s.state {
		case ansiStripText:
			switch c {
			case 0x1b:
				s.state = ansiStripAfterESC
			case 0x9b: // 8-bit CSI
				s.state = ansiStripCSI
			case 0x9d: // 8-bit OSC
				s.state = ansiStripOSC
			case 0x90, 0x98, 0x9e, 0x9f: // DCS, SOS, PM, APC
				s.state = ansiStripString
			case 0x9c: // orphan 8-bit ST
			default:
				out = append(out, c)
				s.utf8Continuations = utf8ContinuationCount(c)
			}
		case ansiStripAfterESC:
			switch c {
			case '[':
				s.state = ansiStripCSI
			case ']':
				s.state = ansiStripOSC
			case 'P', 'X', '^', '_':
				s.state = ansiStripString
			default:
				// ESC plus a single final/intermediate command has no visible text.
				s.state = ansiStripText
			}
		case ansiStripCSI:
			if c >= 0x40 && c <= 0x7e {
				s.state = ansiStripText
			} else if c == 0x1b {
				s.state = ansiStripAfterESC
			}
		case ansiStripOSC:
			switch c {
			case 0x07, 0x9c: // BEL or 8-bit ST
				s.state = ansiStripText
			case 0x1b:
				s.state = ansiStripOSCEscape
			}
		case ansiStripOSCEscape:
			if c == '\\' {
				s.state = ansiStripText
			} else {
				// A malformed ESC inside OSC remains part of the control string.
				s.state = ansiStripOSC
			}
		case ansiStripString:
			switch c {
			case 0x9c:
				s.state = ansiStripText
			case 0x1b:
				s.state = ansiStripStringEscape
			}
		case ansiStripStringEscape:
			if c == '\\' {
				s.state = ansiStripText
			} else {
				s.state = ansiStripString
			}
		}
	}
	return out
}

func utf8ContinuationCount(c byte) uint8 {
	switch {
	case c >= 0xc2 && c <= 0xdf:
		return 1
	case c >= 0xe0 && c <= 0xef:
		return 2
	case c >= 0xf0 && c <= 0xf4:
		return 3
	default:
		return 0
	}
}

// StripANSI removes ANSI / VT escape sequences from b, returning a fresh
// slice. It handles both 7-bit ESC-prefixed and 8-bit C1 forms, including:
//   - CSI:  ESC '[' ... <final byte in 0x40..0x7E>
//   - OSC:  ESC ']' ... <BEL (0x07) or ST (ESC '\')>
//   - DCS/SOS/PM/APC control strings terminated by ST
//   - Other single-byte ESC sequences (ESC X): the ESC and the following byte
//
// Additionally, a leading "orphan CSI tail" is stripped: if the input
// begins with a run of CSI parameter bytes (digits or ';') terminated by
// a CSI final byte (0x40-0x7E), those bytes are dropped. This guards
// against callers that hand us a tail-buffer slice cut in the middle of
// a CSI sequence — without this, the parameter bytes would leak through
// as garbage like "153;153;153mHello".
//
// All other input — including newlines, tabs, ASCII, multi-byte UTF-8,
// and CR — is preserved verbatim. A truncated sequence at end-of-input
// is dropped silently (the partial sequence is consumed but not emitted).
//
// Single-pass, O(len(b)) time, no regex.
func StripANSI(b []byte) []byte {
	var stripper ANSIStreamStripper
	return stripper.Strip(b)
}

// stripLeadingCSITail drops a run of digits / semicolons at the start of
// b when terminated by a CSI final byte (0x40-0x7E). The shape matches
// the suffix of a typical CSI sequence — e.g. SGR color codes
// "38;2;153;153;153m" — so a tail buffer cut mid-sequence doesn't leak
// garbage into the stripped output. Requires at least one digit in the
// run so prose like ";note" survives, and constrains intermediates to
// digits/';' so the false-positive surface stays vanishingly small.
func stripLeadingCSITail(b []byte) []byte {
	i := 0
	sawDigit := false
	for i < len(b) && (isDigit(b[i]) || b[i] == ';') {
		if isDigit(b[i]) {
			sawDigit = true
		}
		i++
	}
	if !sawDigit || i >= len(b) {
		return b
	}
	if b[i] >= 0x40 && b[i] <= 0x7E {
		return b[i+1:]
	}
	return b
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
