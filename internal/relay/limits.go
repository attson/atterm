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

func requestIPLimitKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// LimitRegistry holds per-feature rate limiters for auth endpoints (SEC-5).
// All limiters use fixed windows. Create a new registry per server instance
// (or per test) to avoid bucket leakage.
type LimitRegistry struct {
	loginFail   *fixedWindowLimiter // 10 / 5min keyed by "ip\x00sha256hex(email)"
	signup      *fixedWindowLimiter // 5 / hour keyed by IP
	inviteFail  *fixedWindowLimiter // 10 / hour keyed by IP
	pairCreate  *fixedWindowLimiter // 10 / minute keyed by userID
	pairConsume *fixedWindowLimiter // 10 / minute keyed by IP
}

// NewLimitRegistry returns a LimitRegistry with the SEC-5 rate limits configured.
func NewLimitRegistry() *LimitRegistry {
	return &LimitRegistry{
		loginFail:   newFixedWindowLimiter(10, 5*time.Minute),
		signup:      newFixedWindowLimiter(5, time.Hour),
		inviteFail:  newFixedWindowLimiter(10, time.Hour),
		pairCreate:  newFixedWindowLimiter(10, time.Minute),
		pairConsume: newFixedWindowLimiter(10, time.Minute),
	}
}

// newLimitRegistryForTest returns a LimitRegistry with the given individual
// limits set. Pass limit <= 0 to disable a particular bucket. Used in tests
// to isolate one limiter without another interfering.
func newLimitRegistryForTest(loginFailLimit, signupLimit, inviteFailLimit int) *LimitRegistry {
	return &LimitRegistry{
		loginFail:   newFixedWindowLimiter(loginFailLimit, 5*time.Minute),
		signup:      newFixedWindowLimiter(signupLimit, time.Hour),
		inviteFail:  newFixedWindowLimiter(inviteFailLimit, time.Hour),
		pairCreate:  newFixedWindowLimiter(1000, time.Minute),
		pairConsume: newFixedWindowLimiter(1000, time.Minute),
	}
}

// AllowLoginFailure returns true if the (ip, emailHash) pair has not exceeded
// the brute-force login limit (10 failures per 5-minute window).
// emailHash must be the hex-encoded sha256 of the lowercased email.
func (r *LimitRegistry) AllowLoginFailure(ip, emailHash string) bool {
	return r.loginFail.allow(ip + "\x00" + emailHash)
}

// AllowSignup returns true if the IP has not exceeded the signup rate limit
// (5 signups per hour).
func (r *LimitRegistry) AllowSignup(ip string) bool {
	return r.signup.allow(ip)
}

// AllowInviteFail returns true if the IP has not exceeded the invite-failure
// rate limit (10 failures per hour).
func (r *LimitRegistry) AllowInviteFail(ip string) bool {
	return r.inviteFail.allow(ip)
}

// AllowPairCreate returns true if userID has not exceeded the pairing-token
// mint rate limit (10 / minute).
func (r *LimitRegistry) AllowPairCreate(userID string) bool {
	return r.pairCreate.allow(userID)
}

// AllowPairConsume returns true if ip has not exceeded the consume rate limit
// (10 / minute). Defense in depth — the 256-bit token entropy alone already
// makes brute force impractical.
func (r *LimitRegistry) AllowPairConsume(ip string) bool {
	return r.pairConsume.allow(ip)
}

// sha256Hex returns the lowercase hex-encoded SHA-256 digest of s.
// Used to construct per-email brute-force limit keys without storing the email.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(sum)*2)
	for i, b := range sum {
		dst[i*2] = hextable[b>>4]
		dst[i*2+1] = hextable[b&0x0f]
	}
	return string(dst)
}
