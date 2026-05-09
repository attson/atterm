// Package ringbuf is a byte-budgeted, FIFO ring of OUT-payload chunks tagged with seq.
//
// It backs each session's scrollback: agents push fixed-size OUT chunks; clients
// catching up call Since(lastSeq) to replay. When the byte budget is exceeded
// the oldest chunks are dropped — readers detect this via OldestSeq() and treat
// it as a truncation event (the protocol layer handles that).
package ringbuf

import "sync"

// Chunk is one OUT frame's bytes paired with its seq.
type Chunk struct {
	Seq  uint64
	Data []byte
}

// Buffer is a byte-budgeted FIFO of Chunks.
type Buffer struct {
	mu       sync.RWMutex
	chunks   []Chunk
	bytes    int
	maxBytes int
}

// New returns a Buffer with the given byte budget.
func New(maxBytes int) *Buffer {
	if maxBytes <= 0 {
		maxBytes = 4 * 1024 * 1024
	}
	return &Buffer{maxBytes: maxBytes}
}

// Push appends a chunk, evicting the oldest until the byte budget is satisfied.
func (b *Buffer) Push(c Chunk) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chunks = append(b.chunks, c)
	b.bytes += len(c.Data)
	for b.bytes > b.maxBytes && len(b.chunks) > 1 {
		b.bytes -= len(b.chunks[0].Data)
		b.chunks = b.chunks[1:]
	}
}

// Since returns chunks with Seq > sinceSeq, in order. Caller must not mutate.
func (b *Buffer) Since(sinceSeq uint64) []Chunk {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.chunks) == 0 {
		return nil
	}
	// linear scan — chunks are seq-ordered, so binary search would work, but
	// the typical replay length is short enough that a scan is fine here.
	for i, c := range b.chunks {
		if c.Seq > sinceSeq {
			out := make([]Chunk, len(b.chunks)-i)
			copy(out, b.chunks[i:])
			return out
		}
	}
	return nil
}

// OldestSeq returns the smallest seq still in the buffer, or 0 if empty.
func (b *Buffer) OldestSeq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.chunks) == 0 {
		return 0
	}
	return b.chunks[0].Seq
}

// LatestSeq returns the largest seq in the buffer, or 0 if empty.
func (b *Buffer) LatestSeq() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.chunks) == 0 {
		return 0
	}
	return b.chunks[len(b.chunks)-1].Seq
}
