package webpush

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/google/uuid"
)

const (
	maxLabelLen = 256
	sendTimeout = 10 * time.Second
)

const (
	NotificationCommandCompleted   = "command_completed"
	NotificationCommandFailed      = "command_failed"
	NotificationCommandFinished    = "command_finished"
	NotificationWaitingInput       = "waiting_input"
	NotificationIdleTimeout        = "idle_timeout"
	NotificationUplinkDisconnected = "uplink_disconnected"
)

// CommandFinished is the input to a command-finished push.
type CommandFinished struct {
	SessionID        uuid.UUID
	HostID           uuid.UUID
	ExitCode         int
	ElapsedMS        int
	Label            string
	RemotePermission string
	// SealedBody is the agent-composed AEAD envelope (M6-foundation)
	// that the service worker can decrypt with the user's account_key
	// to render rich content the relay was not able to read. When
	// non-empty the relay includes a base64 copy in the push payload
	// under "sealedBody"; when empty the SW falls back to the
	// existing title/body strings.
	SealedBody []byte
}

// SessionNotification is the input to task-state push notifications that are
// not command-finished events.
type SessionNotification struct {
	SessionID        uuid.UUID
	HostID           uuid.UUID
	NotificationType string
	Label            string
	RemotePermission string
	IdleForSeconds   int
}

// DispatchCommandFinished fans the event out to all subscriptions registered
// under ownerUserID. Always returns immediately; fanout runs in goroutines.
// Failures with 404/410 status prune the subscription; other errors are
// logged and the subscription is kept.
//
// If ownerUserID is empty (legacy / dev-mode paths where no user account is
// associated), the function is a no-op: no subscribers are matched and no
// pushes are sent.
func (s *Service) DispatchCommandFinished(ownerUserID string, ev CommandFinished) {
	if ownerUserID == "" {
		return
	}
	if len(ev.Label) > maxLabelLen {
		ev.Label = ev.Label[:maxLabelLen]
	}
	// dispatch intentionally reads subscriptions from the DB on each send for cross-instance freshness — do not reintroduce an in-memory cache
	subs := s.SubscriptionsForUser(ownerUserID)
	if len(subs) == 0 {
		return
	}
	body := payloadJSON(ev)
	for _, sub := range subs {
		go s.sendOne(ownerUserID, sub, body)
	}
}

// DispatchSessionNotification fans a task-state notification out to all
// subscriptions registered under ownerUserID.
func (s *Service) DispatchSessionNotification(ownerUserID string, ev SessionNotification) {
	if ownerUserID == "" {
		return
	}
	if len(ev.Label) > maxLabelLen {
		ev.Label = ev.Label[:maxLabelLen]
	}
	subs := s.SubscriptionsForUser(ownerUserID)
	if len(subs) == 0 {
		return
	}
	body := sessionNotificationPayloadJSON(ev)
	for _, sub := range subs {
		go s.sendOne(ownerUserID, sub, body)
	}
}

// SendTest dispatches a "test" notification to every subscription under
// userID. Returns the number of pushes dispatched (not delivered).
func (s *Service) SendTest(userID string) int {
	subs := s.SubscriptionsForUser(userID)
	body, _ := json.Marshal(map[string]interface{}{
		"title": "AT Term test",
		"body":  "It works.",
	})
	for _, sub := range subs {
		go s.sendOne(userID, sub, body)
	}
	return len(subs)
}

func (s *Service) sendOne(userID string, sub Subscription, body []byte) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("webpush", "panic in send: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	resp, err := s.tr.Send(ctx, sub, body)
	if err != nil {
		logging.Warn("webpush", "send err endpoint=%s: %v", sub.Endpoint, err)
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		logging.Info("webpush", "endpoint %s gone (status %d); pruning", sub.Endpoint, resp.StatusCode)
		if err := s.store.RemoveWebPushSubscription(ctx, userID, sub.Endpoint); err != nil {
			logging.Warn("webpush", "prune subscription: %v", err)
		}
	default:
		logging.Warn("webpush", "send non-2xx endpoint=%s status=%d", sub.Endpoint, resp.StatusCode)
	}
}

