// internal/feishu/service_test.go
package feishu

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeStore implements the BindingStore interface for service tests.
type fakeStore struct {
	mu         sync.Mutex
	byHash     map[string]*Binding
	byUser     map[string]*Binding
	pending    map[string]string // code -> user_id
	boundCalls []string          // user_ids passed to MarkBound
	disabled   map[string]bool
	bindError  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{byHash: map[string]*Binding{}, byUser: map[string]*Binding{}, pending: map[string]string{}, disabled: map[string]bool{}}
}

func (f *fakeStore) addBinding(b *Binding) {
	f.byHash[b.AppIDHash] = b
	f.byUser[b.UserID] = b
}

func (f *fakeStore) GetBindingByAppIDHash(ctx context.Context, hash string) (*Binding, error) {
	if b, ok := f.byHash[hash]; ok {
		return b, nil
	}
	return nil, ErrBindingNotFound
}
func (f *fakeStore) GetBindingByUserID(ctx context.Context, userID string) (*Binding, error) {
	if b, ok := f.byUser[userID]; ok {
		return b, nil
	}
	return nil, ErrBindingNotFound
}
func (f *fakeStore) MarkBound(ctx context.Context, userID, openID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boundCalls = append(f.boundCalls, userID)
	if b, ok := f.byUser[userID]; ok {
		b.OpenID = openID
	}
	return nil
}
func (f *fakeStore) MarkDisabled(ctx context.Context, userID string) error {
	f.disabled[userID] = true
	return nil
}
func (f *fakeStore) ConsumePendingBind(ctx context.Context, code string) (string, error) {
	if u, ok := f.pending[code]; ok {
		delete(f.pending, code)
		return u, nil
	}
	return "", ErrFeishuPendingBindNotFoundService
}

// fakeIM captures Send calls.
type fakeIM struct {
	mu              sync.Mutex
	sentInteractive []struct {
		Token, OpenID string
		Body          []byte
	}
	sentText []struct{ Token, OpenID, Text string }
	err      error
}

func (f *fakeIM) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sentInteractive = append(f.sentInteractive, struct {
		Token, OpenID string
		Body          []byte
	}{token, openID, body})
	return nil
}
func (f *fakeIM) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentText = append(f.sentText, struct{ Token, OpenID, Text string }{token, openID, text})
	return nil
}

// fakeToken always returns a fixed token; used in service tests.
type fakeToken struct {
	tok string
	err error
}

func (f *fakeToken) Get(ctx context.Context, appID, secret string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.tok, nil
}
func (f *fakeToken) Invalidate(string) {}

func newSvc(store BindingStore, im IMClient, tok TokenSource) *Service {
	return NewService(ServiceConfig{Store: store, IM: im, Token: tok})
}

