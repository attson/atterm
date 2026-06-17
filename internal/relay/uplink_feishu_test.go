// internal/relay/uplink_feishu_test.go
package relay

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
)

// fakeIMClient counts SendInteractiveToOpenID calls.
type fakeIMClient struct{ n int32 }

func (f *fakeIMClient) SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) error {
	atomic.AddInt32(&f.n, 1)
	return nil
}
func (f *fakeIMClient) SendTextToOpenID(ctx context.Context, token, openID, text string) error {
	return nil
}

type fakeTokSrc struct{}

func (fakeTokSrc) Get(ctx context.Context, a, b string) (string, error) { return "tt", nil }
func (fakeTokSrc) Invalidate(string)                                    {}

func TestUplinkFeishu_SendOnCommandFinished(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ul@example.com")
	_ = st.UpsertFeishuBinding(ctx, u.ID, userstore.FeishuBindingCredentials{AppID: "a", AppSecret: "s", EncryptKey: "k", VerifyToken: "v"})
	_ = st.MarkFeishuBindingBound(ctx, u.ID, "ou_user")

	imc := &fakeIMClient{}
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: imc, Token: fakeTokSrc{},
	})
	// Simulate the dispatch site directly. (Avoid full uplink fixture.)
	svc.SendCommandFinished(ctx, u.ID, feishu.CommandFinishedInput{SessionID: uuid.New(), ExitCode: 0, Label: "x"})

	// Give the goroutine a moment.
	for i := 0; i < 50 && atomic.LoadInt32(&imc.n) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&imc.n) != 1 {
		t.Fatalf("expected 1 IM call, got %d", imc.n)
	}
}

func TestUplinkFeishu_SkipsUnboundUser(t *testing.T) {
	ctx := context.Background()
	st := newTestUserStoreWithCipher(t)
	u, _ := st.CreateOpaqueUser(ctx, "ul2@example.com")
	// No binding at all.

	imc := &fakeIMClient{}
	svc := feishu.NewService(feishu.ServiceConfig{
		Store: NewFeishuBindStore(st), IM: imc, Token: fakeTokSrc{},
	})
	svc.SendCommandFinished(ctx, u.ID, feishu.CommandFinishedInput{SessionID: uuid.New()})
	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&imc.n) != 0 {
		t.Fatalf("expected 0 IM calls, got %d", imc.n)
	}
}
