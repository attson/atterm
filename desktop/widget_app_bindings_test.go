package main

import (
	"context"
	"testing"
)

func TestHandleWidgetActivateRaisesWindowAndEmitsDedicatedEvent(t *testing.T) {
	ctx := context.Background()
	activated := false
	var eventName string
	var eventPayload map[string]interface{}
	app := &App{
		ctx: ctx,
		windowActivator: func(got context.Context) {
			if got != ctx {
				t.Fatalf("activation context differs from app context")
			}
			activated = true
		},
		eventsEmitter: func(_ context.Context, name string, data ...interface{}) {
			eventName = name
			if len(data) == 1 {
				eventPayload, _ = data[0].(map[string]interface{})
			}
		},
	}

	app.handleWidgetEvent(widgetEvent{Type: "activate", SessionID: "session-123"})

	if !activated {
		t.Fatal("main window was not activated")
	}
	if eventName != "widget:activate" {
		t.Fatalf("event name = %q; want widget:activate", eventName)
	}
	if got, _ := eventPayload["session_id"].(string); got != "session-123" {
		t.Fatalf("session_id = %q; want session-123", got)
	}
}

func TestHandleWidgetActivateIgnoresEmptySessionID(t *testing.T) {
	activated := false
	emitted := false
	app := &App{
		ctx:             context.Background(),
		windowActivator: func(context.Context) { activated = true },
		eventsEmitter:   func(context.Context, string, ...interface{}) { emitted = true },
	}

	app.handleWidgetEvent(widgetEvent{Type: "activate"})

	if activated || emitted {
		t.Fatalf("empty session id activated=%v emitted=%v; want both false", activated, emitted)
	}
}
