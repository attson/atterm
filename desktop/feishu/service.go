package feishu

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

type Mode int

const (
	ModeRelay Mode = iota
	ModeLocal
)

// ServiceConfig assembles all the moving parts at startup.
type ServiceConfig struct {
	Mode Mode

	// Relay mode:
	RelayURL   string
	RelayToken func() string
	// RelayHTTPClient is the client used for relay REST calls (binding store +
	// borrowed-token source). Production wires the insecure-capable, ALPN-pinned
	// relay client here so a self-signed relay works. Nil falls back to HTTPClient.
	RelayHTTPClient *http.Client

	// Local mode:
	FeishuBase string
	HTTPClient *http.Client

	// Optional clock override for tests.
	Now func() int64

	// Sessions is the registry the hook server uses for existence checks.
	Sessions SessionLookup
}

// Service is the top-level façade.
type Service struct {
	cfg ServiceConfig

	store      BindingStore
	tokenSrc   TokenSource
	imClient   IMClient
	dispatcher *Dispatcher
	hookSrv    *HookServer

	lcMu     sync.Mutex // guards longConn (EnsureLongConn / CloseLongConn)
	longConn *LongConn

	routerMu sync.RWMutex
	router   *internalfeishu.Router
}

// SetRouter installs the inbound router used by handleReplyMessage and
// handleCardAction. Called by relay_host (via app.go) once the CardIndex and
// FeishuSubscriber registry are built. Safe to call concurrently; nil clears
// the router so anchor card actions are no longer routed (existing inject/ack
// paths in handleCardAction are unaffected).
func (s *Service) SetRouter(r *internalfeishu.Router) {
	s.routerMu.Lock()
	s.router = r
	s.routerMu.Unlock()
}

func (s *Service) getRouter() *internalfeishu.Router {
	s.routerMu.RLock()
	defer s.routerMu.RUnlock()
	return s.router
}

