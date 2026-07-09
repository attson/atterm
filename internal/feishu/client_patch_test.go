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
	err := c.PatchCard(context.Background(), "tok123", "card_token_xyz", "anchor_body_md", bodyMarkdown, 7)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.method != "PATCH" {
		t.Errorf("method = %q, want PATCH", got.method)
	}
	if !strings.Contains(got.path, "card_token_xyz") {
		t.Errorf("path = %q, want it to contain card token", got.path)
	}
	// V2 streaming PATCH targets the element by id at /elements/<element_id>;
	// the old card-level path silently no-ops, which is why the body never
	// updated in feishu even though our PATCH returned code=0.
	if !strings.Contains(got.path, "/elements/anchor_body_md") {
		t.Errorf("path = %q, want it to contain /elements/<element_id>", got.path)
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
	err := c.PatchCard(context.Background(), "tok", "card_token", "anchor_body_md", "body", 1)
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
	if !strings.Contains(err.Error(), "230030") {
		t.Errorf("error should expose code, got: %v", err)
	}
}

// SendAnchorCard must create a CardKit entity (so PATCH later can address it
// by card_id) and then send an IM message that references the card_id.
// Returning the IM message_id as the card_token — as the initial impl did —
// makes every subsequent PATCH fail with "field validation failed" because
// the cardkit/v1/cards/{id}/elements/{element_id} endpoint can't resolve an
// IM message_id as a card_id.
func TestSendAnchorCard_CreatesCardEntityAndReturnsRealCardID(t *testing.T) {
	var (
		createCalls int
		imCalls     int
		imContent   string
		createData  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cardkit/v1/cards"):
			createCalls++
			b, _ := io.ReadAll(r.Body)
			var got struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			_ = json.Unmarshal(b, &got)
			createData = got.Data
			if got.Type != "card_json" {
				t.Errorf("create body type = %q, want card_json", got.Type)
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"card_id":"card_xyz_123"}}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/im/v1/messages"):
			imCalls++
			b, _ := io.ReadAll(r.Body)
			var got struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(b, &got)
			imContent = got.Content
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"message_id":"om_xyz"}}`))
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 2 * time.Second})
	cardJSON, _ := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	msgID, cardID, err := c.SendAnchorCard(context.Background(), "tok", "ou_owner", cardJSON)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if createCalls != 1 || imCalls != 1 {
		t.Fatalf("expected 1 cardkit create + 1 im send, got create=%d im=%d", createCalls, imCalls)
	}
	if msgID != "om_xyz" {
		t.Errorf("msgID = %q, want om_xyz", msgID)
	}
	if cardID != "card_xyz_123" {
		t.Errorf("cardID = %q, want card_xyz_123 (real cardkit card_id, not im msg_id)", cardID)
	}
	// IM content must reference the card_id, not embed the card JSON.
	if !strings.Contains(imContent, "card_xyz_123") {
		t.Errorf("im content should reference card_id, got: %s", imContent)
	}
	if !strings.Contains(imContent, `"type":"card"`) {
		t.Errorf("im content should be type=card, got: %s", imContent)
	}
	// CardKit create payload must include the element_id so PATCH can target it.
	if !strings.Contains(createData, AnchorBodyElementID) {
		t.Errorf("cardkit create data missing element_id %q: %s", AnchorBodyElementID, createData)
	}
}
