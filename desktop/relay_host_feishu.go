package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/attson/atterm/desktop/feishu"
	internalfeishu "github.com/attson/atterm/internal/feishu"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

// SetFeishuDispatcher atomically swaps the dispatcher used for command-finished
// and waiting-input Feishu cards. Safe to call concurrently with session
// callbacks. A nil dispatcher disables dispatch. Also wires the per-session
// anchor-button-swap callback so AskUserQuestion can flip the keystroke row
// to option buttons, and the AskUserQuestion form-insert callback.
func (h *relayHost) SetFeishuDispatcher(d *feishu.Dispatcher) {
	h.feishuDispatcher.Store(d)
	if d != nil {
		d.SetOnAnchorButtons(h.swapAnchorButtons)
		d.SetOnAskForm(h.updateAnchorAskForm)
		d.SetOnTurnMissingChunker(h.onTurnMissingChunker)
	}
}

// updateAnchorAskForm inserts an AskUserQuestion form container after the
// body markdown (questions non-empty), or removes it (questions nil).
// Best-effort: errors logged, never bubble up.
func (h *relayHost) updateAnchorAskForm(sessionIDStr string, questions []feishu.AskUserQuestionEntry) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	anchor := h.feishuCards.BySessionID(sessionIDStr)
	if anchor == nil {
		return
	}
	if len(questions) == 0 {
		go h.deleteAnchorForm(anchor)
		return
	}
	// Insert (or replace) the form: DELETE any existing first, then CREATE.
	// Both steps under SendMu so seq stays contiguous.
	specs := make([]internalfeishu.AskFormQuestion, 0, len(questions))
	for _, q := range questions {
		opts := make([]internalfeishu.AskFormOpt, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, internalfeishu.AskFormOpt{Label: o.Label})
		}
		specs = append(specs, internalfeishu.AskFormQuestion{Question: q.Question, Options: opts, MultiSelect: q.MultiSelect})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			log.Printf("feishu-anchor-form: token failed session=%s: %v", sessionIDStr, err)
			return
		}
		anchor.SendMu.Lock()
		defer anchor.SendMu.Unlock()
		if anchor.FormMounted {
			seqDel := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seqDel); err != nil {
				log.Printf("feishu-anchor-form: pre-DELETE failed session=%s: %v", sessionIDStr, err)
				return
			}
			anchor.FormMounted = false
		}
		// Also strip the standalone Type-here input while the form is up —
		// each question already has its own custom input, so keeping the
		// original input around just crowds the card. CurrentInputID == ""
		// means "no input currently mounted"; deleteAnchorForm restores it.
		if anchor.CurrentInputID != "" {
			seqInp := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorInputWithSeq(ctx, tok, anchor.CardToken, anchor.CurrentInputID, seqInp); err != nil {
				log.Printf("feishu-anchor-form: input DELETE failed session=%s: %v", sessionIDStr, err)
				// Non-fatal — keep going and mount the form anyway.
			} else {
				anchor.CurrentInputID = ""
			}
		}
		// Same for the buttons row (^C/^D/Esc/Enter/结束). The form has its
		// own 提交/重置 buttons; the keystroke row on top of that is noise.
		if anchor.ButtonsMounted {
			seqBtn := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, seqBtn); err != nil {
				log.Printf("feishu-anchor-form: buttons DELETE failed session=%s: %v", sessionIDStr, err)
			} else {
				anchor.ButtonsMounted = false
			}
		}
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		// Pass seqCre as the mount seq so widget element_ids get a fresh
		// suffix each time the form remounts on the same anchor card.
		// Otherwise the Feishu client caches widget state by element_id
		// and reloads the previous submission's dropdowns/inputs on
		// subsequent AskUserQuestion cycles (image #107).
		form := internalfeishu.RenderAskQuestionForm(sessionIDStr, specs, seqCre)
		if err := disp.InsertAnchorFormWithSeq(ctx, tok, anchor.CardToken, form, seqCre); err != nil {
			log.Printf("feishu-anchor-form: CREATE failed session=%s q=%d: %v", sessionIDStr, len(questions), err)
			return
		}
		anchor.FormMounted = true
		anchor.PendingForm = specs
		log.Printf("feishu-anchor-form: inserted session=%s q=%d seq=%d", sessionIDStr, len(questions), seqCre)
	}()
}

