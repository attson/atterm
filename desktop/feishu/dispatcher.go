// desktop/feishu/dispatcher.go
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	internalfeishu "github.com/attson/atterm/internal/feishu"
)

// IMClient is the subset of internal/feishu.Client the dispatcher uses.
type IMClient interface {
	SendInteractiveToOpenID(ctx context.Context, token, openID string, body []byte) (string, error)
	SendTextToOpenID(ctx context.Context, token, openID, text string) error
}

// CardKitClient is the subset of internal/feishu.Client used for anchor card
// operations (send + streaming PATCH). Stored separately in DispatcherConfig
// so tests can stub it without affecting the IM send path.
type CardKitClient interface {
	SendAnchorCard(ctx context.Context, tenantToken, openID string, cardBody []byte) (msgID, cardToken string, err error)
	PatchCard(ctx context.Context, tenantToken, cardToken, elementID, bodyMarkdown string, sequence int64) error
	PatchCardElement(ctx context.Context, tenantToken, cardToken, elementID string, partial map[string]any, sequence int64) error
}

// CommandFinishedEvent feeds the dispatcher from the heuristic OSC 133 D path.
type CommandFinishedEvent struct {
	SessionID  uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte

	// Context fields enrich the non-sealed card. The relay_host dispatch point
	// leaves OutputTail empty for sealed (E2EE) sessions; SessionTitle/Cwd are
	// dropped by the sealed render branch (RenderCommandFinishedCard), which
	// never reads them when the card is sealed.
	SessionTitle string
	Cwd          string
	OutputTail   string
}

// WaitingInputDispatchEvent feeds the dispatcher from both hook + heuristic paths.
type WaitingInputDispatchEvent struct {
	SessionID      uuid.UUID
	IdleForSeconds int
	Source         WaitingSource
	QuestionText   string
	DedupKey       string
	Options        []QuestionOption // non-empty → render AskQuestion card
	SessionTitle   string           // optional context for the heuristic card
	Cwd            string
	CurrentCommand string
	RecentOutput   string
}

// optionInjectText maps an AskUserQuestion option to the bytes injected into
// the PTY when its Feishu button is tapped.
//
// R1: how claude's TUI accepts an option selection is not yet verified against
// a live session. Default = the 1-based option number + Enter. If a live test
// shows the TUI needs arrow keys instead, change ONLY this function, e.g.
// `strings.Repeat("\x1b[B", i) + "\r"`.
func optionInjectText(i int, _ QuestionOption) string {
	return fmt.Sprintf("%d\n", i+1)
}

type WaitingSource int

const (
	WaitingSourceHeuristic WaitingSource = iota
	WaitingSourceHook
)

// DispatcherConfig holds the wired-in dependencies.
type DispatcherConfig struct {
	Store   BindingStore
	Token   TokenSource
	IM      IMClient
	CardKit CardKitClient // optional; nil disables anchor card send/patch
	// Now returns Unix seconds. Default = time.Now().Unix.
	Now func() int64
}

// cardMsgMap maps a sent card's message_id → session id, so a user replying to
// the card in Feishu can be routed back to the right session (handled in 9B).
// It is bounded (FIFO eviction) to cap memory for long-lived processes.
type cardMsgMap struct {
	mu    sync.Mutex
	m     map[string]uuid.UUID
	order []string
}

func (c *cardMsgMap) remember(msgID string, sid uuid.UUID) {
	if msgID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]uuid.UUID{}
	}
	if _, ok := c.m[msgID]; !ok {
		c.order = append(c.order, msgID)
		if len(c.order) > 512 {
			delete(c.m, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.m[msgID] = sid
}

func (c *cardMsgMap) lookup(msgID string) (uuid.UUID, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sid, ok := c.m[msgID]
	return sid, ok
}

// Dispatcher merges trigger streams into Feishu IM sends. Safe for concurrent use.
type Dispatcher struct {
	cfg DispatcherConfig

	muD          sync.Mutex
	lastDispatch map[string]int64
	failCounts   map[string]int // per-session consecutive command failures
	authFailures int            // global auth-class failure count

	cardMsgs cardMsgMap

	aiMu            sync.Mutex
	aiChunkers      map[string]*internalfeishu.AIChunker
	onAnchorButtons func(sessionID string, options []string) // nil opts → restore defaults
}

// LookupCardSession returns the session id a previously sent card's message_id
// was bound to, for routing direct replies back to that session (9B).
func (d *Dispatcher) LookupCardSession(msgID string) (uuid.UUID, bool) {
	return d.cardMsgs.lookup(msgID)
}

const (
	dedupWindowSeconds = 30
	maxAuthFails       = 3
)

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().Unix() }
	}
	return &Dispatcher{
		cfg:          cfg,
		lastDispatch: map[string]int64{},
		failCounts:   map[string]int{},
	}
}

