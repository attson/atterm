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
	return r.injectInto(anchor, operatorOpenID, []byte(text+"\n"))
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
		return r.injectInto(anchor, operatorOpenID, []byte(text+"\n"))
	case "key":
		b := keyBytes(event)
		if b == nil {
			return Decision{Action: ActionReject, Toast: "未知按键"}
		}
		return r.injectInto(anchor, operatorOpenID, b)
	case "end":
		if operatorOpenID != anchor.OwnerOpenID {
			return Decision{Action: ActionReject, Toast: "无权限"}
		}
		return Decision{Action: ActionInject}
	default:
		return Decision{Action: ActionReject, Toast: "未知交互"}
	}
}

func (r *Router) injectInto(anchor *CardAnchor, operatorOpenID string, payload []byte) Decision {
	if operatorOpenID != anchor.OwnerOpenID {
		return Decision{Action: ActionReject, Toast: "无权限"}
	}
	sub := r.lookup(anchor.SessionID)
	if sub == nil {
		return Decision{Action: ActionReject, Toast: "会话已结束"}
	}
	// In Phase 1 we always claim driver on first input — preempt protocol
	// arrives in Task 16 (Phase 2). For now any input promotes Feishu.
	sub.ClaimDriver()
	if !sub.SendInput(payload) {
		return Decision{Action: ActionReject, Toast: "输入未被接收（队列已满）"}
	}
	return Decision{Action: ActionInject}
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
