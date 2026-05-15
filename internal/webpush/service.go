package webpush

import (
	"log"
	"sync"

	"github.com/google/uuid"
)

// Service is the public face of the webpush package. One per relay process.
type Service struct {
	subject string
	dir     string

	mu        sync.Mutex
	vapidPriv string
	vapidPub  string

	subStore *subStore
	tr       *transport

	resolverMu sync.RWMutex
	resolver   func(uuid.UUID) []string
}

// Open initializes the service. Recoverable conditions (missing file,
// corrupt state, unwritable dir) are downgraded to in-memory mode + a
// one-time WARN log. A non-nil error is returned only when even the
// in-memory fallback cannot be constructed (e.g., crypto generation fails).
func Open(dir, vapidSubject string) (*Service, error) {
	if vapidSubject == "" {
		vapidSubject = "mailto:noreply@atterm.local"
	}
	svc := &Service{
		subject:  vapidSubject,
		dir:      dir,
		subStore: newSubStore(),
	}
	if dir == "" {
		priv, pub, err := generateVAPIDKeypair()
		if err != nil {
			return nil, err
		}
		svc.vapidPriv = priv
		svc.vapidPub = pub
		log.Printf("webpush: running in-memory (no config dir); subscriptions will be lost on restart")
	} else {
		state, err := loadOrInitState(dir)
		if err != nil {
			// Fall back to in-memory.
			log.Printf("webpush: persistence unavailable (%v); running in-memory", err)
			priv, pub, genErr := generateVAPIDKeypair()
			if genErr != nil {
				return nil, genErr
			}
			svc.vapidPriv = priv
			svc.vapidPub = pub
			svc.dir = ""
		} else {
			svc.vapidPriv = state.PrivateKey
			svc.vapidPub = state.PublicKey
			svc.subStore.load(state.Subscriptions)
		}
	}
	svc.tr = newTransport(svc.vapidPriv, svc.vapidPub, svc.subject, nil)
	return svc, nil
}

// PublicKey returns the VAPID public key as a base64url string for the
// browser's applicationServerKey.
func (s *Service) PublicKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vapidPub
}

// AddSubscription registers a subscription and persists state.
func (s *Service) AddSubscription(tokenHash string, sub Subscription) error {
	if err := s.subStore.Add(tokenHash, sub); err != nil {
		return err
	}
	s.persistBestEffort()
	return nil
}

// RemoveSubscription deregisters an endpoint and persists state.
func (s *Service) RemoveSubscription(tokenHash, endpoint string) error {
	s.subStore.Remove(tokenHash, endpoint)
	s.persistBestEffort()
	return nil
}

// SetSessionResolver registers the function that maps a session id to the
// token-hashes allowed to view it. The resolver is called from the dispatch
// goroutine; implementations must be cheap.
func (s *Service) SetSessionResolver(f func(uuid.UUID) []string) {
	s.resolverMu.Lock()
	s.resolver = f
	s.resolverMu.Unlock()
}

func (s *Service) lookupResolver() func(uuid.UUID) []string {
	s.resolverMu.RLock()
	defer s.resolverMu.RUnlock()
	return s.resolver
}

// SubscriptionsForToken is a test-only helper. Returns subscriptions for the
// given token hash without exposing internal types in production callers.
func (s *Service) SubscriptionsForToken(tokenHash string) []Subscription {
	return s.subStore.ByToken(tokenHash)
}

func (s *Service) persistBestEffort() {
	if s.dir == "" {
		return
	}
	state := persistedState{
		PrivateKey:    s.vapidPriv,
		PublicKey:     s.vapidPub,
		Subscriptions: s.subStore.snapshot(),
	}
	if err := saveState(s.dir, state); err != nil {
		log.Printf("webpush: persistBestEffort: %v", err)
	}
}
