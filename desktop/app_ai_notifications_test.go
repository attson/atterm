package main

import "testing"

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