// replyText sends a plain text message back to openID using the dispatcher's
// token source and IM client. Best-effort: errors are logged and swallowed.
func (s *Service) replyText(ctx context.Context, openID, text string) {
	if s.dispatcher == nil {
		return
	}
	tok, _, err := s.dispatcher.GetToken(ctx)
	if err != nil {
		log.Printf("feishu: replyText get token: %v", err)
		return
	}
	if err := s.imClient.SendTextToOpenID(ctx, tok, openID, text); err != nil {
		log.Printf("feishu: replyText send to %s: %v", openID, err)
	}
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Mode == ModeRelay && cfg.RelayToken == nil {
		return nil, errors.New("desktop/feishu: relay mode requires RelayToken func")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.FeishuBase == "" {
		cfg.FeishuBase = "https://open.feishu.cn"
	}

	var store BindingStore
	var ts TokenSource
	switch cfg.Mode {
	case ModeRelay:
		// Relay REST calls go through RelayHTTPClient (insecure-capable +
		// ALPN-pinned in production); fall back to the general HTTPClient.
		relayClient := cfg.RelayHTTPClient
		if relayClient == nil {
			relayClient = cfg.HTTPClient
		}
		rs := NewRelayBackedBindingStore(cfg.RelayURL, cfg.RelayToken)
		rs.client = relayClient
		store = rs
		rts := NewRelayBorrowedTokenSource(cfg.RelayURL, cfg.RelayToken)
		rts.client = relayClient
		ts = rts
	case ModeLocal:
		ls := NewLocalKeychainBindingStore()
		store = ls
		ts = NewLocalTenantTokenSource(ls, cfg.FeishuBase, cfg.HTTPClient, func() time.Time { return time.Now() })
	default:
		return nil, errors.New("desktop/feishu: invalid mode")
	}

	im := &authClassAdaptingClient{inner: internalfeishu.NewClient(cfg.FeishuBase, cfg.HTTPClient)}

	d := NewDispatcher(DispatcherConfig{
		Store:   store,
		Token:   ts,
		IM:      im,
		CardKit: im,
		Now:     cfg.Now,
	})

	if cfg.Sessions == nil {
		cfg.Sessions = noOpSessionLookup{}
	}
	sessions := cfg.Sessions
	hookSrv := NewHookServer(d, sessions)

	return &Service{
		cfg: cfg, store: store, tokenSrc: ts,
		imClient: im, dispatcher: d, hookSrv: hookSrv,
	}, nil
}

func (s *Service) Store() BindingStore     { return s.store }
func (s *Service) Dispatcher() *Dispatcher { return s.dispatcher }
func (s *Service) HookServer() *HookServer { return s.hookSrv }
func (s *Service) Token() TokenSource      { return s.tokenSrc }

// Exists makes Service satisfy SessionLookup for embedded use; production
// callers pass an external SessionLookup via ServiceConfig.
func (s *Service) Exists(uuid.UUID) bool { return true }

// Inject satisfies SessionLookup for embedded use; production callers pass an
// external SessionLookup via ServiceConfig.
func (s *Service) Inject(uuid.UUID, string) error { return nil }

// EnsureLongConn starts the long-conn lazily once credentials exist.
// No-op in relay mode.
func (s *Service) EnsureLongConn(ctx context.Context) error {
	if s.cfg.Mode != ModeLocal {
		return nil
	}
	s.lcMu.Lock()
	defer s.lcMu.Unlock()
	v, err := s.store.Get(ctx)
	if err != nil {
		return err
	}
	if v.AppID == "" || v.AppSecret == "" {
		return errors.New("desktop/feishu: credentials missing")
	}
	if s.longConn != nil {
		return nil
	}
	lc := NewLongConn(LongConnConfig{
		AppID:     v.AppID,
		AppSecret: v.AppSecret,
		Backoff:   BackoffConfig{Initial: time.Second, Max: 5 * time.Minute},
		OnBindMessage: func(ctx context.Context, senderOpenID, text string) {
			s.handleBindMessage(ctx, senderOpenID, text)
		},
		OnReplyMessage: func(ctx context.Context, senderOpenID, parentID, text string) {
			s.handleReplyMessage(ctx, senderOpenID, parentID, text)
		},
		OnCardAction: func(ctx context.Context, sessionID, kind, event, operatorOpenID, text string, formValue map[string]any) {
			s.handleCardAction(ctx, sessionID, kind, event, operatorOpenID, text, formValue)
		},
		OnAuthClassFailure: func(ctx context.Context, _ error) {
			_ = s.store.SetDisabled(ctx)
		},
	})
	if err := lc.Start(ctx); err != nil {
		return err
	}
	s.longConn = lc
	return nil
}

// CloseLongConn stops the long connection if one is running and clears it.
// Safe to call in any mode (no-op when there is no long-conn, e.g. relay
// mode). Used when the service is being replaced on a relay login/logout.
func (s *Service) CloseLongConn(ctx context.Context) error {
	s.lcMu.Lock()
	defer s.lcMu.Unlock()
	if s.longConn == nil {
		return nil
	}
	err := s.longConn.Close(ctx)
	s.longConn = nil
	return err
}

// RenderAck returns the ack-update card the long-conn's card-action
// handler echoes back to Feishu. Pulled out so tests don't need the SDK.
func (s *Service) RenderAck(event, sessionID string) internalfeishu.AckResponse {
	return internalfeishu.RenderAckUpdateCard(internalfeishu.AckUpdateInput{
		Event: event, SessionID: sessionID,
	})
}

func (s *Service) handleBindMessage(ctx context.Context, senderOpenID, text string) {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "/bind ") {
		return
	}
	code := strings.TrimSpace(strings.TrimPrefix(t, "/bind "))
	if !s.consumePending(code) {
		// Mirror the relay path's user feedback (internal/feishu/service.go) so
		// local-mode binds aren't silent on an invalid/expired short code.
		s.replyText(ctx, senderOpenID, "❌ 短码无效或已过期")
		return
	}
	if err := s.store.SetBound(ctx, senderOpenID); err != nil {
		log.Printf("feishu: bind SetBound: %v", err)
		s.replyText(ctx, senderOpenID, "❌ 服务端错误,请稍后再试")
		return
	}
	s.replyText(ctx, senderOpenID, "✅ 已绑定到 atterm")
}

