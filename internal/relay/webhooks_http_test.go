package relay

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// webhookServer builds a full Server with auth wired so /api/me/webhooks
// routes are gated by requireSession. Returns the Server and a fresh user's
// session token.
func webhookServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, tok, _ := serverWithAuthAndSession(t)
	return s, tok
}

func TestCreateAndListWebhook(t *testing.T) {
	s, tok := webhookServer(t)

	body := map[string]any{
		"url":            "https://open.feishu.cn/x",
		"format":         "feishu",
		"name":           "phone",
		"allow_insecure": false,
	}
	wCreate := postJSONWithBearer(s, "/api/me/webhooks", body, tok)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create webhook: expected 201, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	wList := getWithBearer(s, "/api/me/webhooks", tok)
	if wList.Code != http.StatusOK {
		t.Fatalf("list webhooks: expected 200, got %d: %s", wList.Code, wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), "open.feishu.cn") {
		t.Errorf("list webhooks: response does not contain 'open.feishu.cn': %s", wList.Body.String())
	}
}

func TestCreateWebhookRejectsBadFormat(t *testing.T) {
	s, tok := webhookServer(t)

	body := map[string]any{
		"url":    "https://hooks.slack.com/x",
		"format": "slack",
		"name":   "myslack",
	}
	w := postJSONWithBearer(s, "/api/me/webhooks", body, tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad format: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWebhookRejectsInsecureWithoutFlag(t *testing.T) {
	s, tok := webhookServer(t)

	body := map[string]any{
		"url":            "http://r.example.com/x",
		"format":         "generic",
		"name":           "insecure-no-flag",
		"allow_insecure": false,
	}
	w := postJSONWithBearer(s, "/api/me/webhooks", body, tok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("insecure without flag: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWebhookAcceptsInsecureWithFlag(t *testing.T) {
	s, tok := webhookServer(t)

	body := map[string]any{
		"url":            "http://r.example.com/x",
		"format":         "generic",
		"name":           "insecure-ok",
		"allow_insecure": true,
	}
	w := postJSONWithBearer(s, "/api/me/webhooks", body, tok)
	if w.Code != http.StatusCreated {
		t.Fatalf("insecure with flag: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteWebhook(t *testing.T) {
	s, tok := webhookServer(t)

	// Create one first.
	body := map[string]any{
		"url":    "https://open.feishu.cn/del",
		"format": "feishu",
		"name":   "to-delete",
	}
	wCreate := postJSONWithBearer(s, "/api/me/webhooks", body, tok)
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

	wDel := deleteWithBearer(s, "/api/me/webhooks/"+id, tok)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("delete webhook: expected 204, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// Verify it's gone.
	wList := getWithBearer(s, "/api/me/webhooks", tok)
	if strings.Contains(wList.Body.String(), id) {
		t.Errorf("deleted webhook still appears in list: %s", wList.Body.String())
	}
}
