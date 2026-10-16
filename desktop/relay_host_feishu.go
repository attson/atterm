package main

import (
	"context"
	"errors"
	"fmt"
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
		specs = append(specs, internalfeishu.AskFormQuestion{
			Question:    q.Question,
			Options:     opts,
			MultiSelect: q.MultiSelect,
			InputMode:   q.InputMode,
		})
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			logWarn("feishu-form", "token failed session=%s: %v", sessionIDStr, err)
			return
		}
		anchor.SendMu.Lock()
		defer anchor.SendMu.Unlock()
		if anchor.FormMounted {
			seqDel := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seqDel); err != nil {
				logWarn("feishu-form", "pre-DELETE failed session=%s: %v", sessionIDStr, err)
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
				logWarn("feishu-form", "input DELETE failed session=%s: %v", sessionIDStr, err)
				// Non-fatal — keep going and mount the form anyway.
			} else {
				anchor.CurrentInputID = ""
			}
		}
		// Same for the terminal controls (including shell pagination). The form has its
		// own 提交/重置 buttons; the keystroke row on top of that is noise.
		if anchor.ButtonsMounted {
			seqBtn := atomic.AddInt64(&anchor.PatchSeq, 1)
			if err := disp.DeleteAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, seqBtn); err != nil {
				logWarn("feishu-form", "buttons DELETE failed session=%s: %v", sessionIDStr, err)
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
			logError("feishu-form", "CREATE failed session=%s q=%d: %v", sessionIDStr, len(questions), err)
			return
		}
		anchor.FormMounted = true
		anchor.PendingForm = specs
		logInfo("feishu-form", "inserted session=%s q=%d seq=%d", sessionIDStr, len(questions), seqCre)
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
		logWarn("feishu-form", "token failed session=%s: %v", anchor.SessionID, err)
		return
	}
	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	if anchor.FormMounted {
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		if err := disp.DeleteAnchorFormWithSeq(ctx, tok, anchor.CardToken, seq); err != nil {
			logWarn("feishu-form", "DELETE failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.FormMounted = false
		anchor.PendingForm = nil
		logInfo("feishu-form", "removed session=%s seq=%d", anchor.SessionID, seq)
	}
	// Restore the standalone Type-here input we tore down when the form went
	// up. New element_id every time — Feishu's client caches by id and would
	// otherwise resurrect stale value even across DELETE+CREATE.
	if anchor.CurrentInputID == "" {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		newID := fmt.Sprintf("anchor_input_%d", seqCre)
		if err := disp.CreateAnchorInputWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, newID, seqCre); err != nil {
			logWarn("feishu-form", "input CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.CurrentInputID = newID
		logInfo("feishu-form", "input restored session=%s seq=%d new_id=%s", anchor.SessionID, seqCre, newID)
	}
	// Restore the terminal controls after the input so the on-card
	// element order stays: body → input → buttons. CurrentInputID is the
	// insert-after anchor when present; when input somehow failed to
	// restore, fall back to inserting after the body.
	if !anchor.ButtonsMounted {
		seqCre := atomic.AddInt64(&anchor.PatchSeq, 1)
		target := anchor.CurrentInputID
		if target == "" {
			target = internalfeishu.AnchorBodyElementID
		}
		if err := disp.CreateAnchorButtonsWithSeq(ctx, tok, anchor.CardToken, anchor.SessionID, target, anchor.Pagination, seqCre); err != nil {
			logWarn("feishu-form", "buttons CREATE (restore) failed session=%s: %v", anchor.SessionID, err)
			return
		}
		anchor.ButtonsMounted = true
		logInfo("feishu-form", "buttons restored session=%s seq=%d", anchor.SessionID, seqCre)
	}
}

// swapAnchorButtons PATCHes the anchor card's button row. options nil →
// restore the default terminal controls; options non-empty
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tok, _, err := disp.GetToken(ctx)
		if err != nil {
			logWarn("feishu-form", "token failed session=%s: %v", sessionIDStr, err)
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
			logDebug("feishu-form", "skip (buttons not mounted) session=%s opts=%d", sessionIDStr, len(options))
			return
		}
		var partial map[string]any
		if len(options) > 0 {
			partial = internalfeishu.AskOptionsColumnSet(sessionIDStr, options)
		} else {
			partial = internalfeishu.DefaultButtonsColumnSet(sessionIDStr, anchor.Pagination)
		}
		// PATCH addresses the column_set by element_id; its tag is immutable.
		delete(partial, "tag")
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		err = disp.PatchAnchorElement(ctx, tok, anchor.CardToken,
			internalfeishu.AnchorButtonsElementID, partial, seq)
		anchor.SendMu.Unlock()
		if err != nil {
			logWarn("feishu-form", "PATCH failed session=%s opts=%d: %v", sessionIDStr, len(options), err)
			return
		}
		logInfo("feishu-form", "swapped session=%s opts=%d seq=%d", sessionIDStr, len(options), seq)
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

// onAISidCaptured forwards an AI recovery projection to the registered
// callback (set by the App during startup). aiSid="" invalidates a previous
// generation; a non-empty value installs a precise resume credential. Safe to
// call when the callback is nil — the resolver just fires and forgets.
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
// a network round-trip so callers — settings, task-state callbacks, hook HTTP
// handlers — must not block). The attach path reads the session's actual type
// so an "ai" policy cannot accidentally attach a shell session merely because
// it emitted a task-state event.
func (h *relayHost) lazyAttachIfMissing(ctx context.Context, sess *session.Session, sid uuid.UUID) {
	sidStr := sid.String()
	if !h.tryStartLazyAttach(sidStr) {
		return
	}
	go func() {
		defer h.clearLazyAttachInFlight(sidStr)
		h.attachFeishuSubscriberForAutoAttach(ctx, sess, sid)
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
	h.mu.Lock()
	active := h.sessions[sid]
	h.mu.Unlock()
	if active == nil || !active.feishuUserActivated.Load() {
		return
	}
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		return
	}
	h.lazyAttachIfMissing(context.Background(), sess, sid)
}

// attachFeishuSubscriberForAutoAttach checks whether the user's binding and
// the session's current type allow an automatic attach, then posts an anchor
// card and starts the subscriber. All Feishu I/O is error-swallowed and
// non-blocking with respect to the session lifecycle.
func (h *relayHost) attachFeishuSubscriberForAutoAttach(ctx context.Context, sess *session.Session, sessID uuid.UUID) {
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
	info := sess.Info()
	// Gate on the authoritative session type rather than the event that happened
	// to trigger this attempt. Toggle-on scans, task-state changes, and AI hook
	// events can therefore share the same path without widening an "ai" policy.
	switch autoAttach {
	case "none":
		return
	case "ai":
		if info.Type != session.SessionTypeAI {
			return
		}
	case "all":
		// All live session types are eligible.
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

	h.mu.Lock()
	restored := false
	if as, ok := h.sessions[sessID]; ok {
		restored = as.restored
	}
	h.mu.Unlock()

	pagination := info.Type != session.SessionTypeAI
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
		Pagination:   pagination,
	})
	if err != nil {
		logError("feishu-anchor", "render create failed session=%s: %v", sessID, err)
		return
	}

	msgID, cardToken, _, err := disp.SendAnchorCard(ctx, cardBody)
	if err != nil {
		logError("feishu-anchor", "send anchor card failed session=%s: %v", sessID, err)
		return
	}

	anchor := &internalfeishu.CardAnchor{
		SessionID:      sessionIDStr,
		CardMsgID:      msgID,
		CardToken:      cardToken,
		OwnerOpenID:    openID,
		CreatedAt:      time.Now(),
		Pagination:     pagination,
		CurrentInputID: internalfeishu.AnchorInputElementID,
		ButtonsMounted: true,
	}
	h.feishuCards.Put(anchor)

	// rt threads the live status state. render() composes the wrapper (status
	// preamble + inner body) from current state and PATCHes on meaningful body
	// or task-state changes. Do not periodically render an idle card: CardKit
	// may clear an unsent anchor_input draft when a sibling element is patched.
	rt := &anchorRuntime{createdAt: time.Now()}
	rt.taskState.Store("")
	rt.lastInner.Store("")
	rt.lastOutputAt.Store(time.Time{})
	rt.aiMode.Store(!pagination)

	rt.patches = newLatestAnchorPatchQueue(func(wrapped string) {
		started := time.Now()
		tok, _, err := disp.GetToken(context.Background())
		if err != nil {
			logWarn("feishu-anchor", "patch token failed session=%s: %v", sessID, err)
			return
		}
		anchor.SendMu.Lock()
		seq := atomic.AddInt64(&anchor.PatchSeq, 1)
		err = internalfeishu.PatchWithRetry(func() error {
			return disp.PatchAnchor(context.Background(), tok, anchor.CardToken, wrapped, seq)
		})
		anchor.SendMu.Unlock()
		if err == nil {
			logDebug("feishu-anchor", "patch ok session=%s seq=%d elapsed=%s", sessID, seq, time.Since(started))
			return
		}
		if internalfeishu.IsCardGoneError(err) {
			logInfo("feishu-anchor", "card gone session=%s — detaching", sessID)
			h.feishuCards.RemoveBySessionID(sessID.String())
			h.detachFeishuSubscriber(sessID)
			return
		}
		var ace *internalfeishu.AuthClassError
		if errors.As(err, &ace) {
			logWarn("feishu-anchor", "auth refresh needed session=%s (%v)", sessID, err)
			return
		}
		logWarn("feishu-anchor", "patch gave up after retry session=%s: %v", sessID, err)
	})

	rt.render = func() {
		state, _ := rt.taskState.Load().(string)
		inner, _ := rt.lastInner.Load().(string)
		lastOutputAt, _ := rt.lastOutputAt.Load().(time.Time)
		wrapped := internalfeishu.PrependStatusWithLastOutput(state, time.Since(rt.createdAt), lastOutputAt, inner)
		rt.patches.Enqueue(wrapped)
	}

	// applyBody is the structured AI callback. shellFlush adds a generation
	// gate so a PTY frame already being drained when shell→AI happens cannot
	// restore the stale paginated body after the upgrade reset.
	applyBody := func(body string) {
		preview := body
		if len(preview) > 60 {
			preview = preview[:60]
		}
		logDebug("feishu-anchor", "flush session=%s body_len=%d preview=%q", sessID, len(body), preview)
		rt.lastInner.Store(body)
		rt.lastOutputAt.Store(time.Now())
		rt.render()
	}
	shellFlush := func(body string) {
		if rt.aiMode.Load() {
			return
		}
		applyBody(body)
	}
	rt.flush = applyBody

	h.feishuSubsMu.Lock()
	h.getOrCreateFeishuSessionLocked(sessionIDStr).anchor = rt
	h.feishuSubsMu.Unlock()

	// SendAnchorCard is a network round trip. The session may have launched an
	// AI CLI while that request was in flight, so do not reuse the type snapshot
	// captured for the card header when selecting the live body source.
	pumpPTYBytes := sess.Info().Type != session.SessionTypeAI
	sub := internalfeishu.AttachFeishuSubscriber(sess, openID, pumpPTYBytes, shellFlush)

	h.feishuSubsMu.Lock()
	h.getOrCreateFeishuSessionLocked(sessionIDStr).sub = sub
	h.feishuSubsMu.Unlock()

	// Check once more after publishing sub. This closes the remaining window:
	// classification before publication could not upgrade the missing sub,
	// while classification after publication upgrades through its callback.
	finalType := sess.Info().Type
	if finalType == session.SessionTypeAI {
		h.upgradeFeishuSessionToAI(sessionIDStr)
	}

	logInfo("feishu-anchor", "attached session=%s card_msg_id=%s initial_type=%s final_type=%s", sessID, msgID, info.Type, finalType)
}

// upgradeFeishuSessionToAI converts an already-attached shell anchor to the
// structured AI event stream. It is intentionally safe to call before an
// anchor exists and repeatedly for subsequent top-level AI commands.
func (h *relayHost) upgradeFeishuSessionToAI(sessionIDStr string) {
	disp := h.feishuDispatcher.Load()
	if disp == nil {
		return
	}

	h.feishuSubsMu.Lock()
	fs := h.feishuSessions[sessionIDStr]
	if fs == nil || fs.sub == nil || fs.anchor == nil || fs.anchor.flush == nil {
		h.feishuSubsMu.Unlock()
		return
	}
	fs.sub.SetPumpPTYBytes(false)
	if fs.aiChunker != nil {
		h.feishuSubsMu.Unlock()
		return
	}
	rt := fs.anchor
	rt.aiMode.Store(true)
	rt.lastInner.Store("")
	rt.lastOutputAt.Store(time.Time{})
	chunker := internalfeishu.NewAIChunker(rt.flush)
	fs.aiChunker = chunker
	sub := fs.sub
	// Register while holding feishuSubsMu so detach cannot remove the session
	// and its dispatcher entry between our local state change and this call.
	disp.AttachAIChunker(sessionIDStr, chunker)
	h.feishuSubsMu.Unlock()
	// Remove the shell page marker/history immediately. The first structured
	// turn may arrive later (Codex binds its rollout asynchronously), but the
	// card must already present itself as an AI conversation.
	if rt.render != nil {
		rt.render()
	}

	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				chunker.Tick()
			case <-sub.Done():
				return
			}
		}
	}()

	// CardKit operations are network I/O and this helper may run inside the
	// session's locked classification callback, so update controls async.
	go h.patchAnchorForAIMode(sessionIDStr, disp)
}