func (d *Dispatcher) DispatchCommandFinished(ctx context.Context, ev CommandFinishedEvent) {
	// Maintain the per-session consecutive-failure streak. A non-zero exit
	// increments it; a success resets it. Guarded by muD.
	sidStr := ev.SessionID.String()
	d.muD.Lock()
	var failCount int
	if ev.ExitCode != 0 {
		d.failCounts[sidStr]++
		failCount = d.failCounts[sidStr]
	} else {
		delete(d.failCounts, sidStr)
	}
	d.muD.Unlock()

	d.dispatch(ctx, ev.SessionID, "cmd:"+sidStr, func() ([]byte, error) {
		card := internalfeishu.RenderCommandFinishedCard(internalfeishu.CommandFinishedInput{
			SessionID:    ev.SessionID,
			ExitCode:     ev.ExitCode,
			ElapsedMS:    ev.ElapsedMS,
			Label:        ev.Label,
			SealedBody:   ev.SealedBody,
			SessionTitle: ev.SessionTitle,
			Cwd:          ev.Cwd,
			FailureCount: failCount,
			OutputTail:   ev.OutputTail,
		})
		return json.Marshal(card)
	})
}

func (d *Dispatcher) DispatchWaitingInput(ctx context.Context, ev WaitingInputDispatchEvent) {
	// The session-level key gates all waiting-input sends for this session,
	// regardless of whether the trigger came from the hook or heuristic path.
	sessionKey := "waiting:" + ev.SessionID.String()

	key := ev.DedupKey
	if key == "" {
		key = sessionKey
	}

	d.dispatchWaiting(ctx, ev.SessionID, key, sessionKey, func() ([]byte, error) {
		var card internalfeishu.Card
		if len(ev.Options) > 0 {
			opts := make([]internalfeishu.AskOption, 0, len(ev.Options))
			for i, o := range ev.Options {
				opts = append(opts, internalfeishu.AskOption{
					Label:       o.Label,
					Description: o.Description,
					InjectText:  optionInjectText(i, o),
				})
			}
			card = internalfeishu.RenderAskQuestionCard(internalfeishu.AskQuestionInput{
				SessionID: ev.SessionID,
				Question:  ev.QuestionText,
				Options:   opts,
			})
		} else {
			card = internalfeishu.RenderWaitingInputCard(internalfeishu.WaitingInputInput{
				SessionID:      ev.SessionID,
				IdleForSeconds: ev.IdleForSeconds,
				QuestionText:   ev.QuestionText,
				SessionTitle:   ev.SessionTitle,
				Cwd:            ev.Cwd,
				CurrentCommand: ev.CurrentCommand,
				RecentOutput:   ev.RecentOutput,
			})
		}
		return json.Marshal(card)
	})
}

// dispatchWaiting is like dispatch but also blocks on and records the
// session-level key, preventing heuristic fallbacks from firing after a
// hook-sourced card has been sent within the dedup window.
func (d *Dispatcher) dispatchWaiting(ctx context.Context, sid uuid.UUID, dedupKey, sessionKey string, render func() ([]byte, error)) {
	now := d.cfg.Now()

	d.muD.Lock()
	// Block if either the specific key or the session-level key is still fresh.
	if last, ok := d.lastDispatch[dedupKey]; ok && now-last < dedupWindowSeconds {
		d.muD.Unlock()
		return
	}
	if sessionKey != dedupKey {
		if last, ok := d.lastDispatch[sessionKey]; ok && now-last < dedupWindowSeconds {
			d.muD.Unlock()
			return
		}
	}
	// Stamp eagerly — under the same lock as the check — so a concurrent
	// caller (e.g. hook + heuristic racing for the same WaitingInput
	// event) sees us as in-flight and bails. We may roll back below if
	// the send turns out to be impossible (no token).
	d.lastDispatch[dedupKey] = now
	if sessionKey != dedupKey {
		d.lastDispatch[sessionKey] = now
	}
	d.muD.Unlock()

	rollback := func() {
		d.muD.Lock()
		defer d.muD.Unlock()
		// Only roll back our own stamp — a newer dispatch may have replaced it.
		if d.lastDispatch[dedupKey] == now {
			delete(d.lastDispatch, dedupKey)
		}
		if sessionKey != dedupKey && d.lastDispatch[sessionKey] == now {
			delete(d.lastDispatch, sessionKey)
		}
	}

	tok, openID, _, err := d.cfg.Token.Get(ctx)
	if err != nil {
		rollback()
		if errors.Is(err, ErrTokenNotConfigured) || errors.Is(err, ErrTokenDisabled) {
			return
		}
		log.Printf("feishu: dispatch token: %v", err)
		return
	}
	if openID == "" {
		rollback()
		return
	}

	body, err := render()
	if err != nil {
		// Render errors are programming bugs, not transient — keep the
		// stamp so we don't retry-storm on the same broken event.
		log.Printf("feishu: render card: %v", err)
		return
	}

	mid, err := d.cfg.IM.SendInteractiveToOpenID(ctx, tok, openID, body)
	if err != nil {
		d.recordSendError(ctx, sid, err)
		// Keep the stamp — gate retries to the dedup window.
		return
	}
	d.cardMsgs.remember(mid, sid)
}

