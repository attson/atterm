package feishu

import (
	"sync"
	"testing"
	"time"
)

func TestCardIndex_RoundTrip(t *testing.T) {
	idx := NewCardIndex()
	anchor := &CardAnchor{
		SessionID:   "sess1",
		CardMsgID:   "msg1",
		CardToken:   "tok1",
		OwnerOpenID: "ou_abc",
		CreatedAt:   time.Now(),
	}
	idx.Put(anchor)

	if got := idx.BySessionID("sess1"); got != anchor {
		t.Errorf("BySessionID = %v, want %v", got, anchor)
	}
	if got := idx.ByMsgID("msg1"); got != anchor {
		t.Errorf("ByMsgID = %v, want %v", got, anchor)
	}
	if got := idx.ByCardToken("tok1"); got != anchor {
		t.Errorf("ByCardToken = %v, want %v", got, anchor)
	}
}

func TestCardIndex_Remove(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t"})
	idx.RemoveBySessionID("s")
	if got := idx.BySessionID("s"); got != nil {
		t.Errorf("BySessionID after remove = %v, want nil", got)
	}
	if got := idx.ByMsgID("m"); got != nil {
		t.Errorf("ByMsgID after remove = %v, want nil", got)
	}
}

func TestCardIndex_ConcurrentSafe(t *testing.T) {
	idx := NewCardIndex()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			idx.Put(&CardAnchor{SessionID: string(rune(i)), CardMsgID: string(rune(i + 1000)), CardToken: string(rune(i + 2000))})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = idx.BySessionID(string(rune(i)))
		}(i)
	}
	wg.Wait()
}