func (h *relayHost) patchAnchorForAIMode(sessionIDStr string, disp *feishu.Dispatcher) {
	anchor := h.feishuCards.BySessionID(sessionIDStr)
	if anchor == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, _, err := disp.GetToken(ctx)
	if err != nil {
		logWarn("feishu-anchor", "AI controls token failed session=%s: %v", sessionIDStr, err)
		return
	}

	anchor.SendMu.Lock()
	defer anchor.SendMu.Unlock()
	anchor.Pagination = false
	if !anchor.ButtonsMounted {
		return
	}
	partial := internalfeishu.DefaultButtonsColumnSet(sessionIDStr, false)
	delete(partial, "tag")
	seq := atomic.AddInt64(&anchor.PatchSeq, 1)
	if err := disp.PatchAnchorElement(ctx, tok, anchor.CardToken,
		internalfeishu.AnchorButtonsElementID, partial, seq); err != nil {
		logWarn("feishu-anchor", "AI controls PATCH failed session=%s: %v", sessionIDStr, err)
		return
	}
	logInfo("feishu-anchor", "upgraded shell anchor to AI session=%s seq=%d", sessionIDStr, seq)
}

// OnRemoteTerminalToggle reacts to changes in the binding's
// RemoteTerminalEnabled flag. When flipped on, it immediately backfills live
// sessions allowed by SessionAutoAttach. When flipped off, it detaches every
// active FeishuSubscriber and PATCHes each anchor to its archived state.
// Sessions themselves are unaffected.
func (h *relayHost) OnRemoteTerminalToggle(enabled bool) {
	if enabled {
		if h.feishuRemoteTermState == nil {
			return
		}
		stateEnabled, _, autoAttach, ok := h.feishuRemoteTermState(context.Background())
		if !ok || !stateEnabled || autoAttach == "none" {
			return
		}
		for _, sess := range h.server.Registry().List() {
			if autoAttach == "ai" && sess.Info().Type != session.SessionTypeAI {
				continue
			}
			if autoAttach != "ai" && autoAttach != "all" {
				return
			}
			h.lazyAttachIfMissing(context.Background(), sess, sess.ID)
		}
		return
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
	var rt *anchorRuntime
	if fs := h.feishuSessions[sessionIDStr]; fs != nil {
		sub = fs.sub
		rt = fs.anchor
	}
	delete(h.feishuSessions, sessionIDStr)
	h.feishuSubsMu.Unlock()
	if rt != nil && rt.patches != nil {
		rt.patches.Stop()
	}

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
			logWarn("feishu-anchor", "archive token failed session=%s: %v", sessID, err)
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
				logWarn("feishu-anchor", "archive patch failed session=%s: %v", sessID, perr)
			}
		}
	}()
}