func (d *Dispatcher) dispatch(ctx context.Context, sid uuid.UUID, dedupKey string, render func() ([]byte, error)) {
	now := d.cfg.Now()

	d.muD.Lock()
	if last, ok := d.lastDispatch[dedupKey]; ok && now-last < dedupWindowSeconds {
		d.muD.Unlock()
		return
	}
	d.lastDispatch[dedupKey] = now
	d.muD.Unlock()

	rollback := func() {
		d.muD.Lock()
		defer d.muD.Unlock()
		if d.lastDispatch[dedupKey] == now {
			delete(d.lastDispatch, dedupKey)
		}
	}

	tok, openID, _, err := d.cfg.Token.Get(ctx)
	if err != nil {
		rollback()
		if errors.Is(err, ErrTokenNotConfigured) || errors.Is(err, ErrTokenDisabled) {
			return
		}
		log.Printf("feishu: dispatch token: %v", err)
		return
	}
	if openID == "" {
		rollback()
		return
	}

	body, err := render()
	if err != nil {
		log.Printf("feishu: render card: %v", err)
		return
	}

	mid, err := d.cfg.IM.SendInteractiveToOpenID(ctx, tok, openID, body)
	if err != nil {
		d.recordSendError(ctx, sid, err)
		return
	}
	d.cardMsgs.remember(mid, sid)
}

// SendAnchorCard POSTs the given card body to the configured open_id and
// returns (msgID, cardToken, openID, error). It resolves the tenant token via
// the configured TokenSource so callers do not need to manage credentials.
// Returns ErrTokenNotConfigured / ErrTokenDisabled transparently.
func (d *Dispatcher) SendAnchorCard(ctx context.Context, cardBody []byte) (msgID, cardToken, openID string, err error) {
	if d.cfg.CardKit == nil {
		return "", "", "", fmt.Errorf("feishu dispatcher: no CardKitClient configured")
	}
	tok, oid, _, err := d.cfg.Token.Get(ctx)
	if err != nil {
		return "", "", "", err
	}
	if oid == "" {
		return "", "", "", fmt.Errorf("feishu dispatcher: open_id empty (not bound)")
	}
	mid, tok2, err := d.cfg.CardKit.SendAnchorCard(ctx, tok, oid, cardBody)
	if err != nil {
		return "", "", "", err
	}
	return mid, tok2, oid, nil
}

// PatchAnchor patches the live body markdown of an anchor card. tenantToken
// must be a valid tenant_access_token; callers should obtain it via
// SendAnchorCard's returned triple or refresh via the TokenSource. sequence
// is strictly increasing per card (use CardAnchor.PatchSeq). Always targets
// the body markdown element identified by internalfeishu.AnchorBodyElementID.
func (d *Dispatcher) PatchAnchor(ctx context.Context, tenantToken, cardToken, bodyMarkdown string, sequence int64) error {
	if d.cfg.CardKit == nil {
		return fmt.Errorf("feishu dispatcher: no CardKitClient configured")
	}
	return d.cfg.CardKit.PatchCard(ctx, tenantToken, cardToken, internalfeishu.AnchorBodyElementID, bodyMarkdown, sequence)
}

// ClearAnchorInput resets the anchor card's input element's default_value to
// "" so the next user reply starts with an empty textarea. Called after each
// successful inject from kind=input so the previous text doesn't linger.
func (d *Dispatcher) ClearAnchorInput(ctx context.Context, tenantToken, cardToken string, sequence int64) error {
	if d.cfg.CardKit == nil {
		return fmt.Errorf("feishu dispatcher: no CardKitClient configured")
	}
	return d.cfg.CardKit.PatchCardElement(ctx, tenantToken, cardToken,
		internalfeishu.AnchorInputElementID,
		map[string]any{"default_value": ""}, sequence)
}

