package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

func TestServiceLoopbackTargetRejectsNonLoopback(t *testing.T) {
	for _, host := range []string{"", "localhost", "127.0.0.1", "::1"} {
		if _, err := serviceLoopbackTarget(host, 3000); err != nil {
			t.Errorf("host %q rejected: %v", host, err)
		}
	}
	for _, host := range []string{"10.0.0.7", "example.test", "192.168.1.2"} {
		if _, err := serviceLoopbackTarget(host, 3000); err == nil {
			t.Errorf("host %q accepted", host)
		}
	}
}

func TestServicePreviewHandlerRoutesPathPrefixes(t *testing.T) {
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "frontend:%s", r.URL.Path)
	}))
	defer frontend.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "api:%s", r.URL.Path)
	}))
	defer api.Close()
	frontendURL, _ := url.Parse(frontend.URL)
	apiURL, _ := url.Parse(api.URL)
	h := newServicePreviewHandler([]servicePreviewRoute{
		newServicePreviewRoute("", frontendURL),
		newServicePreviewRoute("/api", apiURL),
	})
	server := httptest.NewServer(h)
	defer server.Close()

	checks := []struct {
		path string
		want string
	}{
		{path: "/", want: "frontend:/"},
		{path: "/assets/app.js", want: "frontend:/assets/app.js"},
		{path: "/api/users", want: "api:/api/users"},
		{path: "/api", want: "api:/api"},
		{path: "/api-v2", want: "frontend:/api-v2"},
	}
	for _, check := range checks {
		resp, err := http.Get(server.URL + check.path)
		if err != nil {
			t.Fatalf("GET %s: %v", check.path, err)
		}
		body := make([]byte, 128)
		n, _ := resp.Body.Read(body)
		_ = resp.Body.Close()
		if got := string(body[:n]); got != check.want {
			t.Errorf("GET %s = %q, want %q", check.path, got, check.want)
		}
	}
}

func TestNormalizeServicePreviewPrefix(t *testing.T) {
	tests := []struct {
		raw  string
		root bool
		want string
		ok   bool
	}{
		{raw: "", root: true, want: "", ok: true},
		{raw: "/", root: true, want: "", ok: true},
		{raw: "/api", want: "/api", ok: true},
		{raw: " /api/ ", want: "/api", ok: true},
		{raw: "api", ok: false},
		{raw: "/api//v1", ok: false},
		{raw: "/api/../admin", ok: false},
		{raw: "/api?target=x", ok: false},
	}
	for _, test := range tests {
		got, err := normalizeServicePreviewPrefix(test.raw, test.root)
		if (err == nil) != test.ok || got != test.want {
			t.Errorf("normalizeServicePreviewPrefix(%q, %v) = %q, %v", test.raw, test.root, got, err)
		}
	}
}

func TestServicePreviewHandlerProxiesWebSocket(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/echo" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		messageType, payload, err := conn.Read(r.Context())
		if err == nil {
			_ = conn.Write(r.Context(), messageType, append([]byte("echo:"), payload...))
		}
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	gateway := httptest.NewServer(newServicePreviewHandler([]servicePreviewRoute{
		newServicePreviewRoute("/ws", backendURL),
	}))
	defer gateway.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(gateway.URL, "http")+"/ws/echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := conn.Write(ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.Read(ctx)
	if err != nil || string(payload) != "echo:hello" {
		t.Fatalf("websocket response = %q, %v", payload, err)
	}
}

func TestServicePreviewHandlerFlushesSSE(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "data: second\n\n")
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
		}
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)
	gateway := httptest.NewServer(newServicePreviewHandler([]servicePreviewRoute{
		newServicePreviewRoute("", backendURL),
	}))
	defer gateway.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(gateway.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("first SSE line = %q, %v", line, err)
	}
	close(release)
	_, _ = reader.ReadString('\n')
	line, err = reader.ReadString('\n')
	if err != nil || line != "data: second\n" {
		t.Fatalf("second SSE line = %q, %v", line, err)
	}
}
