package relay

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCreateAndListWebhook(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh1@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	body := map[string]any{
		"url":            "https://open.feishu.cn/x",
		"format":         "feishu",
		"name":           "phone",
		"allow_insecure": false,
	}
	wCreate := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, csrfTok)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create webhook: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	wList := getWithCookie(handler, "/api/me/webhooks", cookie)
	if wList.Code != http.StatusOK {
		t.Fatalf("list webhooks: expected 200, got %d: %s", wList.Code, wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), "open.feishu.cn") {
		t.Errorf("list webhooks: response does not contain 'open.feishu.cn': %s", wList.Body.String())
	}
}

func TestCreateWebhookRejectsBadFormat(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh2@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	body := map[string]any{
		"url":    "https://hooks.slack.com/x",
		"format": "slack",
		"name":   "myslack",
	}
	w := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, csrfTok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad format: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWebhookRejectsInsecureWithoutFlag(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh3@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	body := map[string]any{
		"url":            "http://r.example.com/x",
		"format":         "generic",
		"name":           "insecure-no-flag",
		"allow_insecure": false,
	}
	w := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, csrfTok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("insecure without flag: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWebhookAcceptsInsecureWithFlag(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh4@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	body := map[string]any{
		"url":            "http://r.example.com/x",
		"format":         "generic",
		"name":           "insecure-ok",
		"allow_insecure": true,
	}
	w := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, csrfTok)
	if w.Code != http.StatusCreated {
		t.Fatalf("insecure with flag: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteWebhook(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh5@example.com", "correcthorsebattery")
	csrfTok := csrfTokenFor(t, handler, cookie)

	// Create one first.
	body := map[string]any{
		"url":    "https://open.feishu.cn/del",
		"format": "feishu",
		"name":   "to-delete",
	}
	wCreate := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, csrfTok)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create for delete: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(wCreate.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("create response missing id")
	}

	wDel := deleteWithCSRF(handler, "/api/me/webhooks/"+id, cookie, csrfTok)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("delete webhook: expected 204, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// Verify it's gone.
	wList := getWithCookie(handler, "/api/me/webhooks", cookie)
	if strings.Contains(wList.Body.String(), id) {
		t.Errorf("deleted webhook still appears in list: %s", wList.Body.String())
	}
}

func TestCreateWebhookRequiresCSRF(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()

	cookie, _, _ := signupAndLogin(t, handler, store, "wh6@example.com", "correcthorsebattery")

	body := map[string]any{
		"url":    "https://open.feishu.cn/x",
		"format": "feishu",
		"name":   "no-csrf",
	}
	// Post without CSRF token (empty string).
	w := postJSONWithCSRF(handler, "/api/me/webhooks", body, cookie, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("no CSRF: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
