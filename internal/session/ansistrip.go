package session

// StripANSI removes ANSI / VT escape sequences from b, returning a fresh
// slice. Three sequence shapes are stripped:
//   - CSI:  ESC '[' ... <final byte in 0x40..0x7E>
//   - OSC:  ESC ']' ... <BEL (0x07) or ST (ESC '\')>
//   - Other single-byte ESC sequences (ESC X): the ESC and the following byte
//
// All other input — including newlines, tabs, ASCII, multi-byte UTF-8,
// and CR — is preserved verbatim. A truncated sequence at end-of-input
// is dropped silently (the partial sequence is consumed but not emitted).
//
// Single-pass, O(len(b)) time, no regex.
func StripANSI(b []byte) []byte {
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