// deleteAnchorForm removes the currently-mounted form (if still there) and
// restores the standalone input + buttons row that were torn down when the
// form went up. Independent state checks — the service layer's
// handleAskFormSubmit ALSO calls a variant of this that flips FormMounted
// as soon as the form's DELETEd, so this path routinely arrives with the
// form already gone; it must still restore input/buttons in that case.
// Safe to call repeatedly: each of the three fixups is guarded on state.
func (h *relayHost) deleteAnchorForm(anchor *internalfeishu.CardAnchor) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := disp.GetToken(ctx)
	if err != nil {
		log.Printf("feishu-anchor-form: token failed session=%s: %v", anchor.SessionID, err)
		return
	}
	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	if anchor.FormMounted {
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seq); err != nil {
			log.Printf("feishu-anchor-form: DELETE failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.FormMounted = false
		anchor.PendingForm = nil
		log.Printf("feishu-anchor-form: removed session=%s seq=%d", anchor.SessionID, seq)
	}
	// Restore the standalone Type-here input we tore down when the form went
	// up. New element_id every time — Feishu's client caches by id and would
	// otherwise resurrect stale value even across DELETE+CREATE.
	if anchor.CurrentInputID == "" {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		newID := fmt.Sprintf("anchor_input_%d", seqCre)
		if err := disp.CreateAnchorInputWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, newID, seqCre); err != nil {
			log.Printf("feishu-anchor-form: input CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.CurrentInputID = newID
		log.Printf("feishu-anchor-form: input restored session=%s seq=%d new_id=%s", anchor.SessionID, seqCre, newID)
	}
	// Restore the keystroke buttons row after the input so the on-card
	// element order stays: body → input → buttons. CurrentInputID is the
	// insert-after anchor when present; when input somehow failed to
	// restore, fall back to inserting after the body.
	if !anchor.ButtonsMounted {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		target := anchor.CurrentInputID
		if target == "" {
			target = internalfeishu.AnchorBodyElementID
		}
		if err := disp.CreateAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, target, seqCre); err != nil {
			log.Printf("feishu-anchor-form: buttons CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.ButtonsMounted = true
		log.Printf("feishu-anchor-form: buttons restored session=%s seq=%d", anchor.SessionID, seqCre)
	}
}

// swapAnchorButtons PATCHes the anchor card's button row. options nil →
// restore the default keystroke row (^C/^D/Esc/Enter/结束); options non-empty
// → render one primary button per option label (clicking submits the label
// as if typed into the input box). Best-effort: errors logged, never bubble.
func (h *relayHost) swapAnchorButtons(sessionIDStr string, options []string) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	anchor := h.feishuCards.BySessionID(sessionIDStr)
	if anchor == nil {
		return
	}
	var partial map[string]any
	if len(options) > 0 {
		partial = internalfeishu.AskOptionsColumnSet(sessionIDStr, options)
	} else {
		partial = internalfeishu.DefaultButtonsColumnSet(sessionIDStr)
	}
	// PATCH the column_set element; `tag` is immutable per V2 contract so
	// strip it from the partial body.
	delete(partial, "tag")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			log.Printf("feishu-anchor-buttons: token failed session=%s: %v", sessionIDStr, err)
			return
		}
		anchor.SendMu.Lock()
		// Skip when the buttons element isn't on the card: an
		// AskUserQuestion form-mount tears down anchor_buttons, and the
		// PATCH here would then hit Feishu's 300313 "elementID not found".
		// The form tear-down path is responsible for CREATE-ing the row
		// back; we shouldn't step on it with a PATCH that can't work.
		if !anchor.ButtonsMounted {
			anchor.SendMu.Unlock()
			log.Printf("feishu-anchor-buttons: skip (buttons not mounted) session=%s opts=%d", sessionIDStr, len(options))
			return
		}
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		err = disp.PatchAnchorElement(ctx, tok, anchor.CardToken,
			internalfeishu.AnchorButtonsElementID, partial, seq)
		anchor.SendMu.Unlock()
		if err != nil {
			log.Printf("feishu-anchor-buttons: PATCH failed session=%s opts=%d: %v", sessionIDStr, len(options), err)
			return
		}
		log.Printf("feishu-anchor-buttons: swapped session=%s opts=%d seq=%d", sessionIDStr, len(options), seq)
	}()
}

