package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseOptionsRequiresURLAndExplicitInsecureLoopback(t *testing.T) {
	env := func(string) string { return "" }
	if _, err := parseOptions(nil, env); err == nil || !strings.Contains(err.Error(), "url") {
		t.Fatalf("missing URL error=%v", err)
	}
	if _, err := parseOptions([]string{"--url", "http://127.0.0.1:8081"}, env); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("implicit plaintext error=%v", err)
	}
	got, err := parseOptions([]string{
		"--url", "http://127.0.0.1:8081", "--allow-insecure-loopback", "--timeout", "8s",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.url != "http://127.0.0.1:8081" || !got.allowInsecureLoopback || got.timeout != 8*time.Second {
		t.Fatalf("options=%+v", got)
	}
}

func TestParseOptionsReadsDeploymentEnvironment(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "ATTERM_RENDEZVOUS_TEST_URL":
			return "https://rendezvous.example"
		case "ATTERM_RENDEZVOUS_TEST_ORIGIN":
			return "https://app.example"
		default:
			return ""
		}
	}
	got, err := parseOptions(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if got.url != "https://rendezvous.example" || got.origin != "https://app.example" {
		t.Fatalf("options=%+v", got)
	}
}
