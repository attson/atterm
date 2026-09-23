package peertransport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	fragmentHeaderSize = 8 + 4 + 4
	fragmentDataSize   = MaxRecordPlaintext - fragmentHeaderSize
	// MaxFrameSize is the largest marshaled proto.Frame accepted by the direct
	// transport. It matches the existing Relay connection read limit.
	MaxFrameSize = 16 * 1024 * 1024
	// ReassemblyTimeout bounds how long an incomplete frame can retain memory.
	ReassemblyTimeout = 10 * time.Second
)

var (
	// ErrInvalidFragment covers malformed, interleaved or out-of-order chunks.
	ErrInvalidFragment = errors.New("peertransport: invalid fragment")
	// ErrReassemblyTimeout means an incomplete message exceeded its deadline.
	ErrReassemblyTimeout = errors.New("peertransport: reassembly timeout")
)

// FragmentFrame splits one marshaled terminal frame into bounded FRAGMENT
// plaintexts. Callers should use a FRAME record when len(frame) <= 16 KiB.
func FragmentFrame(messageID uint64, frame []byte) ([][]byte, error) {
	if len(frame) <= MaxRecordPlaintext {
		return nil, fmt.Errorf("%w: frame does not require fragmentation", ErrInvalidFragment)
	}
	if len(frame) > MaxFrameSize {
		return nil, fmt.Errorf("%w: frame is %d bytes", ErrInvalidFragment, len(frame))
	}
	count := (len(frame) + fragmentDataSize - 1) / fragmentDataSize
	fragments := make([][]byte, 0, count)
	for offset := 0; offset < len(frame); offset += fragmentDataSize {
		end := offset + fragmentDataSize
		if end > len(frame) {
			end = len(frame)
		}
		fragment := make([]byte, fragmentHeaderSize+end-offset)
		binary.BigEndian.PutUint64(fragment[0:8], messageID)
		binary.BigEndian.PutUint32(fragment[8:12], uint32(offset))
		binary.BigEndian.PutUint32(fragment[12:16], uint32(len(frame)))
		copy(fragment[fragmentHeaderSize:], frame[offset:end])
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// Reassembler accepts one contiguous fragmented message at a time. Direct
// records are ordered and reliable, so interleaving or offset gaps indicate a
// peer implementation error and fail the route closed.
type Reassembler struct {
	messageID uint64
	total     uint32
	next      uint32
	startedAt time.Time
	buffer    []byte
	active    bool
}

// Add validates and appends one FRAGMENT plaintext. complete is true only
// when frame contains the full reassembled marshaled terminal frame.
func (r *Reassembler) Add(fragment []byte, now time.Time) (frame []byte, complete bool, err error) {
	if r.active && now.Sub(r.startedAt) > ReassemblyTimeout {
		r.Reset()
		return nil, false, ErrReassemblyTimeout
	}
	if len(fragment) <= fragmentHeaderSize || len(fragment) > MaxRecordPlaintext {
		return nil, false, fmt.Errorf("%w: size %d", ErrInvalidFragment, len(fragment))
	}
	messageID := binary.BigEndian.Uint64(fragment[0:8])
	offset := binary.BigEndian.Uint32(fragment[8:12])
	total := binary.BigEndian.Uint32(fragment[12:16])
	chunk := fragment[fragmentHeaderSize:]
	if total <= MaxRecordPlaintext || total > MaxFrameSize {
		return nil, false, fmt.Errorf("%w: total %d", ErrInvalidFragment, total)
	}
	if uint64(offset)+uint64(len(chunk)) > uint64(total) {
		return nil, false, fmt.Errorf("%w: chunk exceeds total", ErrInvalidFragment)
	}
	if !r.active {
		if offset != 0 {
			return nil, false, fmt.Errorf("%w: first offset %d", ErrInvalidFragment, offset)
		}
		r.messageID = messageID
		r.total = total
		r.startedAt = now
		r.buffer = make([]byte, 0, int(total))
		r.active = true
	}
	if messageID != r.messageID || total != r.total || offset != r.next {
		return nil, false, fmt.Errorf("%w: message or offset mismatch", ErrInvalidFragment)
	}
	r.buffer = append(r.buffer, chunk...)
	r.next += uint32(len(chunk))
	if r.next != r.total {
		return nil, false, nil
	}
	frame = r.buffer
	r.buffer = nil
	r.active = false
	r.messageID = 0
	r.total = 0
	r.next = 0
	r.startedAt = time.Time{}
	return frame, true, nil
}

// Reset drops an incomplete message and releases its backing allocation.
func (r *Reassembler) Reset() {
	r.messageID = 0
	r.total = 0
	r.next = 0
	r.startedAt = time.Time{}
	r.buffer = nil
	r.active = false
}
