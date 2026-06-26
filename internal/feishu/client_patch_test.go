package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPatchCard_Success(t *testing.T) {
	var got struct {
		path   string
		auth   string
		body   map[string]any
		method string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		got.method = r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	bodyMarkdown := "$ ls\nfoo bar"
	err := c.PatchCard(context.Background(), "tok123", "card_token_xyz", bodyMarkdown, 7)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.method != "PATCH" {
		t.Errorf("method = %q, want PATCH", got.method)
	}
	if !strings.Contains(got.path, "card_token_xyz") {
		t.Errorf("path = %q, want it to contain card token", got.path)
	}
	if got.auth != "Bearer tok123" {
		t.Errorf("auth = %q, want Bearer tok123", got.auth)
	}
}

func TestPatchCard_NonZeroCodeReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":230030,"msg":"card not found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	err := c.PatchCard(context.Background(), "tok", "card_token", "body", 1)
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
	if !strings.Contains(err.Error(), "230030") {
		t.Errorf("error should expose code, got: %v", err)
	}
}
