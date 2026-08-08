package main

import (
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const notificationClickEvent = "notification:click"

func (a *App) handleNotificationResponse(result wailsruntime.NotificationResult) {
	if result.Error != nil {
		logWarn("notify", "notification response: %v", result.Error)
		return
	}
	if a == nil || a.ctx == nil || a.eventsEmitter == nil {
		return
	}

	info := result.Response.UserInfo
	sessionID, _ := info["session_id"].(string)
	if sessionID == "" {
		sessionID, _ = info["sessionId"].(string)
	}
	if sessionID == "" {
		return
	}

	payload := map[string]interface{}{
		"id":                result.Response.ID,
		"action_identifier": result.Response.ActionIdentifier,
		"session_id":        sessionID,
	}
	if kind, ok := info["kind"].(string); ok && kind != "" {
		payload["kind"] = kind
	}
	a.eventsEmitter(a.ctx, notificationClickEvent, payload)
}
