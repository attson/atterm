package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	internalfeishu "github.com/attson/atterm/internal/feishu"
	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
)

func TestService_RelayMode_SelectsRelayStore(t *testing.T) {
	svc, _ := NewService(ServiceConfig{
		Mode:       ModeRelay,
		RelayURL:   "http://example",
		RelayToken: func() string { return "tok" },
	})
	if _, ok := svc.store.(*RelayBackedBindingStore); !ok {
		t.Fatalf("relay mode should pick relay store, got %T", svc.store)
	}
}

func TestService_LocalMode_SelectsLocalStore(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{
		Mode:       ModeLocal,
		FeishuBase: "https://open.feishu.cn",
	})
	if _, ok := svc.store.(*LocalKeychainBindingStore); !ok {
		t.Fatalf("local mode should pick local store, got %T", svc.store)
	}
}

func TestService_DispatchExposesDispatcher(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	if svc.Dispatcher() == nil {
		t.Fatalf("Service.Dispatcher() must return non-nil")
	}
}

func TestService_HookServerExposed(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	addr, _, err := svc.HookServer().Start()
	if err != nil {
		t.Fatalf("hook server start: %v", err)
	}
	if addr == "" {
		t.Fatalf("expected bound addr")
	}
}

// stubSessionLookup is the minimal SessionLookup used by the assembly tests.
type stubSessionLookup struct{}

func (stubSessionLookup) Exists(uuid.UUID) bool          { return true }
func (stubSessionLookup) Inject(uuid.UUID, string) error { return nil }

var _ SessionLookup = stubSessionLookup{}

func TestService_HandleCardAction_RoutesToInternalEvent(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)
	svc, _ := NewService(ServiceConfig{Mode: ModeLocal, FeishuBase: "https://open.feishu.cn"})
	ack := svc.RenderAck("command_finished", "session-123")
	if ack.Card == nil {
		t.Fatalf("RenderAck must return a card payload")
	}
}

func TestService_ExistsSatisfiesLookup(t *testing.T) {
	var _ SessionLookup = (*Service)(nil)
}

func TestService_RelayMode_RequiresTokenFn(t *testing.T) {
	_, err := NewService(ServiceConfig{Mode: ModeRelay, RelayURL: "http://x"})
	if err == nil {
		t.Fatalf("expected error when RelayToken is nil in relay mode")
	}
}

