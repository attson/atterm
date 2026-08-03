package relay

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// body omits expires_at. 7 days balances "convenient for share-with-a-colleague"
// against "stale invite codes pile up" — bind-and-forget is a security smell.
const defaultInviteExpiry = 7 * 24 * time.Hour

// handleCreateInvite implements POST /admin/api/invitations.
// Body (optional):
//
//	{
//	  "expires_at": <RFC3339 or null or "" → defaults to now + 7 days>,
//	  "note":       "<string>",
//	  "count":      <int, default 1, clamped to [1, 50]> — bulk-create N invites
//	                with the same note/expiry; each gets a distinct plaintext.
//	}
//
// Response 201:
//
//	count == 1: {"plaintext": "inv_…", "code_prefix": "...", "note": "...",
//	             "expires_at": "...", "created_at": "..."}
//	count >  1: {"invites": [<the same shape>, ...]}
func (a *AdminServer) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresAt *string `json:"expires_at"`
		Note      string  `json:"note"`
		Count     int     `json:"count"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck — body is optional
	}

	count := body.Count
	if count <= 0 {
		count = 1
	}
	if count > 50 {
		http.Error(w, "count exceeds maximum (50)", http.StatusBadRequest)
		return
	}

	var expiresAt *time.Time
	if body.ExpiresAt != nil && *body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *body.ExpiresAt)
		if err != nil {
			http.Error(w, "invalid expires_at format, use RFC3339", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	} else {
		t := time.Now().Add(defaultInviteExpiry)
		expiresAt = &t
	}

	type invResp struct {
		Plaintext  string  `json:"plaintext"`
		CodePrefix string  `json:"code_prefix"`
		Note       string  `json:"note"`
		ExpiresAt  *string `json:"expires_at"`
		CreatedAt  string  `json:"created_at"`
	}

	invites := make([]invResp, 0, count)
	for i := 0; i < count; i++ {
		secret, inv, err := a.Store.CreateInvitation(r.Context(), expiresAt, body.Note)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		row := invResp{
			Plaintext:  secret.Expose(),
			CodePrefix: inv.CodePrefix,
			Note:       inv.Note,
			CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
		}
		if inv.ExpiresAt != nil {
			s := inv.ExpiresAt.UTC().Format(time.RFC3339)
			row.ExpiresAt = &s
		}
		invites = append(invites, row)
	}

	if count == 1 {
		writeJSONStatus(w, http.StatusCreated, invites[0])
		return
	}
	writeJSONStatus(w, http.StatusCreated, struct {
		Invites []invResp `json:"invites"`
	}{Invites: invites})
}

// handleListInvites implements GET /admin/api/invitations.
// Response 200: array of invitation rows (no plaintext).
func (a *AdminServer) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invs, err := a.Store.ListInvitations(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type invRow struct {
		CodePrefix string  `json:"code_prefix"`
		Note       string  `json:"note"`
		CreatedAt  string  `json:"created_at"`
		ExpiresAt  *string `json:"expires_at"`
		ConsumedAt *string `json:"consumed_at"`
		ConsumedBy string  `json:"consumed_by,omitempty"`
	}

	out := make([]invRow, 0, len(invs))
	for _, inv := range invs {
		row := invRow{
			CodePrefix: inv.CodePrefix,
			Note:       inv.Note,
			CreatedAt:  inv.CreatedAt.UTC().Format(time.RFC3339),
			ConsumedBy: inv.ConsumedBy,
		}
		if inv.ExpiresAt != nil {
			s := inv.ExpiresAt.UTC().Format(time.RFC3339)
			row.ExpiresAt = &s
		}
		if inv.ConsumedAt != nil {
			s := inv.ConsumedAt.UTC().Format(time.RFC3339)
			row.ConsumedAt = &s
		}
		out = append(out, row)
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleListUsers implements GET /admin/api/users.
// Response 200: array of {id, email, created_at, disabled_at, is_admin}.
func (a *AdminServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.Store.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type userRow struct {
		ID         string  `json:"id"`
		Email      string  `json:"email"`
		CreatedAt  string  `json:"created_at"`
		DisabledAt *string `json:"disabled_at,omitempty"`
		IsAdmin    bool    `json:"is_admin"`
	}

	out := make([]userRow, 0, len(users))
	for _, u := range users {
		row := userRow{
			ID:        u.ID,
			Email:     u.Email,
			CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
			IsAdmin:   u.IsAdmin,
		}
		if u.DisabledAt != nil {
			s := u.DisabledAt.UTC().Format(time.RFC3339)
			row.DisabledAt = &s
		}
		out = append(out, row)
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// handleDisableUser implements POST /admin/api/users/{id}/disable.
// Response 200: {"status": "disabled"}
func (a *AdminServer) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if userID == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}

	if err := a.Store.DisableUser(r.Context(), userID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSONStatus(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// handlePromoteUser flips users.is_admin = true for {id}. Idempotent.
// Audit logged with actor (the requesting admin) and target.
func (a *AdminServer) handlePromoteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	var actorID string
	if u, ok := UserFromContext(r.Context()); ok {
		actorID = u.ID
	}
	if err := a.Store.SetUserAdmin(r.Context(), id, true); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("admin role change: actor=%s target=%s op=promote", actorID, id)
	w.WriteHeader(http.StatusNoContent)
}

// countAdmins returns how many users currently have is_admin=1. Used to
// prevent demoting / deleting the last admin and locking the deploy out.
func countAdmins(ctx context.Context, store userstore.Store) (int, error) {
	users, err := store.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		if u.IsAdmin {
			n++
		}
	}
	return n, nil
}

// handleDemoteUser flips users.is_admin = false for {id}, with two
// guardrails: self-demote (400 cannot_demote_self) and last-admin
// (409 last_admin).
func (a *AdminServer) handleDemoteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}
	var actorID string
	if u, ok := UserFromContext(r.Context()); ok {
		actorID = u.ID
	}
	if id == actorID {
		writeError(w, http.StatusBadRequest, "cannot_demote_self")
		return
	}
	target, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if target.IsAdmin {
		n, err := countAdmins(r.Context(), a.Store)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if n <= 1 {
			writeError(w, http.StatusConflict, "last_admin")
			return
		}
	}
	if err := a.Store.SetUserAdmin(r.Context(), id, false); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	log.Printf("admin role change: actor=%s target=%s op=demote", actorID, id)
	w.WriteHeader(http.StatusNoContent)
}
