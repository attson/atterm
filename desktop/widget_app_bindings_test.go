package main

import (
	"context"
	"encoding/json"
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

func TestHandleWidgetMinimizePersistsPreference(t *testing.T) {
	app := &App{cfgStore: newTestConfigStore(t)}
	var event widgetEvent
	if err := json.Unmarshal([]byte(`{"type":"minimize","minimized":true}`), &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}

	app.handleWidgetEvent(event)

	blob, err := json.Marshal(app.cfgStore.Get().Plugins.Widget)
	if err != nil {
		t.Fatalf("encode widget config: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(blob, &persisted); err != nil {
		t.Fatalf("decode widget config: %v", err)
	}
	if minimized, ok := persisted["minimized"].(bool); !ok || !minimized {
		t.Fatalf("minimized preference was not persisted: %s", blob)
	}
}