// payloadJSON encodes the notification payload that the browser SW will
// see. Exposed for tests.
//
// When the agent attaches a SealedBody (M6-final), the relay knows only
// that a command finished — not which command, not its exit code, not
// how long it ran. The push payload therefore carries a generic title
// / body / notificationType, with no exitCode or elapsedMs in data,
// plus the opaque envelope for the SW to decrypt locally.
func payloadJSON(ev CommandFinished) []byte {
	sealed := len(ev.SealedBody) > 0
	notificationType := pickNotificationType(ev, sealed)
	data := map[string]interface{}{
		"notificationType": notificationType,
		"clickUrl":         clickURL(ev.SessionID, notificationType, ev.RemotePermission),
		"sessionId":        ev.SessionID.String(),
		"hostId":           ev.HostID.String(),
	}
	if !sealed {
		data["exitCode"] = ev.ExitCode
		data["elapsedMs"] = ev.ElapsedMS
	}
	if ev.RemotePermission != "" {
		data["remotePermission"] = ev.RemotePermission
	}
	payload := map[string]interface{}{
		"title": pushTitle(ev, sealed),
		"body":  pushBody(ev, sealed),
		"tag":   ev.SessionID.String(),
		"data":  data,
	}
	if sealed {
		payload["sealedBody"] = base64.StdEncoding.EncodeToString(ev.SealedBody)
	}
	b, _ := json.Marshal(payload)
	return b
}

func pickNotificationType(ev CommandFinished, sealed bool) string {
	if sealed {
		return NotificationCommandFinished
	}
	if ev.ExitCode != 0 {
		return NotificationCommandFailed
	}
	return NotificationCommandCompleted
}

func pushTitle(ev CommandFinished, sealed bool) string {
	if sealed {
		return "AT Term"
	}
	label := ev.Label
	if label == "" {
		label = "session"
	}
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen]
	}
	return fmt.Sprintf("AT Term · %s", label)
}

func pushBody(ev CommandFinished, sealed bool) string {
	if sealed {
		return "Session command finished"
	}
	return fmt.Sprintf("Command finished · exit %d · %s", ev.ExitCode, formatElapsed(ev.ElapsedMS))
}

func sessionNotificationPayloadJSON(ev SessionNotification) []byte {
	label := ev.Label
	if label == "" {
		label = "session"
	}
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen]
	}
	notificationType := ev.NotificationType
	if notificationType == "" {
		notificationType = NotificationWaitingInput
	}
	data := map[string]interface{}{
		"notificationType": notificationType,
		"clickUrl":         clickURL(ev.SessionID, notificationType, ev.RemotePermission),
		"sessionId":        ev.SessionID.String(),
		"hostId":           ev.HostID.String(),
	}
	if ev.RemotePermission != "" {
		data["remotePermission"] = ev.RemotePermission
	}
	if ev.IdleForSeconds > 0 {
		data["idleForSeconds"] = ev.IdleForSeconds
	}
	payload := map[string]interface{}{
		"title": fmt.Sprintf("AT Term · %s", label),
		"body":  sessionNotificationBody(notificationType, ev.IdleForSeconds),
		"tag":   ev.SessionID.String(),
		"data":  data,
	}
	b, _ := json.Marshal(payload)
	return b
}

func clickURL(sessionID uuid.UUID, notificationType string, remotePermission string) string {
	target := fmt.Sprintf("/#/s/%s?notification=%s", sessionID, notificationType)
	if notificationType == NotificationWaitingInput {
		target += "&focus=input"
	}
	if remotePermission != "" {
		target += "&permission=" + remotePermission
	}
	return target
}

func sessionNotificationBody(notificationType string, idleForSeconds int) string {
	switch notificationType {
	case NotificationWaitingInput:
		return "Session is waiting for input"
	case NotificationIdleTimeout:
		if idleForSeconds > 0 {
			return fmt.Sprintf("No output for %s", formatElapsed(idleForSeconds*1000))
		}
		return "No output recently"
	case NotificationUplinkDisconnected:
		return "Host disconnected"
	default:
		return "Session needs attention"
	}
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
