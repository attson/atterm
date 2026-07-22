package main

import (
	"context"
	"testing"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestHandleNotificationResponseEmitsSessionClick(t *testing.T) {
	app := &App{ctx: context.Background()}
	var gotName string
	var gotData []interface{}
	app.eventsEmitter = func(_ context.Context, name string, data ...interface{}) {
		gotName = name
		gotData = data
	}

	app.handleNotificationResponse(wailsruntime.NotificationResult{
		Response: wailsruntime.NotificationResponse{
			ID:               "n1",
			ActionIdentifier: "DEFAULT_ACTION",
			UserInfo: map[string]interface{}{
				"session_id": "sid-1",
				"kind":       "bell",
			},
		},
	})

	if gotName != "notification:click" {
		t.Fatalf("event name = %q, want notification:click", gotName)
	}
	if len(gotData) != 1 {
		t.Fatalf("payload count = %d, want 1", len(gotData))
	}
	payload, ok := gotData[0].(map[string]interface{})
	if !ok {
		t.Fatalf("payload type = %T, want map[string]interface{}", gotData[0])
	}
	if got := payload["session_id"]; got != "sid-1" {
		t.Fatalf("session_id = %v, want sid-1", got)
	}
	if got := payload["kind"]; got != "bell" {
		t.Fatalf("kind = %v, want bell", got)
	}
	if got := payload["id"]; got != "n1" {
		t.Fatalf("id = %v, want n1", got)
	}
}

func TestHandleNotificationResponseSkipsWithoutSessionID(t *testing.T) {
	app := &App{ctx: context.Background()}
	called := false
	app.eventsEmitter = func(_ context.Context, _ string, _ ...interface{}) {
		called = true
	}

	app.handleNotificationResponse(wailsruntime.NotificationResult{
		Response: wailsruntime.NotificationResponse{
			ID:               "n1",
			ActionIdentifier: "DEFAULT_ACTION",
			UserInfo:         map[string]interface{}{"kind": "bell"},
		},
	})

	if called {
		t.Fatal("eventsEmitter was called without a session_id")
	}
}
