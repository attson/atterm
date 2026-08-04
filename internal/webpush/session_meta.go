package webpush

import "github.com/attson/atterm/internal/proto"

// SessionLabel is the human-facing string used in push-notification bodies
// for a given session. Prefers the currently-running foreground command,
// falls back to the tab title, then the shell name, then a generic
// placeholder. Kept next to the dispatch code because notification framing
// is the whole reason it exists.
func SessionLabel(info proto.SessionInfo) string {
	if info.CurrentCommand != "" {
		return info.CurrentCommand
	}
	if info.Title != "" {
		return info.Title
	}
	if info.Command != "" {
		return info.Command
	}
	return "session"
}

// TaskNotificationKey returns a monotonic-ish dedup key that changes
// whenever a "task started" event should re-fire a notification.
// Uses CommandStartedAt when set (the command boundary), else LastOutputAt
// (approximate activity time). Falls back to 1 so a fresh session with no
// activity still has a non-zero key.
func TaskNotificationKey(info proto.SessionInfo) int64 {
	if info.CommandStartedAt != 0 {
		return info.CommandStartedAt
	}
	if info.LastOutputAt != 0 {
		return info.LastOutputAt
	}
	return 1
}
