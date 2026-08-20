package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attson/atterm/internal/proto"
	"nhooyr.io/websocket"
)

const prefsWatchReadLimit = 1 << 20

func (a *App) applyRelayPrefsWatch(cfg appConfig) {
	a.mu.Lock()
	if a.prefsWatchCancel != nil {
		a.prefsWatchCancel()
		a.prefsWatchCancel = nil
	}
	if cfg.RelayURL == "" || cfg.RelayPaused || cfg.RelaySessionToken == "" || a.prefsSync == nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.prefsWatchCancel = cancel
	a.mu.Unlock()

	go a.runRelayPrefsWatch(ctx, cfg)
}

func (a *App) runRelayPrefsWatch(ctx context.Context, cfg appConfig) {
	base := strings.TrimRight(uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL), "/")
	url := base + "/client-sessions"
	backoff := 500 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return
		}
		if err := a.runRelayPrefsWatchOnce(ctx, url, cfg.RelaySessionToken, cfg.AllowInsecureRelay); err != nil && ctx.Err() == nil {
			logWarn("prefs", "relay prefs watch: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

func (a *App) runRelayPrefsWatchOnce(ctx context.Context, url, token string, allowInsecure bool) error {
	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	}
	if allowInsecure {
		opts.HTTPClient = relayHTTPClient(true, 0)
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, url, opts)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusInternalError, "")
	conn.SetReadLimit(prefsWatchReadLimit)

	for {
		f, err := readPrefsWatchFrame(ctx, conn)
		if err != nil {
			return err
		}
		if f.Type != proto.TypePrefsChanged {
			continue
		}
		// Hands off to the serial sync loop (prefs_sync_loop.go) instead of
		// pulling directly: this frame's ctx is the watch connection's own
		// (cancelled on relay-config change, independent of a.ctx), but the
		// pull itself must run serialised against every other prefsSync
		// caller, which only a.ctx-scoped work on the loop goroutine can do.
		// Non-blocking, so a burst of change notifications coalesces into
		// one pull instead of one per frame.
		a.enqueueSync(syncRequest{pull: true})
	}
}

func readPrefsWatchFrame(ctx context.Context, conn *websocket.Conn) (proto.Frame, error) {
	mt, data, err := conn.Read(ctx)
	if err != nil {
		return proto.Frame{}, err
	}
	if mt != websocket.MessageBinary {
		return proto.Frame{}, fmt.Errorf("prefs-watch: non-binary websocket message")
	}
	return proto.Unmarshal(data)
}