// SetFeishuRemoteTermState installs the callback the anchor-card guard uses to
// read remote-terminal gate state for the current mode.
func (h *relayHost) SetFeishuRemoteTermState(fn func(ctx context.Context) (bool, string, string, bool)) {
	h.feishuRemoteTermState = fn
}

// appendFeishuHookEnv adds ATTERM_SESSION_ID + ATTERM_HOOK_ENDPOINT to
// a process env slice. The hook endpoint comes from the desktop's
// in-process FeishuService (set up in Task 21); empty endpoint = skip.
func appendFeishuHookEnv(env []string, sessionID, hookEndpoint string) []string {
	if sessionID != "" {
		env = append(env, "ATTERM_SESSION_ID="+sessionID)
	}
	if hookEndpoint != "" {
		env = append(env, "ATTERM_HOOK_ENDPOINT="+hookEndpoint)
	}
	return env
}

// shouldNotifySession reports whether a Feishu notification should fire for a
// session of the given workload type, honoring the "AI sessions only"
// preference. aiOnly=false → always notify; aiOnly=true → only ai sessions.
func shouldNotifySession(sessionType string, aiOnly bool) bool {
	if !aiOnly {
		return true
	}
	return sessionType == session.SessionTypeAI
}

// getOrCreateFeishuSessionLocked returns the per-session record for sid,
// creating it if absent. Caller must hold feishuSubsMu.
func (h *relayHost) getOrCreateFeishuSessionLocked(sid string) *feishuSession {
	fs, ok := h.feishuSessions[sid]
	if !ok {
		fs = &feishuSession{}
		h.feishuSessions[sid] = fs
	}
	return fs
}

// onAISidCaptured forwards a captured AI session id to the registered
// callback (set by the App during startup). Safe to call when the
// callback is nil — the sniff just fires and forgets.
func (h *relayHost) onAISidCaptured(localSessionID uuid.UUID, kind, aiSid string) {
	if h.aiSidCallback != nil {
		h.aiSidCallback(localSessionID, kind, aiSid)
	}
}

// tryStartLazyAttach acquires the per-session lazy-backfill slot for sid
// iff the session has no live FeishuSubscriber and no backfill goroutine
// is already running. Returns true when the caller now owns the slot and
// MUST release it via clearLazyAttachInFlight once the attach path exits.
// Under a concurrent burst of turn/state events on the same session (e.g.
// after the user toggles remote-terminal on and claude spits back a few
// hook events in quick succession), only one caller wins; the rest bail.
func (h *relayHost) tryStartLazyAttach(sidStr string) bool {
	h.feishuSubsMu.Lock()
	defer h.feishuSubsMu.Unlock()
	fs := h.feishuSessions[sidStr]
	if fs != nil && fs.sub != nil {
		return false // already attached
	}
	if fs != nil && fs.lazyAttachInFlight {
		return false // already in flight
	}
	if fs == nil {
		fs = &feishuSession{}
		h.feishuSessions[sidStr] = fs
	}
	fs.lazyAttachInFlight = true
	return true
}

// clearLazyAttachInFlight releases the slot claimed by tryStartLazyAttach.
// Safe to call on a sid that was never claimed.
func (h *relayHost) clearLazyAttachInFlight(sidStr string) {
	h.feishuSubsMu.Lock()
	if fs := h.feishuSessions[sidStr]; fs != nil {
		fs.lazyAttachInFlight = false
	}
	h.feishuSubsMu.Unlock()
}

