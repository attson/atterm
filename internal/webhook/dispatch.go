package webhook

import (
	"context"
	"log"
)

// DispatchCommandFinished fans the event out to all of the owner's webhooks.
// Returns immediately; each POST runs in its own goroutine. Store errors and
// non-2xx POSTs are logged and otherwise ignored (best-effort, like web-push).
func (s *Service) DispatchCommandFinished(ownerUserID string, ev CommandFinished) {
	hooks, err := s.store.ListWebhooks(context.Background(), ownerUserID)
	if err != nil {
		log.Printf("webhook: list for user=%s failed: %v", ownerUserID, err)
		return
	}
	for _, h := range hooks {
		h := h
		body := renderForFormat(h.Format, ev)
		go func() {
			if err := post(s.client, h.URL, body); err != nil {
				log.Printf("webhook: POST id=%s failed: %v", h.ID, err)
			}
		}()
	}
}
