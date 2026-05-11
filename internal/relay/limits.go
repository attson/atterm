package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	defaultRateLimitPerMinute = 600
	defaultMaxConnections     = 64
)

type fixedWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string]windowHit
}

type windowHit struct {
	start time.Time
	count int
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	if limit <= 0 {
		return nil
	}
	return &fixedWindowLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   make(map[string]windowHit),
	}
}

func (l *fixedWindowLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.hits[key]
	if h.start.IsZero() || now.Sub(h.start) >= l.window {
		l.hits[key] = windowHit{start: now, count: 1}
		return true
	}
	if h.count >= l.limit {
		return false
	}
	h.count++
	l.hits[key] = h
	return true
}

func (l *fixedWindowLimiter) setLimit(limit int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
	l.hits = make(map[string]windowHit)
}

type connectionLimiter struct {
	mu    sync.Mutex
	limit int
	open  map[string]int
}

func newConnectionLimiter(limit int) *connectionLimiter {
	if limit <= 0 {
		return nil
	}
	return &connectionLimiter{limit: limit, open: make(map[string]int)}
}

func (l *connectionLimiter) acquire(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[key] >= l.limit {
		return false
	}
	l.open[key]++
	return true
}

func (l *connectionLimiter) release(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[key] <= 1 {
		delete(l.open, key)
		return
	}
	l.open[key]--
}

func (l *connectionLimiter) setLimit(limit int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
}

func requestLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	token := tokenFromRequest(r)
	if token == "" {
		return host
	}
	sum := sha256.Sum256([]byte(token))
	return host + "\x00" + base64.RawURLEncoding.EncodeToString(sum[:])
}