// lazyAttachIfMissing gates on tryStartLazyAttach and, on success, kicks
// off attachFeishuSubscriberForAutoAttach in a goroutine (attach performs
// a network round-trip so callers — task-state callbacks, hook HTTP
// handlers — must not block). triggerMode "ai" is used so
// SessionAutoAttach="ai" bindings also honor the lazy path; "all" bindings
// still attach because their switch case is unconditional. Callers pass
// a nil-safe context (attach's own guards handle a nil dispatcher etc.,
// so a stale session that closed mid-flight is not a panic risk).
func (h *relayHost) lazyAttachIfMissing(ctx context.Context, sess *session.Session, sid uuid.UUID) {
	sidStr := sid.String()
	if !h.tryStartLazyAttach(sidStr) {
		return
	}
	go func() {
		defer h.clearLazyAttachInFlight(sidStr)
		h.attachFeishuSubscriberForAutoAttach(ctx, sess, sid, "ai")
	}()
}

// onTurnMissingChunker is registered with the Dispatcher so a TurnEvent
// arriving for a session with no AIChunker (i.e. the user toggled
// remote-terminal on after this AI session started) triggers a lazy
// backfill. The current turn's body is lost because attach runs
// asynchronously; the anchor card is in place for the next turn.
func (h *relayHost) onTurnMissingChunker(sessionIDStr string) {
	sid, err := uuid.Parse(sessionIDStr)
	if err != nil {
		return
	}
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		return
	}
	h.lazyAttachIfMissing(context.Background(), sess, sid)
}

