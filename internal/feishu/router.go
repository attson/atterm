// router.go is the inbound dispatcher for the remote terminal. It accepts
// already-decrypted, already-verified envelopes and turns them into TypeIn
// frames against the right session.
//
// Permission rule (spec §Binding & permission): the event's operator
// open_id MUST equal the anchor's OwnerOpenID. On mismatch the input is
// dropped and a toast is returned; the caller surfaces the toast through
// the card.action response.
//
// Budget: every Route* call returns in ≤500ms by construction — all the
// real work (CardIndex lookup, ClaimDriver, SendInbound enqueue) is local
// and non-blocking. The remaining 2.5s of Feishu's 3-second callback
// window is reserved for the async anchor PATCH on the caller side.
package feishu

import (
	"sync/atomic"
	"time"
)

// Action is what the router decided to do.
type Action int

const (
	ActionInject  Action = iota // happy path: text was injected, ack with empty toast
	ActionReject                // do not inject; surface Toast to user
	ActionPreempt               // would inject but conflicts with current driver
)

// Decision is the router's verdict. The HTTP/callback layer translates
// it into a card.action response payload.
type Decision struct {
	Action            Action
	Toast             string
	PreemptDriverName string
}

// Subscriber is the minimal interface the router needs from a FeishuSubscriber.
// Defined here (not in subscriber.go) so router tests don't pull in session.
type Subscriber interface {
	ClaimDriver()
	SendInput([]byte) bool
	OwnerOpenID() string
	// CurrentDriverName returns the human-readable name of the session's
	// current driver, or "" when there's no driver / Feishu is already driver.
	CurrentDriverName() string
}

// SubscriberLookup returns the FeishuSubscriber currently attached to a
// session, or nil if none. The router holds no state itself; the lookup
// is provided by the wiring layer (relay_host).
type SubscriberLookup func(sessionID string) Subscriber

type Router struct {
	idx    *CardIndex
	lookup SubscriberLookup
}

func NewRouter(idx *CardIndex, lookup SubscriberLookup) *Router {
	return &Router{idx: idx, lookup: lookup}
}

// RouteReply handles an im.message.receive_v1 event whose content is a
// text reply to a card. msgID is the anchor card's msg_id (extracted by
// the caller from reply_in_thread_id or parent_id).
func (r *Router) RouteReply(msgID, operatorOpenID, text string) Decision {
	anchor := r.idx.ByMsgID(msgID)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "找不到对应会话，请通过新锚卡操作"}
	}
	return r.injectInto(anchor, operatorOpenID, []byte(text), true /*submitAfter*/)
}

// RouteCardAction handles a card.action.trigger event. kind is the value
// of action.value.kind ("input" | "key" | "end"). event names the key for
// kind=key. text is the input text for kind=input.
func (r *Router) RouteCardAction(cardToken, operatorOpenID, kind, event, text string) Decision {
	anchor := r.idx.ByCardToken(cardToken)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "卡片已过期，请通过新指令重启"}
	}
	return r.routeCardActionWith(anchor, operatorOpenID, kind, event, text)
}

// RouteCardActionBySession handles a card.action.trigger event using the
// action's session_id to look up the anchor. This is the preferred path
// when the CardToken from the envelope is empty or doesn't match our index
// (e.g. old-style cards whose CardToken was set to msg_id).
func (r *Router) RouteCardActionBySession(sessionID, operatorOpenID, kind, event, text string) Decision {
	anchor := r.idx.BySessionID(sessionID)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "卡片已过期，请通过新指令重启"}
	}
	return r.routeCardActionWith(anchor, operatorOpenID, kind, event, text)
}

// routeCardActionWith dispatches a card action against a resolved anchor.
func (r *Router) routeCardActionWith(anchor *CardAnchor, operatorOpenID, kind, event, text string) Decision {
	switch kind {
	case "input":
		if text == "" {
			return Decision{Action: ActionReject, Toast: ""}
		}
		return r.injectInto(anchor, operatorOpenID, []byte(text), true /*submitAfter*/)
	case "key":
		b := keyBytes(event)
		if b == nil {
			return Decision{Action: ActionReject, Toast: "未知按键"}
		}
		return r.injectInto(anchor, operatorOpenID, b, false /*submitAfter*/)
	case "end":
		if operatorOpenID != anchor.OwnerOpenID {
			return Decision{Action: ActionReject, Toast: "无权限"}
		}
		return Decision{Action: ActionInject}
	default:
		return Decision{Action: ActionReject, Toast: "未知交互"}
	}
}

