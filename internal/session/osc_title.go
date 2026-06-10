// Package session — OSC 0/1/2 (icon/window title) parser.
//
// XTerm-style escape sequences for terminal titles:
//   ESC ] 0 ; <str> ST   sets both window title and icon (tab) title
//   ESC ] 1 ; <str> ST   sets icon (tab) title only
//   ESC ] 2 ; <str> ST   sets window title only
// where ST is BEL (0x07) or ESC \ (0x1b 0x5c).
//
// atterm has a 1:1 PTY:tab mapping, so we collapse all three prefixes onto a
// single Title field (last-writer-wins) — there is no icon vs window
// distinction to surface.

package session

import (
	"bytes"
	"unicode/utf8"
)

// maxOSCTitlePayload caps the payload length we accept inside an OSC 0/1/2.
// Bounds buffer growth on split Appends and protects against OSC bombing.
const maxOSCTitlePayload = 256

// scanOSCTitles walks data looking for complete OSC 0/1/2 title sequences
// (BEL- or ESC-backslash-terminated). It returns every accepted title in
// order (caller picks last-writer-wins), the byte index in data the caller
// may safely discard, and ok=true when at least one complete title was
// accepted. Bytes from consumed onward are either plain text or an
// incomplete OSC waiting for its terminator on the next chunk — the caller
// keeps them in a buffer for continuation.
//
// Pure; the calling Session method owns the cross-Append buffer.
func scanOSCTitles(data []byte) (titles []string, consumed int, ok bool) {
	pos := 0
	for pos < len(data) {
		idx := indexOSCTitlePrefix(data[pos:])
		if idx < 0 {
			return titles, len(data), ok
		}
		startAbs := pos + idx
		payloadStart := startAbs + 4 // len("\x1b]N;")
		if payloadStart > len(data) {
			return titles, startAbs, ok
		}
		searchEnd := payloadStart + maxOSCTitlePayload + 2
		if searchEnd > len(data) {
			searchEnd = len(data)
		}
		termRel, termLen := oscTerminator(data[payloadStart:searchEnd])
		if termRel < 0 {
			// No terminator in the search window.
			//   a) Hit end of data → incomplete OSC, caller keeps tail.
			//   b) Hit searchEnd before end-of-data → payload too long;
			//      skip past the introducer and continue.
			if searchEnd == len(data) {
				return titles, startAbs, ok
			}
			pos = payloadStart
			continue
		}
		payload := data[payloadStart : payloadStart+termRel]
		if title, accept := normalizeOSCTitle(payload); accept {
			titles = append(titles, title)
			ok = true
		}
		pos = payloadStart + termRel + termLen
	}
	return titles, len(data), ok
}

// indexOSCTitlePrefix returns the index of the next byte sequence
// `ESC ] [0-2] ;` in buf, or -1 if none.
func indexOSCTitlePrefix(buf []byte) int {
	off := 0
	for {
		i := bytes.IndexByte(buf[off:], 0x1b)
		if i < 0 {
			return -1
		}
		abs := off + i
		if abs+3 < len(buf) &&
			buf[abs+1] == ']' &&
			(buf[abs+2] == '0' || buf[abs+2] == '1' || buf[abs+2] == '2') &&
			buf[abs+3] == ';' {
			return abs
		}
		off = abs + 1
		if off >= len(buf) {
			return -1
		}
	}
}

// normalizeOSCTitle strips C0 control bytes and DEL, then validates UTF-8.
// Returns (title, accept). Rejects empty or non-UTF8 payloads.
func normalizeOSCTitle(payload []byte) (string, bool) {
	if len(payload) == 0 || len(payload) > maxOSCTitlePayload {
		return "", false
	}
	cleaned := make([]byte, 0, len(payload))
	for _, b := range payload {
		if b < 0x20 || b == 0x7f {
			continue
		}
		cleaned = append(cleaned, b)
	}
	if len(cleaned) == 0 {
		return "", false
	}
	if !utf8.Valid(cleaned) {
		return "", false
	}
	return string(cleaned), true
}
