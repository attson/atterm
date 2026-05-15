package webpush

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

const (
	maxLabelLen = 256
	sendTimeout = 10 * time.Second
)

// CommandFinished is the input to a command-finished push.
type CommandFinished struct {
	SessionID uuid.UUID
	HostID    uuid.UUID
	ExitCode  int
	ElapsedMS int
	Label     string
}

// DispatchCommandFinished fans the event out to all subscriptions of every
// token-hash returned by the session resolver. Always returns immediately;
// fanout runs in goroutines. Failures with 404/410 status prune the
// subscription; other errors are logged and the subscription is kept.
func (s *Service) DispatchCommandFinished(ev CommandFinished) {
	resolver := s.lookupResolver()
	if resolver == nil {
		return
	}
	if len(ev.Label) > maxLabelLen {
		ev.Label = ev.Label[:maxLabelLen]
	}
	tokens := resolver(ev.SessionID)
	if len(tokens) == 0 {
		return
	}
	body := payloadJSON(ev)
	for _, tokenHash := range tokens {
		subs := s.subStore.ByToken(tokenHash)
		for _, sub := range subs {
			go s.sendOne(tokenHash, sub, body)
		}
	}
}

// SendTest dispatches a "test" notification to every subscription under
// tokenHash. Returns the number of pushes dispatched (not delivered).
func (s *Service) SendTest(tokenHash string) int {
	subs := s.subStore.ByToken(tokenHash)
	body, _ := json.Marshal(map[string]interface{}{
		"title": "AT Term test",
		"body":  "It works.",
	})
	for _, sub := range subs {
		go s.sendOne(tokenHash, sub, body)
	}
	return len(subs)
}

func (s *Service) sendOne(tokenHash string, sub Subscription, body []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("webpush: panic in send: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := s.tr.Send(ctx, sub, body)
	if err != nil {
		log.Printf("webpush: send err endpoint=%s: %v", sub.Endpoint, err)
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		log.Printf("webpush: endpoint %s gone (status %d); pruning", sub.Endpoint, resp.StatusCode)
		s.subStore.Remove(tokenHash, sub.Endpoint)
		s.persistBestEffort()
	default:
		log.Printf("webpush: send non-2xx endpoint=%s status=%d", sub.Endpoint, resp.StatusCode)
	}
}

// payloadJSON encodes the notification payload that the browser SW will
// see. Exposed for tests.
func payloadJSON(ev CommandFinished) []byte {
	label := ev.Label
	if label == "" {
		label = "session"
	}
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen]
	}
	payload := map[string]interface{}{
		"title": fmt.Sprintf("AT Term · %s", label),
		"body":  fmt.Sprintf("Command finished · exit %d · %s", ev.ExitCode, formatElapsed(ev.ElapsedMS)),
		"tag":   ev.SessionID.String(),
		"data": map[string]interface{}{
			"exitCode":  ev.ExitCode,
			"elapsedMs": ev.ElapsedMS,
			"sessionId": ev.SessionID.String(),
			"hostId":    ev.HostID.String(),
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func formatElapsed(ms int) string {
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm%ds", sec/60, sec%60)
}
