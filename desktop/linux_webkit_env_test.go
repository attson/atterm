package main

import "testing"

func TestApplyLinuxWebKitEnvironmentDisablesDMABufRenderer(t *testing.T) {
	env := map[string]string{}

	applyLinuxWebKitEnvironment(
		func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		func(key, value string) error { env[key] = value; return nil },
	)

	for _, key := range linuxWebKitDMABufEnvKeys {
		if got := env[key]; got != "1" {
			t.Fatalf("%s = %q, want 1", key, got)
		}
	}
}

func TestApplyLinuxWebKitEnvironmentPreservesUserOverrides(t *testing.T) {
	env := map[string]string{}
	for _, key := range linuxWebKitDMABufEnvKeys {
		env[key] = "0"
	}

	applyLinuxWebKitEnvironment(
		func(key string) (string, bool) { v, ok := env[key]; return v, ok },
		func(key, value string) error { env[key] = value; return nil },
	)

	for _, key := range linuxWebKitDMABufEnvKeys {
		if got := env[key]; got != "0" {
			t.Fatalf("%s = %q, want existing override 0", key, got)
		}
	}
}
