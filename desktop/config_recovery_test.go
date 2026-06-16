package main

import "testing"

func TestAppConfig_RecoveryDialogEnabledDefaultsTrue(t *testing.T) {
	c := appConfig{}
	if !c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("default should be true")
	}
}

func TestAppConfig_RecoveryDialogEnabledRespectsExplicitFalse(t *testing.T) {
	v := false
	c := appConfig{RecoveryDialogEnabled: &v}
	if c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("explicit false must stick")
	}
}

func TestAppConfig_RecoveryDialogEnabledRespectsExplicitTrue(t *testing.T) {
	v := true
	c := appConfig{RecoveryDialogEnabled: &v}
	if !c.RecoveryDialogEnabledOrDefault() {
		t.Fatal("explicit true must stick")
	}
}

func TestApp_GetSetRecoveryDialogEnabled(t *testing.T) {
	app := newRecoveryTestApp(t)
	// Default = true.
	if !app.GetRecoveryDialogEnabled() {
		t.Fatal("default GetRecoveryDialogEnabled should be true")
	}
	if err := app.SetRecoveryDialogEnabled(false); err != nil {
		t.Fatalf("Set false: %v", err)
	}
	if app.GetRecoveryDialogEnabled() {
		t.Fatal("after Set(false), Get should return false")
	}
	if err := app.SetRecoveryDialogEnabled(true); err != nil {
		t.Fatalf("Set true: %v", err)
	}
	if !app.GetRecoveryDialogEnabled() {
		t.Fatal("after Set(true), Get should return true")
	}
}
