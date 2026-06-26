package session

// StripANSI removes ANSI / VT escape sequences from b, returning a fresh
// slice. Three sequence shapes are stripped:
//   - CSI:  ESC '[' ... <final byte in 0x40..0x7E>
//   - OSC:  ESC ']' ... <BEL (0x07) or ST (ESC '\')>
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
	b = stripLeadingCSITail(b)
	const (
		stateText = iota
		stateAfterEsc
		stateCSI
		stateOSC
	)
	out := make([]byte, 0, len(b))
	state := stateText
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case stateText:
			if c == 0x1b { // ESC
				state = stateAfterEsc
				continue
			}
			out = append(out, c)
		case stateAfterEsc:
			switch c {
			case '[':
				state = stateCSI
			case ']':
				state = stateOSC
			default:
				// ESC X — drop ESC and X.
				state = stateText
			}
		case stateCSI:
			// CSI parameter bytes 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E.
			if c >= 0x40 && c <= 0x7E {
				state = stateText
			}
			// else stay in CSI consuming parameter/intermediate bytes.
		case stateOSC:
			if c == 0x07 { // BEL terminator
				state = stateText
				continue
			}
			if c == 0x1b { // ST is ESC + '\'. Consume ESC; look for '\' next.
				// Peek next byte.
				if i+1 < len(b) && b[i+1] == '\\' {
					i++
					state = stateText
					continue
				}
				// Lone ESC inside OSC — treat as text-state reset.
				state = stateText
				continue
			}
		}
	}
	return out
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