func (r *Router) injectInto(anchor *CardAnchor, operatorOpenID string, payload []byte, submitAfter bool) Decision {
	if operatorOpenID != anchor.OwnerOpenID {
		return Decision{Action: ActionReject, Toast: "无权限"}
	}
	sub := r.lookup(anchor.SessionID)
	if sub == nil {
		return Decision{Action: ActionReject, Toast: "会话已结束"}
	}
	// Silent takeover: previously a non-empty CurrentDriverName returned
	// ActionPreempt without claiming, but handleCardAction never surfaced
	// preempt as a user-visible toast — so Feishu replies silently vanished
	// whenever the local atterm window was the driver. Until a real multi-
	// driver UX ships, treat Feishu input as "the user is in Feishu because
	// they aren't at the desk; take over".
	sub.ClaimDriver()
	if !sub.SendInput(payload) {
		return Decision{Action: ActionReject, Toast: "输入未被接收（队列已满）"}
	}
	// AI TUIs (claude/codex) read a bundled "text\r" or "text\n" as a PASTE,
	// not as type-then-submit — the text lands in the input buffer but is
	// never committed. The fix is to send the CR as a second, delayed
	// SendInput so the TUI's input loop sees it as a discrete "Enter"
	// keystroke. 16ms matches the proven split in the desktop template flow
	// (see feedback_template_send_split_cr).
	if submitAfter {
		go func() {
			time.Sleep(16 * time.Millisecond)
			sub.SendInput([]byte{0x0d})
		}()
	}
	return Decision{Action: ActionInject}
}

// InjectKeystrokesBySession fires a sequence of SendInput calls spaced by
// interKeyDelay so the receiving TUI reads each stroke as a discrete key
// event. A bundled payload (single SendInput with multiple bytes) reads as
// a paste in claude/codex — the \r inside becomes literal \n and never
// commits, so multi-step flows (AskUserQuestion, TUI menus) need every key
// on its own SendInput. Ownership + take-over match injectInto.
//
// Sequencing is asynchronous: the function returns after ClaimDriver + the
// first SendInput queues, and the rest fire from a goroutine. Callers that
// need to know the whole sequence landed have to observe pty output.
func (r *Router) InjectKeystrokesBySession(sessionID, operatorOpenID string, strokes [][]byte, interKeyDelay time.Duration) Decision {
	anchor := r.idx.BySessionID(sessionID)
	if anchor == nil {
		return Decision{Action: ActionReject, Toast: "卡片已过期，请通过新指令重启"}
	}
	if operatorOpenID != anchor.OwnerOpenID {
		return Decision{Action: ActionReject, Toast: "无权限"}
	}
	sub := r.lookup(anchor.SessionID)
	if sub == nil {
		return Decision{Action: ActionReject, Toast: "会话已结束"}
	}
	sub.ClaimDriver()
	if len(strokes) == 0 {
		return Decision{Action: ActionInject}
	}
	// Send the first stroke inline so a caller-visible error surfaces if the
	// PTY inbound queue is completely full at this instant.
	if !sub.SendInput(strokes[0]) {
		return Decision{Action: ActionReject, Toast: "输入未被接收（队列已满）"}
	}
	if len(strokes) == 1 {
		return Decision{Action: ActionInject}
	}
	go func(rest [][]byte) {
		for _, s := range rest {
			time.Sleep(interKeyDelay)
			sub.SendInput(s)
		}
	}(strokes[1:])
	return Decision{Action: ActionInject}
}

// CardTokenFor returns the live CardKit card_token for a session, or "" when
// no anchor is registered. Exposed so the post-input-submit clear flow can
// look up the token without duplicating the index dependency.
func (r *Router) CardTokenFor(sessionID string) string {
	a := r.idx.BySessionID(sessionID)
	if a == nil {
		return ""
	}
	return a.CardToken
}

// NextPatchSeq atomically bumps the per-anchor sequence counter and returns
// the new value. Returns 0 (and is a no-op) when no anchor matches.
func (r *Router) NextPatchSeq(sessionID string) int64 {
	a := r.idx.BySessionID(sessionID)
	if a == nil {
		return 0
	}
	return atomic.AddInt64(&a.PatchSeq, 1)
}

// AnchorBySession returns the *CardAnchor for a session, or nil if none.
// Exposed so callers that need to hold the per-anchor SendMu across a
// multi-step op (e.g. clear-input's DELETE + CREATE) can do so directly.
func (r *Router) AnchorBySession(sessionID string) *CardAnchor {
	return r.idx.BySessionID(sessionID)
}

// keyBytes maps button event names to the raw bytes injected to the PTY.
func keyBytes(event string) []byte {
	switch event {
	case "ctrl_c":
		return []byte{0x03}
	case "ctrl_d":
		return []byte{0x04}
	case "esc":
		return []byte{0x1B}
	case "enter":
		return []byte{0x0D}
	default:
		return nil
	}
}
