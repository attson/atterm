package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerEntrypointDefaultsConfigDir(t *testing.T) {
	body, err := os.ReadFile("docker-entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)

	if !strings.Contains(script, `export ATTERM_RELAY_CONFIG_DIR="/etc/atterm"`) {
		t.Fatalf("docker-entrypoint.sh must export ATTERM_RELAY_CONFIG_DIR=/etc/atterm when unset")
	}
	if !strings.Contains(script, `mkdir -p "$dir"`) {
		t.Fatalf("docker-entrypoint.sh must create the persistence directory before dropping privileges")
	}
}

func TestRelayDockerfilePreparesDefaultConfigDir(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile.relay"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(body)

	if !strings.Contains(dockerfile, "mkdir -p /etc/atterm") ||
		!strings.Contains(dockerfile, "chown atterm:atterm /etc/atterm") {
		t.Fatalf("Dockerfile.relay must create /etc/atterm writable by the atterm user")
	}
}