// attachFeishuSubscriberForAutoAttach is called either immediately on session
// creation (triggerMode="all") or from the AI-classified callback
// (triggerMode="ai"). It checks whether the user's binding allows the attach
// given the configured SessionAutoAttach value, then posts an anchor card and
// starts the subscriber. All Feishu I/O is error-swallowed and non-blocking
// with respect to the session lifecycle.
func (h *relayHost) attachFeishuSubscriberForAutoAttach(ctx context.Context, sess *session.Session, sessID uuid.UUID, triggerMode string) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}
	if h.feishuRemoteTermState == nil {
		return
	}
	enabled, openID, autoAttach, ok := h.feishuRemoteTermState(ctx)
	if !ok {
		// No binding / not ready — silently skip.
		return
	}
	if !enabled {
		return
	}
	if openID == "" {
		return
	}
	// Gate on autoAttach: "ai" triggers only fire from the AI-classified
	// callback; "all" triggers fire immediately. "none" never auto-attaches.
	switch autoAttach {
	case "none":
		return
	case "ai":
		if triggerMode != "ai" {
			return
		}
	case "all":
		// always proceed regardless of triggerMode
	default:
		// unknown value — skip
		return
	}

	sessionIDStr := sessID.String()

	// Guard against double-attach: if a subscriber is already registered for this
	// session (e.g. autoAttach="all" already attached at NewSession time, then
	// SetOnAIClassified fires later and re-enters this function), bail.
	h.feishuSubsMu.Lock()
	if fs := h.feishuSessions[sessionIDStr]; fs != nil && fs.sub != nil {
		h.feishuSubsMu.Unlock()
		return
	}
	h.feishuSubsMu.Unlock()

	// Short label fallback when Info().Title is empty: first 8 chars of UUID.
	label := sessionIDStr
	if len(label) > 8 {
		label = label[:8]
	}

	info := sess.Info()
	h.mu.Lock()
	restored := false
	if as, ok := h.sessions[sessID]; ok {
		restored = as.restored
	}
	h.mu.Unlock()

	cardBody, err := internalfeishu.RenderAnchorCreate(internalfeishu.AnchorState{
		SessionID:    sessionIDStr,
		SessionLabel: label,
		Title:        info.Title,
		Cwd:          info.Cwd,
		// StatusText intentionally empty — the body element's PrependStatus
		// preamble carries live state instead. The subtitle is fixed-at-send
		// in V2 (no element-level PATCH for header), so a stale "running"
		// there is worse than just the cwd alone.
		StatusText:   "",
		BodyMarkdown: "",
		Template:     "blue",
		Restored:     restored,
	})
	if err != nil {
		log.Printf("feishu-anchor: render create failed session=%s: %v", sessID, err)
		return
	}

	msgID, cardToken, _, err := disp.SendAnchorCard(ctx, cardBody)
	if err != nil {
		log.Printf("feishu-anchor: send anchor card failed session=%s: %v", sessID, err)
		return
	}

	anchor := &internalfeishu.CardAnchor{
		SessionID:      sessionIDStr,
		CardMsgID:      msgID,
		CardToken:      cardToken,
		OwnerOpenID:    openID,
		CreatedAt:      time.Now(),
		CurrentInputID: internalfeishu.AnchorInputElementID,
		ButtonsMounted: true,
	}
	h.feishuCards.Put(anchor)

	// rt threads the live status state. Updated by SetOnTaskStateChange and
	// re-rendered on a 30s ticker so elapsed time refreshes even when the AI
	// is idle. render() composes the wrapper (status preamble + inner body)
	// from current state and PATCHes; everyone (chunker flush, state callback,
	// ticker) goes through it so the wire body stays consistent.
	rt := &anchorRuntime{createdAt: time.Now()}
	rt.taskState.Store("")
	rt.lastInner.Store("")

	patchWrapped := func(wrapped string) {
		go func() {
			tok, _, err := disp.GetToken(context.Background())
			if err != nil {
				log.Printf("feishu-anchor: patch token failed session=%s: %v", sessID, err)
				return
			}
			anchor.SendMu.Lock()
			seq := atomic.AddInt64(&anchor.PatchSeq, 1)
			err = internalfeishu.PatchWithRetry(func() error {
				return disp.PatchAnchor(context.Background(), tok, anchor.CardToken, wrapped, seq)
			})
			anchor.SendMu.Unlock()
			if err == nil {
				log.Printf("feishu-anchor: patch ok session=%s seq=%d", sessID, seq)
				return
			}
			if internalfeishu.IsCardGoneError(err) {
				log.Printf("feishu-anchor: card gone session=%s — detaching", sessID)
				h.feishuCards.RemoveBySessionID(sessID.String())
				h.detachFeishuSubscriber(sessID)
				return
			}
			var ace *internalfeishu.AuthClassError
			if errors.As(err, &ace) {
				log.Printf("feishu-anchor: auth refresh needed session=%s (%v)", sessID, err)
				return
			}
			log.Printf("feishu-anchor: patch gave up after retry session=%s: %v", sessID, err)
		}()
	}

	rt.render = func() {
		state, _ := rt.taskState.Load().(string)
		inner, _ := rt.lastInner.Load().(string)
		wrapped := internalfeishu.PrependStatus(state, time.Since(rt.createdAt), inner)
		patchWrapped(wrapped)
	}

	// flush is the Chunker callback — stash the latest inner body and re-render.
	flush := func(body string) {
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		log.Printf("feishu-anchor: flush session=%s body_len=%d preview=%q", sessID, len(body), preview)
		rt.lastInner.Store(body)
		rt.render()
	}

	h.feishuSubsMu.Lock()
	h.getOrCreateFeishuSessionLocked(sessionIDStr).anchor = rt
	h.feishuSubsMu.Unlock()

	// AI sessions render their anchor card body from per-turn AIChunker events
	// (👤/🤖/🛠), not from raw PTY bytes. Shell sessions still need the PTY
	// roller because there are no hook events to fall back on. `info` was
	// captured above for header rendering — reuse it here.
	pumpPTYBytes := info.Type != session.SessionTypeAI
	sub := internalfeishu.AttachFeishuSubscriber(sess, openID, pumpPTYBytes, flush)

	h.feishuSubsMu.Lock()
	h.getOrCreateFeishuSessionLocked(sessionIDStr).sub = sub
	h.feishuSubsMu.Unlock()

	// AI session only: wire a per-session AIChunker into the dispatcher so
	// per-turn hook events stream into the same anchor card. The chunker
	// shares the flush closure (same anchor + token path). The tick goroutine
	// keeps the chunker flushing on idle; it exits cleanly via fs.Done().
	if info.Type == session.SessionTypeAI {
		aiChunker := internalfeishu.NewAIChunker(flush)
		disp.AttachAIChunker(sessionIDStr, aiChunker)
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					aiChunker.Tick()
				case <-sub.Done():
					return
				}
			}
		}()
	}

	// Status preamble refresher: re-render every 30s so elapsed time bumps
	// even when the AI is idle (no body change means flush is never called
	// otherwise). Exits with sub.Done() so detach kills it cleanly.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				rt.render()
			case <-sub.Done():
				return
			}
		}
	}()

	log.Printf("feishu-anchor: attached session=%s card_msg_id=%s", sessID, msgID)
}

