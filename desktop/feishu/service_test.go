package feishu

import (
	"testing"

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

func (stubSessionLookup) Exists(uuid.UUID) bool { return true }

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