// handleReplyMessage routes a Feishu reply (quoting a previously sent card) back
// into the originating session's PTY. parentID is the replied-to card's
// message_id, looked up in the dispatcher's message→session map.
//
// ModeLocal only: ModeRelay free-text reply is not yet wired (cross-process map).
// The relay process holds no copy of this in-process cardMsgs map, so relay users
// must use the card's quick-action buttons instead.
func (s *Service) handleReplyMessage(ctx context.Context, senderOpenID, parentID, text string) {
	t := strings.TrimSpace(text)
	if parentID == "" || t == "" {
		return
	}
	if strings.HasPrefix(t, "/bind ") {
		return // a bind command, not a reply-to-card
	}

	// Anchor card routing takes precedence: if a Router is installed and the
	// replied-to message is an anchor card, let the router handle it (permission
	// gate + subscriber inject). Fall through to the legacy cardMsgs path only
	// when the router is nil or doesn't know the card.
	if r := s.getRouter(); r != nil {
		decision := r.RouteReply(parentID, senderOpenID, t)
		switch decision.Action {
		case internalfeishu.ActionInject:
			return // router handled it; done
		case internalfeishu.ActionReject:
			if decision.Toast != "" {
				s.replyText(ctx, senderOpenID, decision.Toast)
			}
			return
		case internalfeishu.ActionPreempt:
			// Phase 2; treat as reject with toast for now.
			if decision.Toast != "" {
				s.replyText(ctx, senderOpenID, decision.Toast)
			}
			return
		}
		// If the router returned an unknown action (shouldn't happen), fall through.
	}

	// Legacy fallback: look up the card in the dispatcher's in-process map and
	// inject directly via Sessions. This path covers local-mode non-anchor cards
	// (e.g. WaitingInput cards sent before anchor cards were introduced).
	if s.dispatcher == nil {
		return
	}
	sid, ok := s.dispatcher.LookupCardSession(parentID)
	if !ok {
		return
	}
	if err := s.cfg.Sessions.Inject(sid, text+"\n"); err != nil {
		log.Printf("feishu: reply inject session=%s: %v", sid, err)
	}
}

func (s *Service) handleCardAction(ctx context.Context, sessionID, kind, event, operatorOpenID, text string, formValue map[string]any) {
	textLen := len(text)
	log.Printf("feishu-card-action: sid=%s kind=%s event=%q op=%s text_len=%d form_fields=%d",
		sessionID, kind, event, operatorOpenID, textLen, len(formValue))
	switch kind {
	case "form":
		s.handleAskFormSubmit(ctx, sessionID, operatorOpenID, formValue)
	case "input", "key", "end":
		// Anchor card actions: route through the inbound router.
		r := s.getRouter()
		if r == nil {
			return // anchor card routing not configured; drop silently
		}
		decision := r.RouteCardActionBySession(sessionID, operatorOpenID, kind, event, text)
		switch decision.Action {
		case internalfeishu.ActionInject:
			// Happy path. For input submissions, PUT a fresh input element
			// so the visible textbox clears — Feishu V2 input doesn't
			// auto-clear, and a PATCH default_value:"" is a client-side
			// no-op once user has typed. See Dispatcher.ClearAnchorInput.
			if kind == "input" {
				// Grab the anchor directly so clearAnchorInput can hold the
				// per-anchor SendMu across DELETE + CREATE — seq allocation
				// alone isn't enough, we also need to keep other body/button
				// flushes from squeezing between the two ops and bumping
				// the "last seen" sequence past our reserved slots.
				if a := r.AnchorBySession(sessionID); a != nil {
					go s.clearAnchorInput(a, sessionID)
				}
			}
		case internalfeishu.ActionReject:
			if decision.Toast != "" && operatorOpenID != "" {
				s.replyText(ctx, operatorOpenID, decision.Toast)
			}
		case internalfeishu.ActionPreempt:
			// Phase 2; treat as reject with toast for now.
			if decision.Toast != "" && operatorOpenID != "" {
				s.replyText(ctx, operatorOpenID, decision.Toast)
			}
		}
	case "inject":
		// Legacy AskQuestion inject path (PR #250): inject text directly into
		// the session via SessionLookup. Keep existing behaviour intact.
		if text == "" {
			return
		}
		sid, err := uuid.Parse(sessionID)
		if err != nil {
			log.Printf("feishu: card inject bad session_id %q: %v", sessionID, err)
			return
		}
		if err := s.cfg.Sessions.Inject(sid, text); err != nil {
			log.Printf("feishu: card inject session=%s: %v", sid, err)
		}
	default:
		// Unknown kind; ignore.
	}
}

