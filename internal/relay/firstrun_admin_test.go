package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bytemare/opaque"

	"github.com/attson/atterm/internal/userstore"
)

func newTestHandlerEmail(t *testing.T, bootstrapEmail string) (*OpaqueAuthHandler, *opaque.Configuration) {
	t.Helper()
	return newTestOpaqueAuthHandlerEmail(t, bootstrapEmail), defaultConfig()
}

func decodeFinalize(t *testing.T, rec *httptest.ResponseRecorder) registerFinalizeResponse {
	t.Helper()
	var resp registerFinalizeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode finalize: %v (body=%s)", err, rec.Body.String())
	}
	return resp
}

// First-run setup: registering the configured bootstrap email while no admin
// exists, with NO claim token, auto-promotes to admin.
func TestRegisterFinalize_FirstRunEmail_PromotesAdmin(t *testing.T) {
	h, conf := newTestHandlerEmail(t, "boss@example.com")
	rec := registerFinalizeWithClaim(t, h, conf, "boss@example.com", "hunter2hunter2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeFinalize(t, rec)
	if !resp.IsAdmin {
		t.Fatal("response is_admin=false; want true for first-run bootstrap email")
	}
	u, err := h.store.GetUser(context.Background(), resp.UserID)
	if err != nil || !u.IsAdmin {
		t.Fatalf("user not admin after first-run register: %+v err=%v", u, err)
	}
}

// A non-bootstrap email never auto-promotes, even on a fresh store.
func TestRegisterFinalize_FirstRunEmail_NonBootstrapNotAdmin(t *testing.T) {
	h, conf := newTestHandlerEmail(t, "boss@example.com")
	rec := registerFinalizeWithClaim(t, h, conf, "someoneelse@example.com", "pw-cccccccccc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if decodeFinalize(t, rec).IsAdmin {
		t.Fatal("non-bootstrap email must not be auto-admin")
	}
}

// With no bootstrap email configured, the email-gated path is fully disabled.
func TestRegisterFinalize_FirstRunEmail_DisabledWhenUnset(t *testing.T) {
	h, conf := newTestHandlerEmail(t, "")
	rec := registerFinalizeWithClaim(t, h, conf, "anyone@example.com", "pw-dddddddddd", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if decodeFinalize(t, rec).IsAdmin {
		t.Fatal("no bootstrap email set; must not auto-admin")
	}
}

// The bootstrap email can only be registered once (UNIQUE email) — a second
// attempt is a 409, so the admin slot can't be re-claimed.
func TestRegisterFinalize_FirstRunEmail_SameEmail409(t *testing.T) {
	h, conf := newTestHandlerEmail(t, "boss@example.com")
	if rec := registerFinalizeWithClaim(t, h, conf, "boss@example.com", "pw-eeeeeeeeee", ""); rec.Code != http.StatusOK {
		t.Fatalf("first status=%d", rec.Code)
	}
	rec := registerFinalizeWithClaim(t, h, conf, "boss@example.com", "pw-ffffffffff", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second same-email status=%d; want 409", rec.Code)
	}
}

// The public /api/bootstrap/status endpoint reports admin existence and never
// leaks the bootstrap email.
func TestBootstrapStatusEndpoint(t *testing.T) {
	store := userstore.NewInMemory(t)
	srv := NewServer(Config{
		Resolver:            NewIdentityResolver(store),
		Store:               store,
		BootstrapAdminEmail: "boss@example.com",
	})
	get := func() (int, string) {
		r := httptest.NewRequest(http.MethodGet, "/api/bootstrap/status", nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	code, body := get()
	if code != http.StatusOK || !strings.Contains(body, `"admin_exists":false`) {
		t.Fatalf("empty store: code=%d body=%s", code, body)
	}
	if strings.Contains(strings.ToLower(body), "email") || strings.Contains(body, "boss@example.com") {
		t.Fatalf("status leaked the bootstrap email: %s", body)
	}

	ctx := context.Background()
	u, _ := store.CreateOpaqueUser(ctx, "a@example.com")
	if err := store.SetUserAdmin(ctx, u.ID, true); err != nil {
		t.Fatal(err)
	}
	if code, body := get(); code != http.StatusOK || !strings.Contains(body, `"admin_exists":true`) {
		t.Fatalf("after admin: code=%d body=%s", code, body)
	}
}
