// internal/feishu/client_test.go
package feishu

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_SendInteractiveToOpenID(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_xxx"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	mid, err := c.SendInteractiveToOpenID(context.Background(), "tt_token", "ou_dest", []byte(`{"msg_type":"interactive","card":{"x":1}}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if mid != "om_xxx" {
		t.Fatalf("message_id: got %q want om_xxx", mid)
	}
	if !strings.HasPrefix(gotPath, "/open-apis/im/v1/messages?") {
		t.Fatalf("path: %s", gotPath)
	}
	if !strings.Contains(gotPath, "receive_id_type=open_id") {
		t.Fatalf("missing receive_id_type query: %s", gotPath)
	}
	if gotAuth != "Bearer tt_token" {
		t.Fatalf("auth header: %q", gotAuth)
	}
	if gotBody["receive_id"] != "ou_dest" || gotBody["msg_type"] != "interactive" {
		t.Fatalf("body: %+v", gotBody)
	}
}

func TestClient_SendInteractive_FeishuError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":99991663,"msg":"invalid access_token"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, srv.Client())
	_, err := c.SendInteractiveToOpenID(context.Background(), "tt", "ou", []byte(`{}`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if !IsAuthClassError(err) {
		t.Fatalf("expected auth-class, got %v", err)
	}
}

func TestClient_SendText(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, srv.Client())
	if err := c.SendTextToOpenID(context.Background(), "tt", "ou_x", "hello"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	if gotBody["msg_type"] != "text" {
		t.Fatalf("msg_type: %v", gotBody["msg_type"])
	}
	content, _ := gotBody["content"].(string)
	if !strings.Contains(content, "hello") {
		t.Fatalf("content: %s", content)
	}
}
