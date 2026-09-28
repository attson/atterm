package peertransport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

const (
	fragmentHeaderSize       = 8 + 4 + 4
	fragmentDataSize         = MaxRecordPlaintext - fragmentHeaderSize
	configFragmentHeaderSize = 4 + 8 + 4 + 1 + 4 + 4
	configFragmentDataSize   = MaxRecordPlaintext - configFragmentHeaderSize
	signalFragmentHeaderSize = 4 + 8 + 4 + 4 + 4
	signalFragmentDataSize   = MaxRecordPlaintext - signalFragmentHeaderSize
	// MaxFrameSize is the largest marshaled proto.Frame accepted by the direct
	// transport. It matches the existing Relay connection read limit.
	MaxFrameSize = 16 * 1024 * 1024
	// MaxConfigMessageSize bounds one logical inventory, batch, or ack message.
	MaxConfigMessageSize = 16 * 1024 * 1024
	// MaxSignalMessageSize bounds one serialized SDP or ICE signal. SDP is
	// allowed to exceed one record, while remaining far below config payloads.
	MaxSignalMessageSize = 64 * 1024
	// ReassemblyTimeout bounds how long an incomplete frame can retain memory.
	ReassemblyTimeout = 10 * time.Second
)

const (
	configFragmentMagic           = "ACF1"
	configFragmentIsolationMarker = uint32(0xffffffff)
	signalFragmentMagic           = "ASF1"
	signalFragmentIsolationMarker = uint32(0xfffffffe)
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

// FragmentConfigMessage splits one config logical message while preserving its
// original kind. Its header cannot be parsed as a terminal fragment.
func FragmentConfigMessage(kind RecordKind, messageID uint64, message []byte) ([][]byte, error) {
	if !kind.configMessage() || len(message) <= MaxRecordPlaintext || len(message) > MaxConfigMessageSize {
		return nil, fmt.Errorf("%w: config kind %d size %d", ErrInvalidFragment, kind, len(message))
	}
	count := (len(message) + configFragmentDataSize - 1) / configFragmentDataSize
	fragments := make([][]byte, 0, count)
	for offset := 0; offset < len(message); offset += configFragmentDataSize {
		end := offset + configFragmentDataSize
		if end > len(message) {
			end = len(message)
		}
		fragment := make([]byte, configFragmentHeaderSize+end-offset)
		copy(fragment[0:4], configFragmentMagic)
		binary.BigEndian.PutUint64(fragment[4:12], messageID)
		binary.BigEndian.PutUint32(fragment[12:16], configFragmentIsolationMarker)
		fragment[16] = byte(kind)
		binary.BigEndian.PutUint32(fragment[17:21], uint32(offset))
		binary.BigEndian.PutUint32(fragment[21:25], uint32(len(message)))
		copy(fragment[configFragmentHeaderSize:], message[offset:end])
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// FragmentSignalMessage splits one serialized signaling message into an
// isolated format that terminal and config reassemblers cannot accept.
func FragmentSignalMessage(messageID uint64, message []byte) ([][]byte, error) {
	if len(message) <= MaxRecordPlaintext || len(message) > MaxSignalMessageSize {
		return nil, fmt.Errorf("%w: signal size %d", ErrInvalidFragment, len(message))
	}
	count := (len(message) + signalFragmentDataSize - 1) / signalFragmentDataSize
	fragments := make([][]byte, 0, count)
	for offset := 0; offset < len(message); offset += signalFragmentDataSize {
		end := offset + signalFragmentDataSize
		if end > len(message) {
			end = len(message)
		}
		fragment := make([]byte, signalFragmentHeaderSize+end-offset)
		copy(fragment[0:4], signalFragmentMagic)
		binary.BigEndian.PutUint64(fragment[4:12], messageID)
		binary.BigEndian.PutUint32(fragment[12:16], signalFragmentIsolationMarker)
		binary.BigEndian.PutUint32(fragment[16:20], uint32(offset))
		binary.BigEndian.PutUint32(fragment[20:24], uint32(len(message)))
		copy(fragment[signalFragmentHeaderSize:], message[offset:end])
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

// ConfigReassembler keeps config message state separate from terminal frame
// reassembly so one logical channel cannot complete a message for the other.
type ConfigReassembler struct {
	kind      RecordKind
	messageID uint64
	total     uint32
	next      uint32
	startedAt time.Time
	buffer    []byte
	active    bool
}

// SignalReassembler keeps signaling state independent from terminal and
// config messages so ciphertext record kinds cannot be confused across them.
type SignalReassembler struct {
	messageID uint64
	total     uint32
	next      uint32
	startedAt time.Time
	buffer    []byte
	active    bool
}

// Add validates and appends one SIGNAL_FRAGMENT plaintext.
func (r *SignalReassembler) Add(fragment []byte, now time.Time) (message []byte, complete bool, err error) {
	if r.active && now.Sub(r.startedAt) > ReassemblyTimeout {
		r.Reset()
		return nil, false, ErrReassemblyTimeout
	}
	if len(fragment) <= signalFragmentHeaderSize || len(fragment) > MaxRecordPlaintext ||
		string(fragment[0:4]) != signalFragmentMagic ||
		binary.BigEndian.Uint32(fragment[12:16]) != signalFragmentIsolationMarker {
		return nil, false, fmt.Errorf("%w: signal fragment structure", ErrInvalidFragment)
	}
	messageID := binary.BigEndian.Uint64(fragment[4:12])
	offset := binary.BigEndian.Uint32(fragment[16:20])
	total := binary.BigEndian.Uint32(fragment[20:24])
	chunk := fragment[signalFragmentHeaderSize:]
	if total <= MaxRecordPlaintext || total > MaxSignalMessageSize ||
		uint64(offset)+uint64(len(chunk)) > uint64(total) {
		return nil, false, fmt.Errorf("%w: signal fragment bounds", ErrInvalidFragment)
	}
	if !r.active {
		if offset != 0 {
			return nil, false, fmt.Errorf("%w: first signal offset %d", ErrInvalidFragment, offset)
		}
		r.messageID = messageID
		r.total = total
		r.startedAt = now
		r.buffer = make([]byte, 0, int(total))
		r.active = true
	}
	if messageID != r.messageID || total != r.total || offset != r.next {
		return nil, false, fmt.Errorf("%w: signal message or offset mismatch", ErrInvalidFragment)
	}
	r.buffer = append(r.buffer, chunk...)
	r.next += uint32(len(chunk))
	if r.next != r.total {
		return nil, false, nil
	}
	message = r.buffer
	r.Reset()
	return message, true, nil
}

// Reset drops one incomplete signaling message.
func (r *SignalReassembler) Reset() {
	r.messageID = 0
	r.total = 0
	r.next = 0
	r.startedAt = time.Time{}
	r.buffer = nil
	r.active = false
}

// Add validates and appends one CONFIG_FRAGMENT plaintext.
func (r *ConfigReassembler) Add(fragment []byte, now time.Time) (kind RecordKind, message []byte, complete bool, err error) {
	if r.active && now.Sub(r.startedAt) > ReassemblyTimeout {
		r.Reset()
		return 0, nil, false, ErrReassemblyTimeout
	}
	if len(fragment) <= configFragmentHeaderSize || len(fragment) > MaxRecordPlaintext ||
		string(fragment[0:4]) != configFragmentMagic ||
		binary.BigEndian.Uint32(fragment[12:16]) != configFragmentIsolationMarker {
		return 0, nil, false, fmt.Errorf("%w: config fragment structure", ErrInvalidFragment)
	}
	messageID := binary.BigEndian.Uint64(fragment[4:12])
	kind = RecordKind(fragment[16])
	offset := binary.BigEndian.Uint32(fragment[17:21])
	total := binary.BigEndian.Uint32(fragment[21:25])
	chunk := fragment[configFragmentHeaderSize:]
	if !kind.configMessage() || total <= MaxRecordPlaintext || total > MaxConfigMessageSize ||
		uint64(offset)+uint64(len(chunk)) > uint64(total) {
		return 0, nil, false, fmt.Errorf("%w: config fragment bounds", ErrInvalidFragment)
	}
	if !r.active {
		if offset != 0 {
			return 0, nil, false, fmt.Errorf("%w: first config offset %d", ErrInvalidFragment, offset)
		}
		r.kind = kind
		r.messageID = messageID
		r.total = total
		r.startedAt = now
		r.buffer = make([]byte, 0, int(total))
		r.active = true
	}
	if kind != r.kind || messageID != r.messageID || total != r.total || offset != r.next {
		return 0, nil, false, fmt.Errorf("%w: config message or offset mismatch", ErrInvalidFragment)
	}
	r.buffer = append(r.buffer, chunk...)
	r.next += uint32(len(chunk))
	if r.next != r.total {
		return 0, nil, false, nil
	}
	kind = r.kind
	message = r.buffer
	r.Reset()
	return kind, message, true, nil
}

// Reset drops one incomplete config message.
func (r *ConfigReassembler) Reset() {
	r.kind = 0
	r.messageID = 0
	r.total = 0
	r.next = 0
	r.startedAt = time.Time{}
	r.buffer = nil
	r.active = false
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