// handleAskFormSubmit turns an AskUserQuestion form submit into the exact
// key sequence claude's TUI expects, then injects it via the router. The
// TUI is keyboard-driven — "Enter to select · Tab/Arrow keys to navigate"
// — so pasting the answer as text just makes claude accept the first
// keystroke ('1') as Q1's selection and drop everything after. What works
// is the actual key sequence a human would type: digit-key per question,
// Tab to move to the next question / to Submit, Enter to commit.
//
// Custom text ("Type something." in the native TUI) is a nested modal we
// haven't reverse-engineered yet; if the user typed anything into a
// per-question txt input we reply-message a "not supported" note back and
// leave the form up so they can re-pick from the dropdown.
func (s *Service) handleAskFormSubmit(ctx context.Context, sessionID, operatorOpenID string, formValue map[string]any) {
	r := s.getRouter()
	if r == nil {
		return
	}
	anchor := r.AnchorBySession(sessionID)
	if anchor == nil {
		log.Printf("feishu: askform submit no-anchor sid=%s", sessionID)
		return
	}
	// Snapshot the questions the mounted form was asking about — writer
	// (relay_host.updateAnchorAskForm) holds SendMu across the CREATE, so
	// this read races only with a concurrent form tear-down, which would
	// have DELETEd the form on the Feishu side too. Copy to avoid holding
	// the lock while we do the label→index math.
	anchor.SendMu.Lock()
	questions := anchor.PendingForm
	anchor.SendMu.Unlock()
	// Log the exact questions we mounted vs. what the user picked, so a
	// "stroke count doesn't match claude's tab count" mismatch is visible
	// in the log next to the stroke plan.
	for i, q := range questions {
		log.Printf("feishu: askform pending sid=%s q[%d]=%q opts=%d", sessionID, i, q.Question, len(q.Options))
	}
	for k, v := range formValue {
		log.Printf("feishu: askform formvalue sid=%s field=%q value=%v", sessionID, k, v)
	}
	if len(questions) == 0 {
		log.Printf("feishu: askform submit no-pending-form sid=%s (form was probably already dismissed)", sessionID)
		return
	}
	slots := parseAskFormSlots(formValue)
	if len(slots) == 0 {
		log.Printf("feishu: askform empty submit sid=%s", sessionID)
		return
	}
	// Reverse-engineered key model (2026-07-08, feedback_askform_key_model.md):
	//
	//   Single-select tab:
	//     digit N = pick option N + confirm + auto-advance. ONE stroke.
	//
	//   Multi-select tab:
	//     digit N = toggle checkbox N (does NOT advance)
	//     Tab (\t) or → = confirm this question + advance
	//
	//   Type-something (custom text) branch (present on every question,
	//   position = len(Options)+1):
	//     digit typeIdx only ticks the box, does NOT focus the input.
	//     Need ↓ (\x1b[B) × (typeIdx-1) to move the cursor down to the
	//     Type-something row before typing takes effect. Then Tab advances.
	//
	//   After the last question, Review page: "1. Submit answers /
	//   2. Cancel". Digit `1` fires.
	strokes := make([][]byte, 0)
	for _, sl := range slots {
		if sl.idx >= len(questions) {
			log.Printf("feishu: askform slot idx=%d out of range (q_count=%d) sid=%s", sl.idx, len(questions), sessionID)
			return
		}
		q := questions[sl.idx]
		qStrokes, ok := buildQuestionStrokes(q, sl, sessionID)
		if !ok {
			return
		}
		strokes = append(strokes, qStrokes...)
	}
	// Review page: "Submit answers" is option 1.
	strokes = append(strokes, []byte{'1'})
	// 80ms between strokes: claude's AskUserQuestion (ink) needs ~50-70ms to
	// re-render on tab-advance, and a stroke that arrives during the re-
	// render can land in the wrong tab (or leak into chat as a queued
	// message). 30ms was tight enough to leave the trailing digit in chat.
	// Log the full stroke plan (byte-for-byte) so misfires are debuggable
	// without another manual capture.
	var dbg strings.Builder
	for i, s := range strokes {
		if i > 0 {
			dbg.WriteByte(' ')
		}
		fmt.Fprintf(&dbg, "%x", s)
	}
	log.Printf("feishu: askform plan sid=%s slots=%d strokes=%d bytes=[%s]", sessionID, len(slots), len(strokes), dbg.String())
	// 350ms inter-key delay. Enough for the sync-required cases (single
	// + custom's Enter reads siH's local synchronous state; multi +
	// custom now uses Ctrl+Return which stays on siH — no blur race).
	// A previous 700ms bump was chasing a race that couldn't be fixed
	// by delay (session e38a4b41 lost ASCII "codex" → "code" at 700ms);
	// swapping the advance mechanism removed the race outright, so we
	// don't need the slow delay.
	decision := r.InjectKeystrokesBySession(sessionID, operatorOpenID, strokes, 350*time.Millisecond)
	log.Printf("feishu: askform submit sid=%s strokes=%d q=%d action=%d", sessionID, len(strokes), len(slots), decision.Action)
	// Remove the form once injected — success or reject, the form has done
	// its job (a rejected submit indicates a permission / session issue,
	// not a bad form; leaving the form up would just confuse).
	go func() {
		if s.dispatcher == nil {
			return
		}
		s.deleteAnchorForm(anchor)
	}()
}

