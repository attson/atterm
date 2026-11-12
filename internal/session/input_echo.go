package session

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxEchoInputGlyphs = 64 * 1024

// echoConfirmedInputTracker keeps only an in-memory candidate until the PTY
// proves that the remote terminal echoed it. It deliberately prefers a missed
// command over persisting hidden password/passphrase input.
type echoConfirmedInputTracker struct {
	line         []*echoGlyph
	cursor       int
	expectations []*echoExpectation
	pending      []*echoGlyph
	invalid      bool
	inputEscape  []byte
	outState     byte
	outCSI       []byte
	outOSCEscape bool
	outErase     byte
	outputAlt    bool
}

type echoGlyph struct {
	data   []byte
	echoed bool
}

type echoExpectation struct {
	glyph   *echoGlyph
	matched int
}

func (t *echoConfirmedInputTracker) observeInput(data []byte, altScreen bool) []string {
	if altScreen {
		t.reset()
		return nil
	}
	if len(data) == 0 {
		return nil
	}
	var confirmed []string
	for len(data) > 0 {
		if len(t.inputEscape) > 0 {
			t.inputEscape = append(t.inputEscape, data[0])
			data = data[1:]
			if inputEscapeComplete(t.inputEscape) {
				t.applyInputEscape(t.inputEscape)
				t.inputEscape = nil
			} else if len(t.inputEscape) > 32 {
				t.invalid = true
				t.inputEscape = nil
			}
			continue
		}
		if data[0] == 0x1b {
			t.inputEscape = append(t.inputEscape[:0], data[0])
			data = data[1:]
			continue
		}
		switch data[0] {
		case '\r', '\n':
			if line := t.commit(); line != "" {
				confirmed = append(confirmed, line)
			}
			data = data[1:]
			continue
		case 0x08, 0x7f:
			t.backspace()
			data = data[1:]
			continue
		case 0x15: // Ctrl-U
			t.clearLine()
			data = data[1:]
			continue
		case 0x17: // Ctrl-W
			t.deleteWord()
			data = data[1:]
			continue
		case 0x03: // Ctrl-C
			t.resetLine()
			data = data[1:]
			continue
		}
		if data[0] < 0x20 {
			t.invalid = true
			data = data[1:]
			continue
		}
		end := 0
		for end < len(data) && data[end] >= 0x20 && data[end] != 0x7f && data[end] != 0x1b {
			end++
		}
		t.insertPrintable(data[:end])
		data = data[end:]
	}
	return confirmed
}

func (t *echoConfirmedInputTracker) observeOutput(data []byte, altScreen bool) []string {
	t.outputAlt = altScreen
	var confirmed []string
	for _, b := range data {
		if t.outErase != 0 {
			if t.outErase == 1 && b == ' ' {
				t.outErase = 2
				continue
			}
			if t.outErase == 2 && b == '\b' {
				t.outErase = 0
				continue
			}
			t.outErase = 0
		}
		switch t.outState {
		case 1: // ESC
			switch b {
			case '[':
				t.outState = 2
				t.outCSI = t.outCSI[:0]
			case ']':
				t.outState = 3
			default:
				t.outState = 0
			}
			continue
		case 2: // CSI
			t.outCSI = append(t.outCSI, b)
			if b >= 0x40 && b <= 0x7e {
				t.applyOutputCSI(t.outCSI)
				t.outState = 0
				t.outCSI = nil
			} else if len(t.outCSI) > 64 {
				t.outState = 0
				t.outCSI = nil
				t.resetLine()
			}
			continue
		case 3: // OSC
			if b == 0x07 {
				t.outState = 0
				t.outOSCEscape = false
			} else if t.outOSCEscape && b == '\\' {
				t.outState = 0
				t.outOSCEscape = false
			} else {
				t.outOSCEscape = b == 0x1b
			}
			continue
		}
		if b == 0x1b {
			t.outState = 1
			continue
		}
		if t.outputAlt {
			continue
		}
		if b == '\b' {
			t.outErase = 1
			continue
		}
		if b == '\r' || b == '\n' {
			if t.pending != nil {
				if line := confirmedGlyphLine(t.pending); line != "" {
					confirmed = append(confirmed, line)
				}
				t.resetLine()
			}
			continue
		}
		if b < 0x20 || b == 0x7f {
			continue
		}
		if len(t.expectations) == 0 {
			if t.pending != nil {
				// A submitted hidden value can coincidentally be the prefix of an
				// error message. Only an exact echoed line is evidence; any visible
				// suffix before CR/LF invalidates the candidate.
				t.invalid = true
				t.pending = nil
			}
			continue
		}
		exp := t.expectations[0]
		if exp.matched >= len(exp.glyph.data) || b != exp.glyph.data[exp.matched] {
			// Unexpected visible output after input means correlation is no longer
			// trustworthy. Keep accepting keystrokes, but never persist this line.
			t.invalid = true
			t.expectations = nil
			t.pending = nil
			continue
		}
		exp.matched++
		if exp.matched == len(exp.glyph.data) {
			exp.glyph.echoed = true
			t.expectations = t.expectations[1:]
		}
	}
	return confirmed
}

