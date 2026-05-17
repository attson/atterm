package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/userstore"
)

func openTestStore(t *testing.T) userstore.Store {
	t.Helper()
	s, err := userstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBootstrapAdmin_EmptyEmail_NoOp(t *testing.T) {
	store := openTestStore(t)
	if err := bootstrapAdmin(context.Background(), store, "", ""); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	users, _ := store.ListUsers(context.Background())
	if len(users) != 0 {
		t.Errorf("created users = %d; want 0", len(users))
	}
}

func TestBootstrapAdmin_MalformedEmail_Errors(t *testing.T) {
	store := openTestStore(t)
	err := bootstrapAdmin(context.Background(), store, "not-an-email", "Strong-passphrase-2026")
	if err == nil {
		t.Fatal("malformed email accepted")
	}
	if !strings.Contains(err.Error(), "ATTERM_BOOTSTRAP_ADMIN_EMAIL") {
		t.Errorf("error doesn't mention the env var: %v", err)
	}
}

func TestBootstrapAdmin_NewUser_Created(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	store := openTestStore(t)
	if err := bootstrapAdmin(context.Background(), store, "fresh@example.com", "Strong-passphrase-2026"); err != nil {
		t.Fatal(err)
	}
	v, _ := store.VerifyPassword(context.Background(), "fresh@example.com", "Strong-passphrase-2026")
	if v == nil || !v.IsAdmin {
		t.Fatal("user not created as admin")
	}
	if !strings.Contains(buf.String(), "WARN") || !strings.Contains(buf.String(), "unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Errorf("expected WARN about unsetting password env, got:\n%s", buf.String())
	}
}

func TestBootstrapAdmin_ExistingUser_PromotedAndWarn(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ctx := context.Background()
	store := openTestStore(t)
	u, _ := store.CreateUser(ctx, "existing@example.com", "original-passphrase")

	if err := bootstrapAdmin(ctx, store, "existing@example.com", "Leftover-env-pwd-2026!"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetUser(ctx, u.ID)
	if !got.IsAdmin {
		t.Error("existing user not promoted")
	}
	if !strings.Contains(buf.String(), "WARN") || !strings.Contains(buf.String(), "password ignored") {
		t.Errorf("expected WARN about ignored password, got:\n%s", buf.String())
	}
}

func TestBootstrapAdmin_NewUser_WeakPassword_Errors(t *testing.T) {
	store := openTestStore(t)
	err := bootstrapAdmin(context.Background(), store, "fresh@example.com", "short")
	if err == nil {
		t.Fatal("weak password accepted")
	}
	// The weak-password path bubbles up either via validateBootstrapPassword
	// (length/class/blacklist) or via ErrEmptyBootstrapPassword. Both
	// satisfy "the deploy fails fast".
	if !errors.Is(err, userstore.ErrEmptyBootstrapPassword) &&
		!strings.Contains(err.Error(), "ATTERM_BOOTSTRAP_ADMIN_PASSWORD") {
		t.Errorf("err = %v; want validateBootstrapPassword failure or ErrEmptyBootstrapPassword", err)
	}
}