// buildQuestionStrokes assembles the keypress sequence for one question of
// an AskUserQuestion form based on its slot (what the user picked in Feishu)
// and its shape (single vs multi select). Returns (strokes, false) with an
// error logged if something is off (label not found, index > 9, etc.); the
// caller aborts the whole submit on false.
//
// Branches:
//   - Single-select, no custom text: one digit does everything (select +
//     confirm + advance). Do NOT add Tab.
//   - Multi-select, no custom text: digit for each checked label (toggle),
//     then Tab to advance.
//   - Any + custom text: (if multi, toggle other checkboxes first,) then
//     the Type-something checkbox digit, then ↓ × (typeIdx-1) to move
//     cursor onto the Type-something row, then the text bytes, then Tab.
func buildQuestionStrokes(q internalfeishu.AskFormQuestion, sl askFormSlot, sessionID string) ([][]byte, bool) {
	var out [][]byte
	findOptIdx := func(label string) int {
		for i, opt := range q.Options {
			if opt.Label == label {
				return i + 1
			}
		}
		return 0
	}
	hasCustom := sl.txt != ""

	// Single-select fast path (one keystroke handles everything).
	if !q.MultiSelect && !hasCustom {
		if sl.sel == "" {
			log.Printf("feishu: askform single-select empty sid=%s q=%d", sessionID, sl.idx)
			return nil, false
		}
		optIdx := findOptIdx(sl.sel)
		if optIdx == 0 || optIdx > 9 {
			log.Printf("feishu: askform label not-found or >9 options sid=%s q=%d label=%q", sessionID, sl.idx, sl.sel)
			return nil, false
		}
		out = append(out, []byte{'0' + byte(optIdx)})
		return out, true
	}

	// Multi-select: toggle each selected label with its digit.
	if q.MultiSelect {
		for _, label := range sl.selMulti {
			optIdx := findOptIdx(label)
			if optIdx == 0 || optIdx > 9 {
				log.Printf("feishu: askform multi label not-found or >9 options sid=%s q=%d label=%q", sessionID, sl.idx, label)
				return nil, false
			}
			out = append(out, []byte{'0' + byte(optIdx)})
		}
	}

	// Custom text: press the Type-something digit, then type. Type-something
	// lives at len(Options)+1 (right after the last real option, before
	// "Chat about this").
	//
	// Single vs multi differs sharply here:
	//   - Single-select: digit typeIdx MOVES the cursor onto the Type-
	//     something row AND drops into text-entry mode. Just type; no ↓
	//     needed. (Verified via image #79/#80: pressing '4' put the cursor
	//     on "▸ 4. Type something." with an inline caret, then typing
	//     "badka" filled it inline.)
	//   - Multi-select: digit typeIdx only TICKS the checkbox; cursor
	//     stays on option 1. Have to ↓ × (typeIdx-1) to walk down to the
	//     Type-something row before typing takes effect. THEN one more ↓
	//     to reach "Chat about this" (the last option) — Tab only
	//     advances the tab when the cursor sits on the last option.
	//     Verified from binary section 18 multi-select handler:
	//       if (C.key === "tab" && !C.shift) {
	//         if (y.focusedValue === b /* b = last option */) P(!0);  // advance
	//         else y.focusNextOption();                               // just move
	//       }
	if hasCustom {
		typeIdx := len(q.Options) + 1
		if typeIdx > 9 {
			log.Printf("feishu: askform typeIdx=%d exceeds single-digit range sid=%s q=%d", typeIdx, sessionID, sl.idx)
			return nil, false
		}
		out = append(out, []byte{'0' + byte(typeIdx)})
		if q.MultiSelect {
			for i := 0; i < typeIdx-1; i++ {
				out = append(out, []byte{0x1b, 0x5b, 0x42})
			}
		}
		// Split the text into per-rune strokes. Each rune is a separate
		// SendInput so ink processes each keypress → setState → the state
		// Map re-renders before the next stroke.
		for _, r := range sl.txt {
			out = append(out, []byte(string(r)))
		}
		if q.MultiSelect {
			// Right-arrow × 2 as no-op "state pumps". Inside siH, Right
			// arrow is text-cursor navigation (moves right if not at end,
			// otherwise no-op). Each stroke triggers a keypress event and
			// forces React to render/settle any pending setState from the
			// preceding text runes. The Ctrl+Return attempt via LF (0x0a)
			// didn't work — ink treats 0x0a as plain return. And bare
			// ↓+Enter loses the trailing char (session e38a4b41: "codex"
			// → "code" at 700ms). The Right-arrow pumps give the last
			// rune's setState a fair chance to reach the controlled prop
			// before the ↓ triggers siH.onBlur.
			out = append(out, []byte{0x1b, 0x5b, 0x43})
			out = append(out, []byte{0x1b, 0x5b, 0x43})
			out = append(out, []byte{0x1b, 0x5b, 0x42})
		}
	}

	// Advance key depends on the branch:
	//   - Multi-select, no custom text: Tab (\t / 0x09).
	//   - Single-select + custom text: Enter (0x0d) on siH → siH.onSubmit
	//     fires with siH's local (synchronous) value → Y = onChange =
	//     advance.
	//   - Multi-select + custom text: Enter (0x0d) after the ↓ (added
	//     above). Cursor is now on the Submit button; Enter fires the
	//     container's `if (X && Y) { Y(D); return; }` branch → submits
	//     form. The Right-arrow pumps preceding the ↓ give React time to
	//     propagate the last rune's setState to the controlled prop
	//     before siH.onBlur triggers on cursor move.
	if q.MultiSelect && !hasCustom {
		out = append(out, []byte{0x09})
	} else if hasCustom {
		out = append(out, []byte{0x0d})
	}
	return out, true
}

