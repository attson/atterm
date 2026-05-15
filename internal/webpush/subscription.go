package webpush

import "sync"

// maxSubsPerToken caps how many endpoints a single relay token may register.
// Beyond this, Add silently drops further endpoints to keep client retries
// idempotent.
const maxSubsPerToken = 16

// Subscription is one browser's push endpoint. JSON shape is the same one
// the Browser Push API hands to the page (endpoint + keys).
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	CreatedAt int64 `json:"created_at"`
}

type subStore struct {
	mu   sync.Mutex
	byID map[string][]Subscription // tokenHash -> subs
}

func newSubStore() *subStore {
	return &subStore{byID: make(map[string][]Subscription)}
}

// Add registers a subscription under the given tokenHash. Same-endpoint
// re-adds overwrite the existing entry (refreshing CreatedAt). Returns nil
// even when the cap is hit (drops silently).
func (s *subStore) Add(tokenHash string, sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.byID[tokenHash]
	for i, existing := range subs {
		if existing.Endpoint == sub.Endpoint {
			subs[i] = sub
			s.byID[tokenHash] = subs
			return nil
		}
	}
	if len(subs) >= maxSubsPerToken {
		return nil
	}
	s.byID[tokenHash] = append(subs, sub)
	return nil
}

// Remove deletes the subscription for the given tokenHash and endpoint.
// Returns true when something was removed.
func (s *subStore) Remove(tokenHash, endpoint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, ok := s.byID[tokenHash]
	if !ok {
		return false
	}
	for i, existing := range subs {
		if existing.Endpoint == endpoint {
			s.byID[tokenHash] = append(subs[:i], subs[i+1:]...)
			if len(s.byID[tokenHash]) == 0 {
				delete(s.byID, tokenHash)
			}
			return true
		}
	}
	return false
}

// ByToken returns a copy of the slice for the given tokenHash.
func (s *subStore) ByToken(tokenHash string) []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.byID[tokenHash]
	if subs == nil {
		return nil
	}
	out := make([]Subscription, len(subs))
	copy(out, subs)
	return out
}

// snapshot returns a deep copy of the entire registry (for persistence).
func (s *subStore) snapshot() map[string][]Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]Subscription, len(s.byID))
	for k, v := range s.byID {
		copied := make([]Subscription, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// load replaces the registry contents (used during Open).
func (s *subStore) load(m map[string][]Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string][]Subscription, len(m))
	for k, v := range m {
		copied := make([]Subscription, len(v))
		copy(copied, v)
		s.byID[k] = copied
	}
}
