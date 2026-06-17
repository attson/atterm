package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeStore struct {
	hooks []Webhook
	err   error
}

func (f *fakeStore) ListWebhooks(_ context.Context, _ string) ([]Webhook, error) {
	return f.hooks, f.err
}

func TestDispatchFansOutToAllWebhooks(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	mk := func(tag string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits[tag]++
			mu.Unlock()
			w.WriteHeader(200)
		}))
	}
	s1 := mk("a")
	defer s1.Close()
	s2 := mk("b")
	defer s2.Close()

	store := &fakeStore{hooks: []Webhook{
		{ID: "1", URL: s1.URL, Format: "generic"},
		{ID: "2", URL: s2.URL, Format: "generic"},
	}}
	svc := New(store)
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New(), HostID: uuid.New(), Label: "x"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := hits["a"] == 1 && hits["b"] == 1
		mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected both webhooks hit once, got %v", hits)
}

func TestDispatchReturnsImmediately(t *testing.T) {
	store := &fakeStore{hooks: []Webhook{{ID: "1", URL: "http://127.0.0.1:1/slow", Format: "generic"}}}
	svc := New(store)
	start := time.Now()
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New()})
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("DispatchCommandFinished should return immediately (fan-out is async)")
	}
}

func TestDispatchStoreErrorIsNoOp(t *testing.T) {
	store := &fakeStore{err: context.DeadlineExceeded}
	svc := New(store)
	svc.DispatchCommandFinished("u1", CommandFinished{SessionID: uuid.New()}) // must not panic
}
