package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

// deleteMeReq builds a DELETE /api/me request with JSON body + Bearer header.
// The optional stepUpUserID, when non-empty, mints a fresh step-up token
// for that user (M1i) and attaches it in the X-Step-Up-Token header so
// the request passes the step-up gate. Pass "" to deliberately omit the
// header — useful for the "step-up required" rejection test.
func deleteMeReq(body any, token, stepUpUserID string) *http.Request {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodDelete, "/api/me", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	if stepUpUserID != "" {
		tok, err := MintStepUpToken(stepUpUserID)
		if err != nil {
			panic(err)
		}
		r.Header.Set("X-Step-Up-Token", tok)
	}
	return r
}

func TestDeleteMe_Success(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "a@b",
		"password": "Correct-Horse-Battery-Staple-1!",
	}, tok, userID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUser(context.Background(), userID); err == nil {
		t.Error("user still exists after DELETE /api/me")
	}
}

func TestDeleteMe_WrongEmail_400(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "other@example.com",
		"password": "Correct-Horse-Battery-Staple-1!",
	}, tok, userID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d; want 400", rec.Code)
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "email_mismatch" {
		t.Errorf("error=%q; want email_mismatch", resp["error"])
	}
	if _, err := store.GetUser(context.Background(), userID); err != nil {
		t.Errorf("user should still exist after rejected delete: %v", err)
	}
}

func TestDeleteMe_LastAdmin_409(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)
	if err := store.SetUserAdmin(context.Background(), userID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "a@b",
		"password": "Correct-Horse-Battery-Staple-1!",
	}, tok, userID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d; want 409", rec.Code)
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "last_admin" {
		t.Errorf("error=%q; want last_admin", resp["error"])
	}
	if _, err := store.GetUser(context.Background(), userID); err != nil {
		t.Errorf("last admin was deleted; should have been refused: %v", err)
	}
}

func TestDeleteMe_EmailCaseInsensitive(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	// Email typed with different case must still match. Helper creates "a@b".
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "A@B",
		"password": "Correct-Horse-Battery-Staple-1!",
	}, tok, userID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d; want 204 (case should not matter): %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUser(context.Background(), userID); err == nil {
		t.Error("user still exists")
	}
}

// TestDeleteMe_MissingStepUpToken_401 covers the M1i-enforce gate.
// A bearer-authenticated request without X-Step-Up-Token must be
// rejected with 401 and "step_up_required" so the UI can prompt the
// user to run the OPAQUE step-up handshake first.
func TestDeleteMe_MissingStepUpToken_401(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	rec := httptest.NewRecorder()
	// stepUpUserID == "" intentionally omits the header.
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "a@b"}, tok, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d: want 401 with missing step-up", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "step_up_required" {
		t.Errorf("error=%q; want step_up_required", resp["error"])
	}
	if _, err := store.GetUser(context.Background(), userID); err != nil {
		t.Errorf("user should still exist after rejected delete: %v", err)
	}
}

// TestDeleteMe_InvalidStepUpToken_401 verifies that a forged or expired
// step-up token also lands in the rejected bucket — distinct error code
// so the UI can hint at "request a fresh handshake" instead of "you
// forgot to do it".
func TestDeleteMe_InvalidStepUpToken_401(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)

	// Hand-craft a request with a clearly invalid step-up token.
	b, _ := json.Marshal(map[string]string{"email": "a@b"})
	r := httptest.NewRequest(http.MethodDelete, "/api/me", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+tok)
	r.Header.Set("X-Step-Up-Token", "stepup_obviously-fake")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d: want 401 with bogus step-up", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "step_up_invalid" {
		t.Errorf("error=%q; want step_up_invalid", resp["error"])
	}
	if _, err := store.GetUser(context.Background(), userID); err != nil {
		t.Errorf("user should still exist after rejected delete: %v", err)
	}
}

func TestDeleteMe_AdminButNotLast_Succeeds(t *testing.T) {
	srv, tok, userID := serverWithAuthAndSession(t)
	store := srv.cfg.Store.(*userstore.SQLiteStore)
	if err := store.SetUserAdmin(context.Background(), userID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin for first admin: %v", err)
	}

	// Second admin so the first isn't the last.
	other, err := store.CreateOpaqueUser(context.Background(), "other@example.com")
	if err != nil {
		t.Fatalf("setup: CreateUser other: %v", err)
	}
	if err := store.SetUserAdmin(context.Background(), other.ID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin for second admin: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "a@b",
		"password": "Correct-Horse-Battery-Staple-1!",
	}, tok, userID))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUser(context.Background(), userID); err == nil {
		t.Error("first admin should have been deleted (second admin still exists)")
	}
	// The other admin must still be present.
	if _, err := store.GetUser(context.Background(), other.ID); err != nil {
		t.Errorf("second admin was wrongly deleted: %v", err)
	}
}