// OnRemoteTerminalToggle reacts to changes in the binding's
// RemoteTerminalEnabled flag. When flipped off, detach every active
// FeishuSubscriber and PATCH each anchor to its archived state. Sessions
// themselves are unaffected. When flipped on, no bulk re-attach — new
// sessions pick up via autoAttach; pre-existing sessions need to recreate
// (P2 — explicit /attach command).
func (h *relayHost) OnRemoteTerminalToggle(enabled bool) {
	if enabled {
		return // No bulk re-attach on enable; new sessions pick up naturally.
	}
	// Snapshot the current session IDs under the lock, then release before
	// calling detachFeishuSubscriber (which re-acquires the lock for each
	// entry and removes it atomically).
	h.feishuSubsMu.Lock()
	sids := make([]string, 0, len(h.feishuSessions))
	for sid := range h.feishuSessions {
		sids = append(sids, sid)
	}
	h.feishuSubsMu.Unlock()
	for _, sid := range sids {
		if id, err := uuid.Parse(sid); err == nil {
			h.detachFeishuSubscriber(id)
		}
	}
}

// detachFeishuSubscriber removes and stops the FeishuSubscriber for sessID,
// then PATCHes the anchor card to archived state. Idempotent.
func (h *relayHost) detachFeishuSubscriber(sessID uuid.UUID) {
	sessionIDStr := sessID.String()

	h.feishuSubsMu.Lock()
	var sub *internalfeishu.FeishuSubscriber
	if fs := h.feishuSessions[sessionIDStr]; fs != nil {
		sub = fs.sub
	}
	delete(h.feishuSessions, sessionIDStr)
	h.feishuSubsMu.Unlock()

	// Detach the AIChunker BEFORE sub.Detach() so in-flight hook events do not
	// push turns into a chunker whose tick goroutine is already exiting.
	disp := h.feishuDispatcher.Load()
	if disp != nil {
		disp.DetachAIChunker(sessionIDStr)
	}

	if sub != nil {
		sub.Detach()
	}

	anchor := h.feishuCards.BySessionID(sessionIDStr)
	h.feishuCards.RemoveBySessionID(sessionIDStr)

	if anchor == nil {
		return
	}

	// Archive the anchor card in a goroutine; session close must not block.
	go func() {
		disp := h.feishuDispatcher.Load()
		if disp == nil {
			return
		}
		tok, _, err := disp.GetToken(context.Background())
		if err != nil {
			log.Printf("feishu-anchor: archive token failed session=%s: %v", sessID, err)
			return
		}
		// Build the archive body markdown: last shell output + footer line.
		footer := "**已结束 at " + time.Now().Format("15:04:05") + "**"
		lastBody := anchor.LastBody
		archiveMD := lastBody
		if archiveMD != "" {
			archiveMD += "\n" + footer
		} else {
			archiveMD = footer
		}
		anchor.SendMu.Lock()
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		perr := disp.PatchAnchor(context.Background(), tok, anchor.CardToken, archiveMD, seq)
		anchor.SendMu.Unlock()
		if perr != nil {
			if !internalfeishu.IsCardGoneError(perr) {
				log.Printf("feishu-anchor: archive patch failed session=%s: %v", sessID, perr)
			}
		}
	}()
}
