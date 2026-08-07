package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// FS frame payloads are segmented rather than a single JSON document so
// file bytes can travel raw inside an AEAD envelope instead of being
// base64'd into JSON. Segment 0 is always plaintext JSON carrying the
// fields the relay routes on; later segments are sealed envelopes.
//
//	payload := segment_count(1B) || segment*
//	segment := length(4B BE) || bytes
//
// The other three sealed sites (META / SessionInfo / CommandEvent)
// base64 an envelope into a JSON string field. That is right for their
// scale — tens of bytes of metadata — but would base64 file contents a
// second time on top of the encoding encoding/json already applies to
// []byte, for 1.78x expansion. Segments keep it at ~1.0x.
var (
	ErrFSSegmentsMalformed = errors.New("proto: malformed FS segments")
	ErrFSSegmentTooLarge   = errors.New("proto: FS segment exceeds max payload")
)

// EncodeSegments frames segments into a single payload. At least one
// segment is required; segment 0 is by convention the plaintext head.
func EncodeSegments(segments [][]byte) ([]byte, error) {
	if len(segments) == 0 {
		return nil, fmt.Errorf("%w: need at least one segment", ErrFSSegmentsMalformed)
	}
	if len(segments) > 255 {
		return nil, fmt.Errorf("%w: %d segments", ErrFSSegmentsMalformed, len(segments))
	}
	total := 1
	for _, s := range segments {
		if len(s) > maxPayload {
			return nil, ErrFSSegmentTooLarge
		}
		total += 4 + len(s)
	}
	out := make([]byte, 0, total)
	out = append(out, byte(len(segments)))
	for _, s := range segments {
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(s)))
		out = append(out, lenBuf[:]...)
		out = append(out, s...)
	}
	return out, nil
}

// DecodeSegments is the inverse of EncodeSegments. It rejects declared
// lengths that do not sum to exactly the remaining bytes, so a truncated
// or padded payload fails loudly rather than yielding partial segments.
func DecodeSegments(payload []byte) ([][]byte, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("%w: empty payload", ErrFSSegmentsMalformed)
	}
	count := int(payload[0])
	if count == 0 {
		return nil, fmt.Errorf("%w: zero segments", ErrFSSegmentsMalformed)
	}
	rest := payload[1:]
	segments := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		if len(rest) < 4 {
			return nil, fmt.Errorf("%w: truncated length prefix at segment %d", ErrFSSegmentsMalformed, i)
		}
		n := int(binary.BigEndian.Uint32(rest[:4]))
		if n > maxPayload {
			return nil, ErrFSSegmentTooLarge
		}
		rest = rest[4:]
		if len(rest) < n {
			return nil, fmt.Errorf("%w: truncated body at segment %d", ErrFSSegmentsMalformed, i)
		}
		seg := make([]byte, n)
		copy(seg, rest[:n])
		segments = append(segments, seg)
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrFSSegmentsMalformed, len(rest))
	}
	return segments, nil
}
