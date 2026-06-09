package userstore

import (
	"context"
	"testing"
	"time"
)

func TestDeleteUser_CascadesAndNullsConsumedBy(t *testing.T) {
	ctx := context.Background()
	s, _ := Open(ctx, ":memory:")
	defer s.Close()

	// Create an admin to issue invites; consumer is the user we'll delete.
	issuer, _ := s.CreateUser(ctx, "admin@example.com", "passphrase-1234")
	secret, invite, _ := s.CreateInvitation(ctx, ptrTime(time.Now().Add(24*time.Hour)), "for delete test")

	consumer, _ := s.CreateUser(ctx, "victim@example.com", "passphrase-1234")
	if err := s.ConsumeInvitation(ctx, secret.Expose(), consumer.ID); err != nil {
		t.Fatal(err)
	}

	// Give the consumer a session so we can verify cascade.
	_, _, _ = s.CreateSession(ctx, consumer.ID, "ua", "", DefaultSessionTTL)

	// Sanity: invitation is consumed by consumer.
	invs, _ := s.ListInvitations(ctx)
	var ours *Invitation
	for i := range invs {
		if invs[i].ConsumedBy == consumer.ID {
			ours = &invs[i]
		}
	}
	if ours == nil {
		t.Fatal("setup: invitation not consumed by victim")
	}
	_ = invite // used to populate the DB

	// Delete the consumer.
	if err := s.DeleteUser(ctx, consumer.ID); err != nil {
		t.Fatal(err)
	}

	// User row gone.
	if _, err := s.GetUser(ctx, consumer.ID); err == nil {
		t.Error("user still present after DeleteUser")
	}
	// sessions cascade gone.
	sess, _ := s.ListSessions(ctx, consumer.ID)
	if len(sess) != 0 {
		t.Errorf("sessions not cascaded: %d remain", len(sess))
	}

	// Invitation still exists, but consumed_by is now NULL (empty string in struct).
	invsAfter, _ := s.ListInvitations(ctx)
	var foundAfter *Invitation
	for i := range invsAfter {
		if invsAfter[i].CodePrefix == ours.CodePrefix {
			foundAfter = &invsAfter[i]
		}
	}
	if foundAfter == nil {
		t.Fatal("invitation disappeared after victim delete")
	}
	if foundAfter.ConsumedBy != "" {
		t.Errorf("invitation.consumed_by not nulled: %q", foundAfter.ConsumedBy)
	}

	_ = issuer
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
