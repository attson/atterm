// subscriber.go wraps session.Subscriber so the Feishu outbound chunker can
// drain PTY frames without auto-claiming driver.
//
// Lifecycle:
//
//	AttachFeishuSubscriber → session.Subscribe(opts: WithoutAutoDrive)
//	  - starts a goroutine that drains sub.Out() and feeds the chunker
//	  - starts a Tick goroutine to keep the chunker flushing on idle
//	FeishuSubscriber.ClaimDriver()
//	  - called on first inbound input (via router.go) to promote to driver
//	FeishuSubscriber.SendInput(text)
//	  - encodes a TypeIn frame and pushes to session.SendInbound
//	FeishuSubscriber.Detach()
//	  - unsubscribes from session, stops goroutines, archives anchor
//	  - chunker keeps any unflushed state to drop on the floor (best-effort)
package feishu

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
)

// FeishuSubscriber is a single per-session attach.
type FeishuSubscriber struct {
	sess  *session.Session
	sub   *session.Subscriber
	chunk *Chunker

	openID     string // anchor owner, recorded for permission gate at attach
	clientID   string // unique per attach, used in driver meta
	clientName string

	tick   chan struct{} // timer signals sent to drainLoop (sole Chunker owner)
	done   chan struct{}
	wg     sync.WaitGroup
	closed atomic.Bool
}

// AttachFeishuSubscriber subscribes to sess as a passive viewer (no auto-drive)
// and wires the bytes into a Chunker that calls flush whenever a PATCH-worthy
// update is ready. The caller owns flush — it should be non-blocking and
// post the actual HTTP call asynchronously (the chunker runs on the drain
// goroutine and blocking would back-pressure PTY fan-out).
//
// The Chunker is not goroutine-safe; all PushBytes and Tick calls are
// serialized on a single drainLoop goroutine. A separate tickLoop sends
// signals to drainLoop over a buffered channel so the timer never blocks.
func AttachFeishuSubscriber(sess *session.Session, ownerOpenID string, flush FlushFunc) *FeishuSubscriber {
	sub, _ := sess.Subscribe(0, "feishu:"+sess.ID.String(), "feishu-bot", session.WithoutAutoDrive())
	fs := &FeishuSubscriber{
		sess:       sess,
		sub:        sub,
		chunk:      NewChunker(flush),
		openID:     ownerOpenID,
		clientID:   "feishu:" + sess.ID.String(),
		clientName: "feishu-bot",
		tick:       make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
	fs.wg.Add(2)
	go fs.drainLoop()
	go fs.tickLoop()
	return fs
}

// drainLoop is the sole goroutine that calls Chunker methods, so no
// synchronisation is needed on the Chunker itself.
func (f *FeishuSubscriber) drainLoop() {
	defer f.wg.Done()
	for {
		select {
		case <-f.done:
			return
		case <-f.sub.Done():
			return
		case <-f.tick:
			f.chunk.Tick()
		case frame, ok := <-f.sub.Out():
			if !ok {
				return
			}
			if frame.Type == proto.TypeOut {
				f.chunk.PushBytes(frame.Payload)
			}
		}
	}
}

// tickLoop fires chunkerFlushPeriod signals to drainLoop. It never touches
// the Chunker directly, eliminating the goroutine-safety concern.
func (f *FeishuSubscriber) tickLoop() {
	defer f.wg.Done()
	t := time.NewTicker(chunkerFlushPeriod)
	defer t.Stop()
	for {
		select {
		case <-f.done:
			return
		case <-t.C:
			// Non-blocking send: if drainLoop is busy processing a frame it
			// will pick this up next iteration; a missed tick is fine since
			// PushBytes already triggers a flush at chunkerBufferBytes.
			select {
			case f.tick <- struct{}{}:
			default:
			}
		}
	}
}

// ClaimDriver promotes this Feishu attach to the session's active driver.
// Idempotent: a second call when already driver is a no-op (Session handles it).
func (f *FeishuSubscriber) ClaimDriver() {
	f.sess.ClaimDriver(f.sub, f.clientID, f.clientName)
}

// SendInput pushes a TypeIn frame to the session. Returns true on success.
// false means the inbound queue is full — caller should toast the user.
func (f *FeishuSubscriber) SendInput(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	return f.sess.SendInbound(proto.Frame{
		Type:      proto.TypeIn,
		SessionID: f.sess.ID,
		Payload:   data,
	})
}

// OwnerOpenID returns the open_id recorded at attach (used by router for
// permission gate).
func (f *FeishuSubscriber) OwnerOpenID() string { return f.openID }

// Detach unsubscribes from the session and stops goroutines. Idempotent.
func (f *FeishuSubscriber) Detach() {
	if !f.closed.CompareAndSwap(false, true) {
		return
	}
	close(f.done)
	f.sess.Unsubscribe(f.sub)
	f.wg.Wait()
}

// feishuDriverClientID exposes the synthetic client ID for tests.
func feishuDriverClientID(f *FeishuSubscriber) string { return f.clientID }