// askFormSlot is one question's parsed answer. Exactly one of sel (single-
// select dropdown label), selMulti (multi_select_static labels), or txt
// (per-question custom-text input) is populated per typical submission,
// though nothing prevents the user from filling both a dropdown AND the
// txt field — handleAskFormSubmit treats txt as an override on top of
// whatever's selected. Empty slots (nothing picked, nothing typed) are
// dropped by parseAskFormSlots before return.
type askFormSlot struct {
	idx      int
	sel      string   // single-select chosen label
	selMulti []string // multi-select chosen labels
	txt      string   // custom-text input for this question
}

// parseAskFormSlots pulls q_<idx>_sel / q_<idx>_txt out of Feishu's form
// submission map and returns them sorted by question index. sel values
// come in as either string (single_select_static) or []any (multi_select_
// static); the two land in different fields so the stroke planner can
// tell the two branches apart.
func parseAskFormSlots(formValue map[string]any) []askFormSlot {
	byIdx := map[int]*askFormSlot{}
	get := func(i int) *askFormSlot {
		s := byIdx[i]
		if s == nil {
			s = &askFormSlot{idx: i}
			byIdx[i] = s
		}
		return s
	}
	for k, v := range formValue {
		if !strings.HasPrefix(k, "q_") {
			continue
		}
		rest := k[2:]
		under := strings.LastIndex(rest, "_")
		if under <= 0 {
			continue
		}
		var i int
		if _, err := fmt.Sscanf(rest[:under], "%d", &i); err != nil {
			continue
		}
		s := get(i)
		switch rest[under+1:] {
		case "sel":
			switch t := v.(type) {
			case string:
				s.sel = strings.TrimSpace(t)
			case []any:
				for _, li := range t {
					if str, ok := li.(string); ok && strings.TrimSpace(str) != "" {
						s.selMulti = append(s.selMulti, strings.TrimSpace(str))
					}
				}
			}
		case "txt":
			if str, ok := v.(string); ok {
				s.txt = strings.TrimSpace(str)
			}
		}
	}
	idxs := make([]int, 0, len(byIdx))
	for i := range byIdx {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	out := make([]askFormSlot, 0, len(idxs))
	for _, i := range idxs {
		s := byIdx[i]
		if s.sel == "" && len(s.selMulti) == 0 && s.txt == "" {
			continue
		}
		out = append(out, *s)
	}
	return out
}

// deleteAnchorForm removes a currently-mounted AskUserQuestion form via the
// CardKit DELETE endpoint. Wrapper around Dispatcher.DeleteAnchorFormWithSeq
// that acquires SendMu and refreshes a token first.
func (s *Service) deleteAnchorForm(anchor *internalfeishu.CardAnchor) {
	if s.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := s.dispatcher.GetToken(ctx)
	if err != nil {
		log.Printf("feishu: askform DELETE token failed: %v", err)
		return
	}
	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	if !anchor.FormMounted {
		return
	}
	seq := atomic.AddInt64(&anchor.PatchSeq, 1)
	if err := s.dispatcher.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seq); err != nil {
		log.Printf("feishu: askform DELETE failed session=%s: %v", anchor.SessionID, err)
		return
	}
	anchor.FormMounted = false
	anchor.PendingForm = nil
	log.Printf("feishu: askform DELETE ok session=%s seq=%d", anchor.SessionID, seq)
}

