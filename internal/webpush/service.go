package webpush

import (
	"log"
	"net/http"
	"sync"
)

// HTTPClient mirrors webpush-go's HTTPClient interface (single Do method)
// so tests outside the package can satisfy it without importing webpush-go.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// InjectTransportForTesting replaces the transport with one driven by the
// given HTTPClient. Test-only; production callers should never invoke this.
func InjectTransportForTesting(s *Service, hc HTTPClient) {
	s.tr = newTransport(s.vapidPriv, s.vapidPub, s.subject, hc)
}

// Service is the public face of the webpush package. One per relay process.
type Service struct {
	subject string
	dir     string

	mu        sync.Mutex
	vapidPriv string
	vapidPub  string

	subStore *subStore
	tr       *transport
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
func (s *Service) AddSubscription(userID string, sub Subscription) error {
	if err := s.subStore.Add(userID, sub); err != nil {
		return err
	}
	s.persistBestEffort()
	return nil
}

// RemoveSubscription deregisters an endpoint and persists state.
func (s *Service) RemoveSubscription(userID, endpoint string) error {
	s.subStore.Remove(userID, endpoint)
	s.persistBestEffort()
	return nil
}

// SubscriptionsForUser is a test-only helper. Returns subscriptions for the
// given user ID without exposing internal types in production callers.
func (s *Service) SubscriptionsForUser(userID string) []Subscription {
	return s.subStore.ByUser(userID)
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
