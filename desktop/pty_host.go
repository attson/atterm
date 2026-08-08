package main

import (
	"encoding/hex"
	"fmt"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/ptyhost"
)

// desktopPtyHost is the desktop-side adapter that satisfies the relay's
// ptyhost.PtyHost interface (see internal/relay/adopt.go). It embeds the
// bare *ptyhost.Host that owns the OS PTY master and layers on:
//
//   - PasteImage  (defined in paste_image.go) — receives an image paste
//     from the relay, materialises it into the cache dir, tries the native
//     OS clipboard, and falls back to typing the path if that fails.
//   - PasteFile   (defined in paste_file.go)  — same flow for arbitrary
//     files; adds the file to the OS-native filenames pasteboard when
//     available.
//   - Write       (below) — wraps every PTY input write with an optional
//     debug log so users chasing "why did my TUI just cancel mid-turn?"
//     can see the raw bytes their prompt is receiving.
//
// The struct lives here rather than in paste_image.go because it is the
// shared adapter for every host-side capability, not an image-paste
// concern.
type desktopPtyHost struct {
	*ptyhost.Host
	cfg *configStore
}

// ptyInputDebugTag flags an input write whose lone/leading ESC is the
// suspected cause of mid-turn cancellations in child TUIs.
func ptyInputDebugTag(p []byte) string {
	switch {
	case len(p) == 1 && p[0] == 0x1b:
		return " LONE-ESC"
	case len(p) > 0 && p[0] == 0x1b:
		return " ESC-LEAD"
	default:
		return ""
	}
}

// logPtyInput emits a DEBUG [pty-input] line for one PTY write when the
// config toggle is on. Never blocks or affects the actual write.
//
// It writes through EmitForced, bypassing the configured log level. The toggle
// *is* the gate here: someone who ticks "log terminal input bytes" and then
// sees an empty log because the level happens to sit at INFO would reasonably
// conclude the feature is broken.
func logPtyInput(cfg *configStore, p []byte) {
	if cfg == nil || !cfg.Get().PtyInputDebugEnabledOrDefault() {
		return
	}
	logging.EmitForced(logging.LevelDebug, "pty-input", fmt.Sprintf(
		"write n=%d hex=%s%s", len(p), hex.EncodeToString(p), ptyInputDebugTag(p)))
}

// Write intercepts all session input for optional debug logging, then
// forwards to the underlying PTY master.
func (h *desktopPtyHost) Write(p []byte) (int, error) {
	logPtyInput(h.cfg, p)
	return h.Host.Write(p)
}