// PatchAnchorElement is the generic element-PATCH passthrough — used by the
// anchor button-row swap (AskUserQuestion options ↔ default keystrokes).
// Other call sites should prefer the typed helpers (PatchAnchor /
// ClearAnchorInput) when one fits.
func (d *Dispatcher) PatchAnchorElement(ctx context.Context, tenantToken, cardToken, elementID string, partial map[string]any, sequence int64) error {
	if d.cfg.CardKit == nil {
		return fmt.Errorf("feishu dispatcher: no CardKitClient configured")
	}
	return d.cfg.CardKit.PatchCardElement(ctx, tenantToken, cardToken, elementID, partial, sequence)
}

// GetToken returns a fresh (tenantToken, openID) pair via the configured
// TokenSource. Used by relay_host when it needs to PATCH an anchor card
// and must refresh the token independently of a SendAnchorCard call.
func (d *Dispatcher) GetToken(ctx context.Context) (tenantToken, openID string, err error) {
	tok, oid, _, e := d.cfg.Token.Get(ctx)
	return tok, oid, e
}

// AttachAIChunker registers a chunker for streaming AI turn events for the
// given session. Called by relay_host when a FeishuSubscriber attaches to
// an AI session. Replaces any existing chunker for the session.
func (d *Dispatcher) AttachAIChunker(sessionID string, ch *internalfeishu.AIChunker) {
	d.aiMu.Lock()
	defer d.aiMu.Unlock()
	if d.aiChunkers == nil {
		d.aiChunkers = map[string]*internalfeishu.AIChunker{}
	}
	d.aiChunkers[sessionID] = ch
}

// DetachAIChunker removes the chunker for the session. Safe when none exists.
func (d *Dispatcher) DetachAIChunker(sessionID string) {
	d.aiMu.Lock()
	defer d.aiMu.Unlock()
	delete(d.aiChunkers, sessionID)
}

// DispatchTurn forwards a parsed TurnEvent into the per-session AIChunker.
// Silently no-ops when no chunker is attached (the session may be shell-only,
// or remote-terminal may be disabled). Translates TurnKind into the
// internalfeishu.Turn*Event types AIChunker.PushTurn expects.
func (d *Dispatcher) DispatchTurn(sessionID string, ev TurnEvent) {
	d.aiMu.Lock()
	chunker := d.aiChunkers[sessionID]
	known := len(d.aiChunkers)
	onButtons := d.onAnchorButtons
	d.aiMu.Unlock()
	if chunker == nil {
		log.Printf("feishu-turn: no chunker sid=%s kind=%v known_chunkers=%d", sessionID, ev.Kind, known)
		return
	}
	log.Printf("feishu-turn: route sid=%s kind=%v text_len=%d opts=%d", sessionID, ev.Kind, len(ev.Text), len(ev.Options))
	switch ev.Kind {
	case TurnUserPrompt:
		chunker.PushTurn(internalfeishu.TurnUserPromptEvent{Text: ev.Text})
	case TurnAssistantFinal:
		chunker.PushTurn(internalfeishu.TurnAssistantFinalEvent{Text: ev.Text})
	case TurnToolStart:
		chunker.PushTurn(internalfeishu.TurnToolStartEvent{ToolName: ev.ToolName})
	case TurnToolEnd:
		chunker.PushTurn(internalfeishu.TurnToolEndEvent{ToolName: ev.ToolName, ToolBody: ev.ToolBody})
	}
	// Anchor button-row swap: AskUserQuestion (TurnAssistantFinal with
	// Options) replaces the default keystroke buttons with clickable option
	// buttons. Any other AssistantFinal — i.e. a regular Stop — restores the
	// default buttons (no-op when already default; cost is one PATCH per
	// turn end, acceptable).
	if onButtons != nil && ev.Kind == TurnAssistantFinal {
		onButtons(sessionID, ev.Options)
	}
}

// SetOnAnchorButtons registers the per-session button-swap callback.
// relay_host wires this to its PATCH path: non-nil options → option buttons;
// nil options → restore default keystroke row.
func (d *Dispatcher) SetOnAnchorButtons(fn func(sessionID string, options []string)) {
	d.aiMu.Lock()
	d.onAnchorButtons = fn
	d.aiMu.Unlock()
}

type feishuAuthClass interface {
	IsFeishuAuthClassError() bool
}

func (d *Dispatcher) recordSendError(ctx context.Context, sid uuid.UUID, err error) {
	var ac feishuAuthClass
	if errors.As(err, &ac) && ac.IsFeishuAuthClassError() {
		d.muD.Lock()
		d.authFailures++
		count := d.authFailures
		d.muD.Unlock()
		if count >= maxAuthFails {
			if setErr := d.cfg.Store.SetDisabled(ctx); setErr != nil && !errors.Is(setErr, ErrRelayManagedBoundState) {
				log.Printf("feishu: SetDisabled: %v", setErr)
			}
			d.cfg.Token.Invalidate()
		}
		return
	}
	log.Printf("feishu: send to %s: %v", sid, err)
}
