package relay

import (
	"context"
	"errors"

	"github.com/attson/atterm/internal/userstore"
	"golang.org/x/sync/semaphore"
)

// ErrArgon2Overloaded is returned by Verify when the queue of pending argon2id
// work exceeds the pool's capacity. Callers should translate this to an HTTP
// 503 response.
var ErrArgon2Overloaded = errors.New("relay: argon2 work pool overloaded; retry later")

// queueDepth is the number of callers that may wait behind the concurrency
// cap before new callers fail fast with ErrArgon2Overloaded (SEC-5).
const queueDepth = 32

// argon2Verify is the verify function used by Argon2Pool. It is a package-level
// var so tests can inject a stub without polluting the production API.
var argon2Verify = func(pw, encoded string) bool {
	return userstore.VerifyPasswordForBootstrap(pw, encoded)
}

// Argon2Pool bounds concurrent argon2id work via two tiers:
//  1. A buffered channel (slots) of capacity concurrency+queueDepth: callers
//     that cannot reserve a slot fail immediately with ErrArgon2Overloaded.
//  2. A semaphore of weight concurrency: callers that reserved a slot block
//     here until a worker finishes, ensuring at most `concurrency` argon2id
//     operations run in parallel.
type Argon2Pool struct {
	// slots is a token bucket; each caller must hold a token while it waits
	// for and occupies a semaphore slot.
	slots chan struct{}
	// sem limits the number of argon2id operations actually running in parallel.
	sem *semaphore.Weighted
	// dummyHash is generated at pool creation with the current argon2 params
	// (SEC-3). Used for timing-safe missing-email login paths.
	dummyHash string
}

// NewArgon2Pool returns a pool that bounds concurrent argon2id work to
// concurrency goroutines. If concurrency < 1, it is clamped to 1.
func NewArgon2Pool(concurrency int) *Argon2Pool {
	if concurrency < 1 {
		concurrency = 1
	}
	// Generate the startup dummy hash with the current SEC-2 argon2 params.
	h, err := userstore.HashPasswordForBootstrap("dummy-bootstrap-9e8d7c6b5a")
	if err != nil {
		panic("relay: argon2 dummy bootstrap: " + err.Error())
	}
	return &Argon2Pool{
		slots:     make(chan struct{}, int64(concurrency)+queueDepth),
		sem:       semaphore.NewWeighted(int64(concurrency)),
		dummyHash: h,
	}
}

// DummyHash returns the argon2id hash generated at startup. Callers use this
// for timing-safe missing-email login paths (SEC-3).
func (p *Argon2Pool) DummyHash() string { return p.dummyHash }

// Verify runs password verification with concurrency bounded by the pool.
// Returns ErrArgon2Overloaded if the queue is full (>= concurrency+queueDepth
// callers already in flight or waiting). Returns a context error if ctx is
// canceled while waiting for a semaphore slot.
func (p *Argon2Pool) Verify(ctx context.Context, plaintext, hash string) (bool, error) {
	// Tier 1: non-blocking slot reservation. If the channel is full, fail fast.
	select {
	case p.slots <- struct{}{}:
	default:
		return false, ErrArgon2Overloaded
	}
	defer func() { <-p.slots }()

	// Tier 2: block on the semaphore until a worker slot is free.
	if err := p.sem.Acquire(ctx, 1); err != nil {
		// ctx was canceled while waiting — propagate as-is.
		return false, err
	}
	defer p.sem.Release(1)

	return argon2Verify(plaintext, hash), nil
}
