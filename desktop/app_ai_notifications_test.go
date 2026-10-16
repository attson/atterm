package main

import (
	"testing"

	"github.com/attson/atterm/desktop/feishu"
)

func TestAINotificationsOnly_GetSetRoundTrip(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetAINotificationsOnly(); !got {
		t.Fatalf("GetAINotificationsOnly() = false; want true (default)")
	}
	if err := a.SetAINotificationsOnly(false); err != nil {
		t.Fatalf("SetAINotificationsOnly(false): %v", err)
	}
	if got := a.GetAINotificationsOnly(); got {
		t.Fatalf("after Set(false), Get() = true; want false")
	}
	if err := a.SetAINotificationsOnly(true); err != nil {
		t.Fatalf("SetAINotificationsOnly(true): %v", err)
	}
	if got := a.GetAINotificationsOnly(); !got {
		t.Fatalf("after Set(true), Get() = false; want true")
	}
}

func TestFeishuCardNotifications_GetSetRoundTrip(t *testing.T) {
	a := newAppWithTempCfg(t)
	if got := a.GetFeishuCardNotificationSettings(); !got.CommandSuccess || !got.CommandFailure || !got.CommandSealed || !got.WaitingInput || !got.AskQuestion {
		t.Fatalf("defaults = %+v, want every card enabled", got)
	}

	if err := a.SetFeishuCardNotificationEnabled(string(feishu.CardNotificationWaitingInput), false); err != nil {
		t.Fatalf("disable waiting_input: %v", err)
	}
	got := a.GetFeishuCardNotificationSettings()
	if got.WaitingInput {
		t.Fatal("waiting_input remained enabled")
	}
	if !got.CommandSuccess || !got.CommandFailure || !got.CommandSealed || !got.AskQuestion {
		t.Fatalf("changing waiting_input disturbed another switch: %+v", got)
	}

	if err := a.SetFeishuCardNotificationEnabled("unknown", false); err == nil {
		t.Fatal("unknown card type should be rejected")
	}
}