// clearAnchorInput PATCHes the anchor card's input element back to empty so
// the user's just-submitted reply doesn't linger in the textbox. Best-effort:
// errors logged, never bubble up (the inject itself already succeeded).
func (s *Service) clearAnchorInput(anchor *internalfeishu.CardAnchor, sessionID string) {
	log.Printf("feishu: clear input entered card=%s disp_nil=%v", anchor.CardToken, s.dispatcher == nil)
	if s.dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := s.dispatcher.GetToken(ctx)
	if err != nil {
		log.Printf("feishu: clear input get token: %v", err)
		return
	}
	// Hold SendMu across DELETE + CREATE so nothing else can send an op
	// with a higher seq in between — Feishu enforces strict monotonicity
	// (code=300317 "sequence compare failed" if it's violated).
	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	seqDel := atomic.AddInt64(&anchor.PatchSeq, 1)
	seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
	// New element_id every cycle — Feishu's client caches typed values by
	// element_id even across DELETE + POST, so reusing the same id would
	// leak the last value straight through. seq is monotonic and unique
	// per anchor, making it a natural unique suffix.
	newInputID := fmt.Sprintf("anchor_input_%d", seqCre)
	if err := s.dispatcher.ClearAnchorInputWithSeqs(ctx, tok, anchor.CardToken, sessionID,
		anchor.CurrentInputID, newInputID, seqDel, seqCre); err != nil {
		log.Printf("feishu: clear input DELETE+CREATE card=%s seq=%d/%d id_old=%s id_new=%s: %v",
			anchor.CardToken, seqDel, seqCre, anchor.CurrentInputID, newInputID, err)
		return
	}
	anchor.CurrentInputID = newInputID
	log.Printf("feishu: clear input ok card=%s seq=%d/%d new_id=%s", anchor.CardToken, seqDel, seqCre, newInputID)
}

// In-memory short-code table for local mode.
var (
	pendingMu    sync.Mutex
	pendingCodes = map[string]int64{}
)

// IssuePending generates a 6-char short-code valid for 15 minutes.
func (s *Service) IssuePending() string {
	code := internalfeishuPairCode()
	pendingMu.Lock()
	pendingCodes[code] = time.Now().Add(15 * time.Minute).Unix()
	pendingMu.Unlock()
	return code
}