func TestService_BeginPair_RelayMode(t *testing.T) {
	var hitBeginPair bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feishu/bindings/me/begin-pair" && r.Method == "POST" {
			hitBeginPair = true
			if got := r.Header.Get("Authorization"); got != "Bearer relay-tok" {
				t.Errorf("auth header: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "ABC123"})
			return
		}
		t.Errorf("unexpected req %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc, err := NewService(ServiceConfig{
		Mode:       ModeRelay,
		RelayURL:   srv.URL,
		RelayToken: func() string { return "relay-tok" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	code, err := svc.BeginPair(context.Background())
	if err != nil {
		t.Fatalf("BeginPair: %v", err)
	}
	if code != "ABC123" {
		t.Fatalf("code: %q", code)
	}
	if !hitBeginPair {
		t.Fatal("begin-pair endpoint was not called")
	}
}

func TestService_BeginPair_LocalMode(t *testing.T) {
	keyring.MockInit()
	defer keyring.MockInitWithError(nil)

	svc, err := NewService(ServiceConfig{
		Mode:       ModeLocal,
		FeishuBase: "https://open.feishu.cn",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	code, err := svc.BeginPair(context.Background())
	if err != nil {
		t.Fatalf("BeginPair local: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-char code, got %q", code)
	}
}

type fakeInjector struct {
	gotSID  uuid.UUID
	gotText string
	err     error
}

func (f *fakeInjector) Exists(uuid.UUID) bool { return true }
func (f *fakeInjector) Inject(sid uuid.UUID, text string) error {
	f.gotSID, f.gotText = sid, text
	return f.err
}

func TestHandleReplyMessage_InjectsToMappedSession(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	sid := uuid.New()
	d.cardMsgs.remember("om_card1", sid)
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "looks good")

	if inj.gotSID != sid {
		t.Fatalf("inject sid = %s, want %s", inj.gotSID, sid)
	}
	if inj.gotText != "looks good\n" {
		t.Fatalf("inject text = %q, want %q", inj.gotText, "looks good\n")
	}
}

func TestHandleReplyMessage_UnknownParentIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_unknown", "hi")
	if inj.gotText != "" {
		t.Fatalf("unmapped parent must not inject, got %q", inj.gotText)
	}
}

func TestHandleReplyMessage_BindCommandIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	d.cardMsgs.remember("om_card1", uuid.New())
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "/bind ABC123")
	if inj.gotText != "" {
		t.Fatalf("/bind must not be injected as a reply, got %q", inj.gotText)
	}
}

func TestHandleReplyMessage_EmptyParentOrTextIgnored(t *testing.T) {
	inj := &fakeInjector{}
	d := NewDispatcher(DispatcherConfig{})
	sid := uuid.New()
	d.cardMsgs.remember("om_card1", sid)
	s := &Service{cfg: ServiceConfig{Sessions: inj}, dispatcher: d}

	s.handleReplyMessage(context.Background(), "ou_op", "", "looks good")
	if inj.gotText != "" {
		t.Fatalf("empty parent must not inject, got %q", inj.gotText)
	}
	s.handleReplyMessage(context.Background(), "ou_op", "om_card1", "   ")
	if inj.gotText != "" {
		t.Fatalf("blank text must not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InjectWritesText(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	sid := uuid.New()
	s.handleCardAction(context.Background(), sid.String(), "inject", "", "", "1\n", nil)
	if inj.gotSID != sid || inj.gotText != "1\n" {
		t.Fatalf("inject got sid=%s text=%q", inj.gotSID, inj.gotText)
	}
}

func TestHandleCardAction_NonInjectIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), uuid.New().String(), "ack", "command_finished", "", "", nil)
	if inj.gotText != "" {
		t.Fatalf("ack should not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InjectErrorDoesNotPanic(t *testing.T) {
	inj := &fakeInjector{err: errors.New("inbound full")}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	// 不应 panic;错误被 log 吞掉。
	s.handleCardAction(context.Background(), uuid.New().String(), "inject", "", "", "x\n", nil)
	if inj.gotText != "x\n" {
		t.Fatalf("inject should still be attempted, got %q", inj.gotText)
	}
}

func TestHandleCardAction_InvalidSessionIDIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), "not-a-uuid", "inject", "", "", "x\n", nil)
	if inj.gotText != "" {
		t.Fatalf("invalid uuid must not inject, got %q", inj.gotText)
	}
}

func TestHandleCardAction_EmptyTextIgnored(t *testing.T) {
	inj := &fakeInjector{}
	s := &Service{cfg: ServiceConfig{Sessions: inj}}
	s.handleCardAction(context.Background(), uuid.New().String(), "inject", "", "", "", nil)
	if inj.gotText != "" {
		t.Fatalf("empty text must not inject, got %q", inj.gotText)
	}
}

// stubRouterSubscriber is a minimal internalfeishu.Subscriber for router tests.
// sentInputs accumulates every SendInput call so tests can assert the new
// text/CR split (one call per chunk, instead of bundled text+\n).
type stubRouterSubscriber struct {
	mu         sync.Mutex
	sentInputs [][]byte
	owner      string
}

func (s *stubRouterSubscriber) ClaimDriver()        {}
func (s *stubRouterSubscriber) OwnerOpenID() string { return s.owner }
func (s *stubRouterSubscriber) SendInput(b []byte) bool {
	s.mu.Lock()
	s.sentInputs = append(s.sentInputs, append([]byte(nil), b...))
	s.mu.Unlock()
	return true
}
func (s *stubRouterSubscriber) CurrentDriverName() string { return "" }
func (s *stubRouterSubscriber) snapshotInputs() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.sentInputs))
	copy(out, s.sentInputs)
	return out
}

