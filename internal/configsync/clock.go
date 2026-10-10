// Package configsync implements the transport-independent replicated state
// used by Peer Spaces. Network adapters exchange immutable signed operations;
// they never write application preferences directly.
package configsync

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const DefaultMaxFutureSkew = 5 * time.Minute

var (
	ErrClockSkew        = errors.New("configsync: remote clock exceeds future-skew limit")
	ErrInvalidTimestamp = errors.New("configsync: invalid HLC timestamp")
)

// Timestamp is a hybrid logical clock value. PhysicalMS makes concurrent
// choices understandable to users; Logical keeps local events monotonic when
// wall time stalls or moves backwards.
type Timestamp struct {
	PhysicalMS int64  `json:"physical_ms"`
	Logical    uint32 `json:"logical"`
}

// CompareTimestamp compares two HLC values.
func CompareTimestamp(a, b Timestamp) int {
	if a.PhysicalMS < b.PhysicalMS {
		return -1
	}
	if a.PhysicalMS > b.PhysicalMS {
		return 1
	}
	if a.Logical < b.Logical {
		return -1
	}
	if a.Logical > b.Logical {
		return 1
	}
	return 0
}

// Clock maintains one thread-safe HLC and bounds timestamps received from
// other devices so a broken wall clock cannot dominate preferences forever.
type Clock struct {
	mu            sync.Mutex
	now           func() time.Time
	maxFutureSkew time.Duration
	last          Timestamp
}

// NewClock creates a clock backed by the system wall clock.
func NewClock(maxFutureSkew time.Duration) *Clock {
	return NewClockWithSource(time.Now, maxFutureSkew)
}

// NewClockWithSource creates a clock with an injectable wall clock for tests.
func NewClockWithSource(now func() time.Time, maxFutureSkew time.Duration) *Clock {
	if now == nil {
		now = time.Now
	}
	if maxFutureSkew < 0 {
		maxFutureSkew = 0
	}
	return &Clock{now: now, maxFutureSkew: maxFutureSkew}
}

// Tick returns the timestamp for a new local operation.
func (c *Clock) Tick() Timestamp {
	c.mu.Lock()
	defer c.mu.Unlock()

	wall := c.now().UnixMilli()
	if wall > c.last.PhysicalMS {
		c.last = Timestamp{PhysicalMS: wall}
	} else {
		c.last = incrementLogical(c.last)
	}
	return c.last
}

// Observe validates and incorporates a timestamp received from another
// device, returning the new local HLC value.
func (c *Clock) Observe(remote Timestamp) (Timestamp, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	wall := c.now().UnixMilli()
	if remote.PhysicalMS <= 0 {
		return c.last, ErrInvalidTimestamp
	}
	if remote.PhysicalMS > wall+c.maxFutureSkew.Milliseconds() {
		return c.last, fmt.Errorf("%w: remote=%d local=%d limit=%s", ErrClockSkew, remote.PhysicalMS, wall, c.maxFutureSkew)
	}

	physical := wall
	if c.last.PhysicalMS > physical {
		physical = c.last.PhysicalMS
	}
	if remote.PhysicalMS > physical {
		physical = remote.PhysicalMS
	}

	switch {
	case physical == c.last.PhysicalMS && physical == remote.PhysicalMS:
		logical := c.last.Logical
		if remote.Logical > logical {
			logical = remote.Logical
		}
		c.last = incrementLogical(Timestamp{PhysicalMS: physical, Logical: logical})
	case physical == c.last.PhysicalMS:
		c.last = incrementLogical(Timestamp{PhysicalMS: physical, Logical: c.last.Logical})
	case physical == remote.PhysicalMS:
		c.last = incrementLogical(Timestamp{PhysicalMS: physical, Logical: remote.Logical})
	default:
		c.last = Timestamp{PhysicalMS: physical}
	}
	return c.last, nil
}

// Restore advances the clock from trusted durable local state without a wall
// clock skew check. It must not be used for network input.
func (c *Clock) Restore(persisted Timestamp) error {
	if persisted.PhysicalMS <= 0 {
		return ErrInvalidTimestamp
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if CompareTimestamp(persisted, c.last) > 0 {
		c.last = persisted
	}
	return nil
}

func incrementLogical(ts Timestamp) Timestamp {
	if ts.Logical == math.MaxUint32 {
		return Timestamp{PhysicalMS: ts.PhysicalMS + 1}
	}
	ts.Logical++
	return ts
}
