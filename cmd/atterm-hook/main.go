// cmd/atterm-hook/main.go
//
// atterm-hook bridges claude-code's Notification hook to a localhost
// HTTP endpoint inside the atterm desktop process. It is deliberately
// trivial: any failure path exits 0 so the hook never wedges
// claude-code itself.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	maxStdin    = 64 * 1024
	httpTimeout = 1 * time.Second
)

type hookNotifyRequest struct {
	SessionID   string          `json:"session_id"`
	AgentKind   string          `json:"agent_kind"`
	HookInput   json.RawMessage `json:"hook_input"`
	HookVersion string          `json:"hook_version,omitempty"`
}

func main() {
	limited := io.LimitReader(os.Stdin, maxStdin+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: read stdin: %v\n", err)
		os.Exit(0)
	}
	if len(body) > maxStdin {
		fmt.Fprintf(os.Stderr, "atterm-hook: stdin too large (>%d), dropping\n", maxStdin)
		os.Exit(0)
	}
	if len(body) == 0 {
		os.Exit(0)
	}

	sessionID := os.Getenv("ATTERM_SESSION_ID")
	if sessionID == "" {
		os.Exit(0)
	}

	endpoint := resolveEndpoint()
	if endpoint == "" {
		os.Exit(0)
	}

	req := hookNotifyRequest{
		SessionID: sessionID,
		AgentKind: "claude-code",
		HookInput: json.RawMessage(body),
	}
	if v := os.Getenv("CLAUDE_CODE_VERSION"); v != "" {
		req.HookVersion = v
	}
	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: encode: %v\n", err)
		os.Exit(0)
	}

	client := &http.Client{Timeout: httpTimeout}
	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: new request: %v\n", err)
		os.Exit(0)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atterm-hook: post: %v\n", err)
		os.Exit(0)
	}
	defer resp.Body.Close()
}

func resolveEndpoint() string {
	if v := os.Getenv("ATTERM_HOOK_ENDPOINT"); v != "" {
		return v
	}
	dir, err := endpointFileDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, "hook-endpoint"))
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

func endpointFileDir() (string, error) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "atterm"), nil
		}
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "atterm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "atterm"), nil
}
