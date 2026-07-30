package relay

import "sync"

type prefsNotifier struct {
	mu   sync.Mutex
	subs map[string]map[*prefsSubscription]struct{}
}

type prefsSubscription struct {
	hub    *prefsNotifier
	userID string
	ch     chan struct{}
	once   sync.Once
}

func newPrefsNotifier() *prefsNotifier {
	return &prefsNotifier{subs: make(map[string]map[*prefsSubscription]struct{})}
}

func (n *prefsNotifier) Subscribe(userID string) *prefsSubscription {
	sub := &prefsSubscription{
		hub:    n,
		userID: userID,
		ch:     make(chan struct{}, 1),
	}
	if userID == "" {
		return sub
	}
	n.mu.Lock()
	set := n.subs[userID]
	if set == nil {
		set = make(map[*prefsSubscription]struct{})
		n.subs[userID] = set
	}
	set[sub] = struct{}{}
	n.mu.Unlock()
	return sub
}

func (n *prefsNotifier) Notify(userID string) {
	if userID == "" {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for sub := range n.subs[userID] {
		select {
		case sub.ch <- struct{}{}:
		default:
		}
	}
}

func (s *prefsSubscription) C() <-chan struct{} {
	return s.ch
}

func (s *prefsSubscription) Close() {
	s.once.Do(func() {
		if s.userID == "" {
			close(s.ch)
			return
		}
		s.hub.mu.Lock()
		if set := s.hub.subs[s.userID]; set != nil {
			delete(set, s)
			if len(set) == 0 {
				delete(s.hub.subs, s.userID)
			}
		}
		close(s.ch)
		s.hub.mu.Unlock()
	})
}
