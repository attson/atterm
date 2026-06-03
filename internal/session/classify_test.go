package session

import "testing"

func TestClassifyCommand(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", "shell"},
		{"whitespace", "   ", "shell"},
		{"plain bash", "bash", "shell"},
		{"plain zsh", "zsh", "shell"},
		{"claude alone", "claude", "ai"},
		{"claude with path", "/usr/local/bin/claude", "ai"},
		{"claude with flags", "claude --help", "ai"},
		{"codex alone", "codex", "ai"},
		{"gemini chat", "gemini chat", "ai"},
		{"aider alone", "aider", "ai"},
		{"sudo claude", "sudo claude", "ai"},
		{"time go test", "time go test ./...", "test"},
		{"env npm test", "DEBUG=1 npm test", "test"},
		{"yarn test", "yarn test", "test"},
		{"pnpm test", "pnpm test --watch", "test"},
		{"cargo test", "cargo test --release", "test"},
		{"docker build", "docker build .", "build"},
		{"docker compose up", "docker compose up", "build"},
		{"docker-compose hyphen", "docker-compose up -d", "build"},
		{"docker ps not build", "docker ps", "shell"},
		{"kubectl", "kubectl get pods", "deploy"},
		{"terraform", "terraform plan", "deploy"},
		{"npx claude limitation", "npx claude", "shell"},
		{"go run not test", "go run ./cmd/foo", "shell"},
		{"nested env wrappers", "env DEBUG=1 sudo claude", "ai"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyCommand(tc.in); got != tc.want {
				t.Fatalf("ClassifyCommand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