func TestService_HandleEvent_BindMessageHappy(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", AppID: "a", AppSecret: "s", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "hash1", OpenID: ""})
	st.pending["AB3CD7"] = "u1"
	im := &fakeIM{}
	svc := newSvc(st, im, &fakeToken{tok: "tt"})

	// Build encrypted body with this binding's encrypt_key.
	plain := []byte(`{"header":{"event_type":"im.message.receive_v1","token":"vv","app_id":"a"},"event":{"sender":{"sender_id":{"open_id":"ou_user1"}},"message":{"content":"{\"text\":\"/bind AB3CD7\"}","message_type":"text"}}}`)
	body := feishuTestEncrypt(t, "kk", plain)

	resp, err := svc.HandleEvent(context.Background(), "hash1", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.Status != HandleStatusOK {
		t.Fatalf("status: %v", resp.Status)
	}
	// Wait up to 1s for the goroutined handleBindMessage to complete.
	// Poll on both boundCalls and sentText with proper locking.
	for i := 0; i < 100; i++ {
		st.mu.Lock()
		boundDone := len(st.boundCalls) > 0
		st.mu.Unlock()
		if boundDone {
			im.mu.Lock()
			textDone := len(im.sentText) > 0
			im.mu.Unlock()
			if textDone {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	st.mu.Lock()
	boundCalls := st.boundCalls
	st.mu.Unlock()
	if len(boundCalls) != 1 || boundCalls[0] != "u1" {
		t.Fatalf("MarkBound calls: %v", boundCalls)
	}
	// And a confirmation text should have been sent.
	im.mu.Lock()
	sentTextLen := len(im.sentText)
	im.mu.Unlock()
	if sentTextLen != 1 {
		t.Fatalf("expected 1 confirmation text, got %d", sentTextLen)
	}
}

func TestService_HandleEvent_UnknownHash(t *testing.T) {
	st := newFakeStore()
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
	resp, err := svc.HandleEvent(context.Background(), "no-such-hash", []byte(`{}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Status != HandleStatusOK || resp.Reason != "unknown_app_id_hash" {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestService_HandleEvent_URLVerification(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "hash1"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

	plain := []byte(`{"type":"url_verification","challenge":"abc","token":"vv"}`)
	body := feishuTestEncrypt(t, "kk", plain)
	resp, err := svc.HandleEvent(context.Background(), "hash1", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.URLChallenge != "abc" {
		t.Fatalf("challenge: %q", resp.URLChallenge)
	}
}

func TestService_HandleEvent_CardAck(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "kk", VerifyToken: "vv", AppIDHash: "h"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

	plain := []byte(`{"header":{"event_type":"card.action.trigger","token":"vv","app_id":"a"},"event":{"action":{"value":{"kind":"ack","session_id":"sid-1","event":"command_finished"}},"operator":{"open_id":"ou_x"}}}`)
	body := feishuTestEncrypt(t, "kk", plain)
	resp, err := svc.HandleEvent(context.Background(), "h", body)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if resp.CardUpdate == nil {
		t.Fatalf("expected card update response")
	}
}

func TestService_HandleEvent_DecryptFailure(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", EncryptKey: "right-key", VerifyToken: "vv", AppIDHash: "h"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
	body := feishuTestEncrypt(t, "wrong-key", []byte(`{}`))
	resp, err := svc.HandleEvent(context.Background(), "h", body)
	if err != nil {
		t.Fatalf("handle should not return err (must reply 200): %v", err)
	}
	if resp.Status != HandleStatusOK || !errors.Is(resp.LogError, ErrDecryptFailed) {
		t.Fatalf("resp: %+v", resp)
	}
}

func TestService_RelayToken_Success(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{
		UserID: "u1", AppID: "a", AppSecret: "s",
		EncryptKey: "k", VerifyToken: "v",
		AppIDHash: "h", OpenID: "ou_x",
	})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})

	tok, openID, hash, err := svc.RelayToken(context.Background(), "u1")
	if err != nil {
		t.Fatalf("RelayToken: %v", err)
	}
	if tok != "tt" || openID != "ou_x" || hash != "h" {
		t.Fatalf("got tok=%q open=%q hash=%q", tok, openID, hash)
	}
}

func TestService_RelayToken_NoBinding(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeIM{}, &fakeToken{tok: "tt"})
	_, _, _, err := svc.RelayToken(context.Background(), "missing")
	if !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("want ErrBindingNotFound, got %v", err)
	}
}

func TestService_RelayToken_Disabled(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", AppIDHash: "h", DisabledAt: 1, OpenID: "ou_x"})
	svc := newSvc(st, &fakeIM{}, &fakeToken{tok: "tt"})
	_, _, _, err := svc.RelayToken(context.Background(), "u1")
	if !errors.Is(err, ErrBindingDisabled) {
		t.Fatalf("want ErrBindingDisabled, got %v", err)
	}
}

func TestService_RelayToken_UpstreamFail(t *testing.T) {
	st := newFakeStore()
	st.addBinding(&Binding{UserID: "u1", AppID: "a", AppSecret: "s", AppIDHash: "h", OpenID: "ou_x"})
	bad := errors.New("upstream boom")
	svc := newSvc(st, &fakeIM{}, &fakeToken{err: bad})
	_, _, _, err := svc.RelayToken(context.Background(), "u1")
	if !errors.Is(err, bad) {
		t.Fatalf("want upstream error, got %v", err)
	}
}