func (t *echoConfirmedInputTracker) insertPrintable(data []byte) {
	if len(data) == 0 || !utf8.Valid(data) {
		t.invalid = true
		return
	}
	if t.pending != nil {
		t.reset()
	}
	for len(data) > 0 {
		if len(t.line) >= maxEchoInputGlyphs {
			t.invalid = true
			return
		}
		_, size := utf8.DecodeRune(data)
		glyph := &echoGlyph{data: append([]byte(nil), data[:size]...)}
		t.line = append(t.line, nil)
		copy(t.line[t.cursor+1:], t.line[t.cursor:])
		t.line[t.cursor] = glyph
		t.cursor++
		t.expectations = append(t.expectations, &echoExpectation{glyph: glyph})
		data = data[size:]
	}
}

func (t *echoConfirmedInputTracker) commit() string {
	if t.invalid || len(t.line) == 0 {
		t.resetLine()
		return ""
	}
	t.pending = append(t.pending[:0], t.line...)
	t.line = nil
	t.cursor = 0
	if line := confirmedGlyphLine(t.pending); line != "" {
		t.reset()
		return line
	}
	return ""
}

func confirmedGlyphLine(glyphs []*echoGlyph) string {
	if len(glyphs) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, glyph := range glyphs {
		if !glyph.echoed {
			return ""
		}
		buf.Write(glyph.data)
	}
	return strings.TrimSpace(buf.String())
}

func (t *echoConfirmedInputTracker) backspace() {
	if t.cursor == 0 {
		return
	}
	t.cursor--
	t.dropGlyph(t.line[t.cursor])
	t.line = append(t.line[:t.cursor], t.line[t.cursor+1:]...)
}

func (t *echoConfirmedInputTracker) clearLine() {
	for _, glyph := range t.line {
		t.dropGlyph(glyph)
	}
	t.line = nil
	t.cursor = 0
}

func (t *echoConfirmedInputTracker) deleteWord() {
	for t.cursor > 0 {
		r, _ := utf8.DecodeRune(t.line[t.cursor-1].data)
		if !unicode.IsSpace(r) {
			break
		}
		t.backspace()
	}
	for t.cursor > 0 {
		r, _ := utf8.DecodeRune(t.line[t.cursor-1].data)
		if unicode.IsSpace(r) {
			break
		}
		t.backspace()
	}
}

func (t *echoConfirmedInputTracker) dropGlyph(glyph *echoGlyph) {
	out := t.expectations[:0]
	for _, exp := range t.expectations {
		if exp.glyph != glyph {
			out = append(out, exp)
		}
	}
	t.expectations = out
}

func (t *echoConfirmedInputTracker) resetLine() {
	t.line = nil
	t.cursor = 0
	t.pending = nil
	t.expectations = nil
	t.invalid = false
	t.inputEscape = nil
}

func (t *echoConfirmedInputTracker) reset() {
	t.resetLine()
	t.outState = 0
	t.outCSI = nil
	t.outOSCEscape = false
	t.outErase = 0
	t.outputAlt = false
}

func (t *echoConfirmedInputTracker) applyOutputCSI(seq []byte) {
	if len(seq) < 2 || seq[0] != '?' {
		return
	}
	final := seq[len(seq)-1]
	if final != 'h' && final != 'l' || !csiParamsContainAltScreen(seq[1:len(seq)-1]) {
		return
	}
	next := final == 'h'
	if next != t.outputAlt {
		t.resetLine()
		t.outErase = 0
		t.outputAlt = next
	}
}

func inputEscapeComplete(seq []byte) bool {
	if len(seq) < 2 {
		return false
	}
	if seq[1] != '[' {
		return true
	}
	return len(seq) >= 3 && seq[len(seq)-1] >= 0x40 && seq[len(seq)-1] <= 0x7e
}

func (t *echoConfirmedInputTracker) applyInputEscape(seq []byte) {
	switch string(seq) {
	case "\x1b[D":
		if t.cursor > 0 {
			t.cursor--
		}
	case "\x1b[C":
		if t.cursor < len(t.line) {
			t.cursor++
		}
	case "\x1b[H", "\x1b[1~":
		t.cursor = 0
	case "\x1b[F", "\x1b[4~":
		t.cursor = len(t.line)
	case "\x1b[3~":
		if t.cursor < len(t.line) {
			t.dropGlyph(t.line[t.cursor])
			t.line = append(t.line[:t.cursor], t.line[t.cursor+1:]...)
		}
	case "\x1b[200~", "\x1b[201~":
		// Bracketed-paste delimiters are transport metadata, not command text.
	default:
		t.invalid = true
	}
}
