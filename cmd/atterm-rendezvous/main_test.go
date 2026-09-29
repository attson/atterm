package main

import (
	"strings"
	"testing"
)

func TestParseOptionsRequiresProductionOriginsAndTLSPosture(t *testing.T) {
	env := func(string) string { return "" }
	if _, err := parseOptions([]string{"--addr", ":8443"}, env); err == nil || !strings.Contains(err.Error(), "origins") {
		t.Fatalf("missing origins error=%v", err)
	}
	if _, err := parseOptions([]string{"--addr", ":8443", "--origins", "https://app.example"}, env); err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("missing TLS error=%v", err)
	}
	if _, err := parseOptions([]string{
		"--addr", "0.0.0.0:8443", "--origins", "https://app.example", "--behind-tls-proxy",
	}, env); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("public proxy backend error=%v", err)
	}
}

func TestParseOptionsAcceptsDirectTLSLoopbackProxyAndExplicitDevelopment(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "ATTERM_RENDEZVOUS_TLS_CERT":
			return "/run/tls/cert.pem"
		case "ATTERM_RENDEZVOUS_TLS_KEY":
			return "/run/tls/key.pem"
		default:
			return ""
		}
	}
	direct, err := parseOptions([]string{
		"--addr", ":8443", "--origins", "https://app.example,capacitor://localhost",
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	if !direct.useTLS || len(direct.origins) != 2 {
		t.Fatalf("direct options=%+v", direct)
	}

	proxy, err := parseOptions([]string{
		"--addr", "127.0.0.1:8081", "--origins", "https://app.example", "--behind-tls-proxy",
	}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if proxy.useTLS {
		t.Fatalf("proxy options=%+v", proxy)
	}

	dev, err := parseOptions([]string{"--addr", "127.0.0.1:8081", "--dev-insecure"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if dev.useTLS || len(dev.origins) != 0 {
		t.Fatalf("dev options=%+v", dev)
	}
}

func TestParseOptionsRejectsInvalidLimitsAndOrigin(t *testing.T) {
	env := func(string) string { return "" }
	base := []string{"--dev-insecure", "--addr", "127.0.0.1:8081"}
	if _, err := parseOptions(append(base, "--max-connections", "-1"), env); err == nil {
		t.Fatal("negative connection limit accepted")
	}
	if _, err := parseOptions(append(base, "--max-mailbox-per-topic", "1048577"), env); err == nil || !strings.Contains(err.Error(), "hard maximum") {
		t.Fatalf("hard-limit error=%v", err)
	}
	if _, err := parseOptions([]string{
		"--addr", "127.0.0.1:8081", "--behind-tls-proxy", "--origins", "https://app.example/path",
	}, env); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("invalid origin error=%v", err)
	}
	if _, err := parseOptions(base, func(key string) string {
		if key == "ATTERM_RENDEZVOUS_MAX_CONNECTIONS" {
			return "many"
		}
		return ""
	}); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("invalid environment limit error=%v", err)
	}
}
