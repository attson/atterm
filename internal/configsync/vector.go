package configsync

// VersionVector records the greatest contiguous operation counter stored for
// each device. Missing map entries and zero counters are equivalent.
type VersionVector map[string]uint64

// VectorRelation describes the causal relation of one version vector to
// another.
type VectorRelation uint8

const (
	VectorEqual VectorRelation = iota
	VectorBefore
	VectorAfter
	VectorConcurrent
)

// CounterRange is an inclusive operation-counter range.
type CounterRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// Clone returns an independent copy with zero entries normalized away.
func (v VersionVector) Clone() VersionVector {
	out := make(VersionVector, len(v))
	for actor, counter := range v {
		if counter != 0 {
			out[actor] = counter
		}
	}
	return out
}

// Compare reports whether v is causally before, after, equal to, or
// concurrent with other.
func (v VersionVector) Compare(other VersionVector) VectorRelation {
	vGreater := false
	otherGreater := false
	for actor, counter := range v {
		if counter > other[actor] {
			vGreater = true
		} else if counter < other[actor] {
			otherGreater = true
		}
	}
	for actor, counter := range other {
		if _, seen := v[actor]; seen {
			continue
		}
		if counter > 0 {
			otherGreater = true
		}
	}
	switch {
	case vGreater && otherGreater:
		return VectorConcurrent
	case vGreater:
		return VectorAfter
	case otherGreater:
		return VectorBefore
	default:
		return VectorEqual
	}
}

// Merge advances v to include every counter known by other.
func (v VersionVector) Merge(other VersionVector) {
	for actor, counter := range other {
		if counter > v[actor] {
			v[actor] = counter
		}
	}
}

// Covers reports whether v includes all operations summarized by other.
func (v VersionVector) Covers(other VersionVector) bool {
	for actor, counter := range other {
		if v[actor] < counter {
			return false
		}
	}
	return true
}

// MissingFrom returns the inclusive ranges present in available but missing
// from v. It is the compact request shape used by anti-entropy adapters.
func (v VersionVector) MissingFrom(available VersionVector) map[string]CounterRange {
	out := make(map[string]CounterRange)
	for actor, remoteCounter := range available {
		if localCounter := v[actor]; remoteCounter > localCounter {
			out[actor] = CounterRange{From: localCounter + 1, To: remoteCounter}
		}
	}
	return out
}
