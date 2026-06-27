package main

import "testing"

func TestFeishuModePrefOrDefault_EmptyReturnsAuto(t *testing.T) {
	c := appConfig{}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, "auto")
	}
}

func TestFeishuModePrefOrDefault_KnownValuesPassthrough(t *testing.T) {
	for _, v := range []string{"auto", "local", "relay"} {
		c := appConfig{FeishuModePref: v}
		if got := c.FeishuModePrefOrDefault(); got != v {
			t.Errorf("FeishuModePrefOrDefault() = %q; want %q", got, v)
		}
	}
}

func TestFeishuModePrefOrDefault_UnknownFallsBackToAuto(t *testing.T) {
	c := appConfig{FeishuModePref: "garbage"}
	if got := c.FeishuModePrefOrDefault(); got != "auto" {
		t.Errorf("FeishuModePrefOrDefault() = %q; want %q (defensive default)", got, "auto")
	}
}