// TestHandleCardAction_RouterInput verifies that handleCardAction with kind="input"
// routes through the router and delivers the payload to the subscriber.
func TestHandleCardAction_RouterInput(t *testing.T) {
	const ownerOpenID = "ou_owner"
	sessID := uuid.New().String()

	stub := &stubRouterSubscriber{owner: ownerOpenID}

	idx := internalfeishu.NewCardIndex()
	idx.Put(&internalfeishu.CardAnchor{
		SessionID:   sessID,
		CardMsgID:   "msg_test",
		CardToken:   "tok_test",
		OwnerOpenID: ownerOpenID,
	})

	router := internalfeishu.NewRouter(idx, func(sessionID string) internalfeishu.Subscriber {
		if sessionID == sessID {
			return stub
		}
		return nil
	})

	s := &Service{cfg: ServiceConfig{Sessions: &fakeInjector{}}}
	s.SetRouter(router)

	s.handleCardAction(context.Background(), sessID, "input", "", ownerOpenID, "ls", nil)

	// Router splits into two SendInput calls (text, then CR after 16ms).
	time.Sleep(80 * time.Millisecond)
	got := stub.snapshotInputs()
	if len(got) != 2 || string(got[0]) != "ls" || got[1][0] != 0x0d {
		t.Fatalf("sentInputs = %q, want [text, {0x0d}] split", got)
	}
}

// TestHandleCardAction_RouterInputWrongOwner verifies that handleCardAction
// rejects input from an operator who is not the anchor owner.
func TestHandleCardAction_RouterInputWrongOwner(t *testing.T) {
	const ownerOpenID = "ou_owner"
	sessID := uuid.New().String()

	stub := &stubRouterSubscriber{owner: ownerOpenID}

	idx := internalfeishu.NewCardIndex()
	idx.Put(&internalfeishu.CardAnchor{
		SessionID:   sessID,
		CardMsgID:   "msg_test2",
		CardToken:   "tok_test2",
		OwnerOpenID: ownerOpenID,
	})

	router := internalfeishu.NewRouter(idx, func(sessionID string) internalfeishu.Subscriber {
		if sessionID == sessID {
			return stub
		}
		return nil
	})

	s := &Service{cfg: ServiceConfig{Sessions: &fakeInjector{}}}
	s.SetRouter(router)

	s.handleCardAction(context.Background(), sessID, "input", "", "ou_intruder", "rm -rf /", nil)

	if got := stub.snapshotInputs(); len(got) != 0 {
		t.Fatalf("wrong owner must not inject, got %q", got)
	}
}

// bindReplyService wires a Service whose replyText path is fully stubbed: an
// in-memory store with saved credentials, a fixed token source, and a
// capturing IM so tests can assert the confirmation text sent back to Feishu.
func bindReplyService(store *inMemBindingStore, im *capturingIM) *Service {
	ts := &stubTokenSource{tok: "tt", openID: "ou_x", hash: "h"}
	d := NewDispatcher(DispatcherConfig{Store: store, Token: ts, IM: im})
	return &Service{cfg: ServiceConfig{Mode: ModeLocal}, store: store, dispatcher: d, imClient: im}
}

func TestHandleBindMessage_SuccessRepliesConfirmation(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s"})
	im := &capturingIM{}
	s := bindReplyService(store, im)
	code := s.IssuePending()

	s.handleBindMessage(context.Background(), "ou_user", "/bind "+code)

	if store.view.OpenID != "ou_user" {
		t.Fatalf("bind must persist OpenID, got %q", store.view.OpenID)
	}
	if len(im.texts) != 1 || im.texts[0] != "✅ 已绑定到 atterm" {
		t.Fatalf("want one success reply, got %v", im.texts)
	}
}

func TestHandleBindMessage_InvalidCodeRepliesError(t *testing.T) {
	store := &inMemBindingStore{}
	_ = store.SetCredentials(context.Background(), Credentials{AppID: "a", AppSecret: "s"})
	im := &capturingIM{}
	s := bindReplyService(store, im)

	s.handleBindMessage(context.Background(), "ou_user", "/bind NOPE12")

	if store.view.OpenID != "" {
		t.Fatalf("invalid code must not bind, got %q", store.view.OpenID)
	}
	if len(im.texts) != 1 || im.texts[0] != "❌ 短码无效或已过期" {
		t.Fatalf("want one invalid-code reply, got %v", im.texts)
	}
}

func TestHandleBindMessage_SetBoundErrorRepliesError(t *testing.T) {
	// Empty store: a valid code consumes, but SetBound returns
	// ErrLocalBindingNotFound (no credentials blob yet) → server-error reply.
	store := &inMemBindingStore{}
	im := &capturingIM{}
	s := bindReplyService(store, im)
	code := s.IssuePending()

	s.handleBindMessage(context.Background(), "ou_user", "/bind "+code)

	if len(im.texts) != 1 || im.texts[0] != "❌ 服务端错误,请稍后再试" {
		t.Fatalf("want one server-error reply, got %v", im.texts)
	}
}