// BeginPair returns a short-code the user can send to the bot to complete the
// bind flow. In relay mode the code is issued by the relay; in local mode it
// is generated in-process via IssuePending.
func (s *Service) BeginPair(ctx context.Context) (string, error) {
	if s.cfg.Mode == ModeRelay {
		url := strings.TrimRight(s.cfg.RelayURL, "/") + "/v1/feishu/bindings/me/begin-pair"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
		if err != nil {
			return "", fmt.Errorf("feishu begin-pair: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.cfg.RelayToken())
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.cfg.HTTPClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("feishu begin-pair: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("feishu begin-pair: relay status %d", resp.StatusCode)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return "", fmt.Errorf("feishu begin-pair: decode: %w", err)
		}
		if body.Code == "" {
			return "", errors.New("feishu begin-pair: relay returned empty code")
		}
		return body.Code, nil
	}
	return s.IssuePending(), nil
}

func (s *Service) consumePending(code string) bool {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	exp, ok := pendingCodes[code]
	if !ok || exp < time.Now().Unix() {
		delete(pendingCodes, code)
		return false
	}
	delete(pendingCodes, code)
	return true
}

// internalfeishuPairCode generates a 6-char short-code from a
// confusable-free alphabet.
func internalfeishuPairCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 6)
	rb := make([]byte, 6)
	if _, err := cryptorand.Read(rb); err != nil {
		panic(err)
	}
	for i, b := range rb {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

type noOpSessionLookup struct{}

func (noOpSessionLookup) Exists(uuid.UUID) bool          { return true }
func (noOpSessionLookup) Inject(uuid.UUID, string) error { return nil }

// authClassAdaptingClient promotes internal/feishu.AuthClassError to
// satisfy the desktop dispatcher's IsFeishuAuthClassError contract.
type authClassAdaptingClient struct {
	inner *internalfeishu.Client
}

func (c *authClassAdaptingClient) SendInteractiveToOpenID(ctx context.Context, tok, open string, body []byte) (string, error) {
	mid, err := c.inner.SendInteractiveToOpenID(ctx, tok, open, body)
	return mid, c.adapt(err)
}
func (c *authClassAdaptingClient) SendTextToOpenID(ctx context.Context, tok, open, text string) error {
	return c.adapt(c.inner.SendTextToOpenID(ctx, tok, open, text))
}
func (c *authClassAdaptingClient) SendAnchorCard(ctx context.Context, tok, openID string, cardBody []byte) (string, string, error) {
	mid, token, err := c.inner.SendAnchorCard(ctx, tok, openID, cardBody)
	return mid, token, c.adapt(err)
}
func (c *authClassAdaptingClient) PatchCard(ctx context.Context, tok, cardToken, elementID, bodyMarkdown string, sequence int64) error {
	return c.adapt(c.inner.PatchCard(ctx, tok, cardToken, elementID, bodyMarkdown, sequence))
}
func (c *authClassAdaptingClient) PatchCardElement(ctx context.Context, tok, cardToken, elementID string, partial map[string]any, sequence int64) error {
	return c.adapt(c.inner.PatchCardElement(ctx, tok, cardToken, elementID, partial, sequence))
}
func (c *authClassAdaptingClient) UpdateCardElement(ctx context.Context, tok, cardToken, elementID string, element map[string]any, sequence int64) error {
	return c.adapt(c.inner.UpdateCardElement(ctx, tok, cardToken, elementID, element, sequence))
}
func (c *authClassAdaptingClient) DeleteCardElement(ctx context.Context, tok, cardToken, elementID string, sequence int64) error {
	return c.adapt(c.inner.DeleteCardElement(ctx, tok, cardToken, elementID, sequence))
}
func (c *authClassAdaptingClient) CreateCardElement(ctx context.Context, tok, cardToken, targetElementID, insertType string, elements []map[string]any, sequence int64) error {
	return c.adapt(c.inner.CreateCardElement(ctx, tok, cardToken, targetElementID, insertType, elements, sequence))
}
func (c *authClassAdaptingClient) adapt(err error) error {
	if err == nil {
		return nil
	}
	if internalfeishu.IsAuthClassError(err) {
		return &authClassErr{inner: err}
	}
	return err
}

type authClassErr struct{ inner error }

func (e *authClassErr) Error() string                { return e.inner.Error() }
func (e *authClassErr) Unwrap() error                { return e.inner }
func (e *authClassErr) IsFeishuAuthClassError() bool { return true }
