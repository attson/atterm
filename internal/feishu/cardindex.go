// cardindex.go holds the in-memory registry that maps atterm session IDs
// to their Feishu DM anchor cards, plus the two reverse indices used by
// the inbound router (reply target msg_id, card action token).
//
// The map is held in-process only; an atterm restart drops it, after which
// old anchors become dead cards (see spec §Failure modes). All accesses are
// guarded by a single RWMutex — the workload is read-heavy (every inbound
// event probes), so RLock/Lock is fine without further sharding.
package feishu

import (
	"sync"
	"time"
)

// CardAnchor is a single live anchor card. The fields are all immutable
// after creation EXCEPT LastPatchAt, LastBody, and PatchSeq, which the
// chunker updates under its own lock (not this index's lock).
type CardAnchor struct {
	SessionID   string
	CardMsgID   string
	CardToken   string
	OwnerOpenID string
	CreatedAt   time.Time

	// Mutated by the outbound chunker; cardindex does not protect these
	// because the chunker holds the only writer to a given session's anchor.
	LastPatchAt time.Time
	LastBody    string

	// PatchSeq is atomically incremented on each PATCH call so Feishu can
	// drop out-of-order updates. The chunker increments it before every
	// PatchCard call; only the chunker goroutine writes it, so atomic ops
	// are sufficient without a mutex.
	PatchSeq int64
}

type CardIndex struct {
	mu      sync.RWMutex
	bySess  map[string]*CardAnchor
	byMsg   map[string]*CardAnchor
	byToken map[string]*CardAnchor
}

func NewCardIndex() *CardIndex {
	return &CardIndex{
		bySess:  make(map[string]*CardAnchor),
		byMsg:   make(map[string]*CardAnchor),
		byToken: make(map[string]*CardAnchor),
	}
}

// Put stores a new anchor. If a previous anchor existed for the same
// SessionID it is replaced (and its msg/token indices removed).
func (i *CardIndex) Put(a *CardAnchor) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if prev, ok := i.bySess[a.SessionID]; ok {
		delete(i.byMsg, prev.CardMsgID)
		delete(i.byToken, prev.CardToken)
	}
	i.bySess[a.SessionID] = a
	i.byMsg[a.CardMsgID] = a
	i.byToken[a.CardToken] = a
}

func (i *CardIndex) BySessionID(id string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.bySess[id]
}

func (i *CardIndex) ByMsgID(id string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.byMsg[id]
}

func (i *CardIndex) ByCardToken(tok string) *CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.byToken[tok]
}

// RemoveBySessionID drops the anchor for the given session, clearing all
// three indices. Safe to call when no anchor exists.
func (i *CardIndex) RemoveBySessionID(id string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if a, ok := i.bySess[id]; ok {
		delete(i.bySess, id)
		delete(i.byMsg, a.CardMsgID)
		delete(i.byToken, a.CardToken)
	}
}

// Snapshot returns a copy of all current anchors. Used by the master-switch
// teardown path to PATCH every anchor to archive state in one pass.
func (i *CardIndex) Snapshot() []*CardAnchor {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]*CardAnchor, 0, len(i.bySess))
	for _, a := range i.bySess {
		out = append(out, a)
	}
	return out
}
