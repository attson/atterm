package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deleteMeReq builds a DELETE /api/me request with JSON body, session cookie,
// and CSRF header. Centralised so each test case stays focused on its assertion.
func deleteMeReq(body any, cookie *http.Cookie, csrf string) *http.Request {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodDelete, "/api/me", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	return r
}

func TestDeleteMe_Success(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
	csrf := csrfTokenFor(t, handler, cookie)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "a@example.com",
		"password": "passphrase-1234",
	}, cookie, csrf))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUser(context.Background(), userID); err == nil {
		t.Error("user still exists after DELETE /api/me")
	}
}

func TestDeleteMe_WrongEmail_400(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
	csrf := csrfTokenFor(t, handler, cookie)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "other@example.com",
		"password": "passphrase-1234",
	}, cookie, csrf))
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

func TestDeleteMe_WrongPassword_401(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "a@example.com", "passphrase-1234")
	csrf := csrfTokenFor(t, handler, cookie)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "a@example.com",
		"password": "wrong-password-1234",
	}, cookie, csrf))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d; want 401", rec.Code)
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "password_incorrect" {
		t.Errorf("error=%q; want password_incorrect", resp["error"])
	}
	if _, err := store.GetUser(context.Background(), userID); err != nil {
		t.Errorf("user should still exist after rejected delete: %v", err)
	}
}

func TestDeleteMe_LastAdmin_409(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "admin@example.com", "passphrase-1234")
	if err := store.SetUserAdmin(context.Background(), userID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin: %v", err)
	}
	csrf := csrfTokenFor(t, handler, cookie)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "admin@example.com",
		"password": "passphrase-1234",
	}, cookie, csrf))
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
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "mixedcase@example.com", "passphrase-1234")
	csrf := csrfTokenFor(t, handler, cookie)

	// Email typed with different case must still match.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{"email": "MixedCase@Example.com", "password": "passphrase-1234"}, cookie, csrf))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d; want 204 (case should not matter): %s", rec.Code, rec.Body.String())
	}
	if _, err := store.GetUser(context.Background(), userID); err == nil {
		t.Error("user still exists")
	}
}

func TestDeleteMe_AdminButNotLast_Succeeds(t *testing.T) {
	srv, store := newTestAuthServer(t)
	handler := srv.Routes()
	cookie, userID, _ := signupAndLogin(t, handler, store, "admin@example.com", "passphrase-1234")
	if err := store.SetUserAdmin(context.Background(), userID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin for first admin: %v", err)
	}
	csrf := csrfTokenFor(t, handler, cookie)

	// Second admin so the first isn't the last.
	other, err := store.CreateUser(context.Background(), "other@example.com", "passphrase-1234")
	if err != nil {
		t.Fatalf("setup: CreateUser other: %v", err)
	}
	if err := store.SetUserAdmin(context.Background(), other.ID, true); err != nil {
		t.Fatalf("setup: SetUserAdmin for second admin: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, deleteMeReq(map[string]string{
		"email":    "admin@example.com",
		"password": "passphrase-1234",
	}, cookie, csrf))
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
