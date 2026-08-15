package session

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
)

const defaultSilenceThresholdMS int64 = 5000
const defaultSilenceRestoreByteThreshold int64 = 256
const defaultSilenceResizeGraceMS int64 = 1500
const silenceActivityBurstWindow = time.Second

func envSilenceDetectEnabled() bool {
	v := os.Getenv("ATTERM_TASK_SILENCE_DETECT")
	if v == "" {
		return true
	}
	return v != "0" && !strings.EqualFold(v, "false")
}

func envSilenceThresholdMS() int64 {
	v := os.Getenv("ATTERM_TASK_SILENCE_THRESHOLD_MS")
	if v == "" {
		return defaultSilenceThresholdMS
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultSilenceThresholdMS
	}
	return n
}

// envSilenceRestoreByteThreshold reads the byte-accumulator threshold for
// restoring running from heuristic waiting_input. A single cursor blink is
// typically a handful of bytes (`\x1b[?25l\x1b[?25h`); meaningful AI output
// is hundreds. The default (256) keeps the sidebar in waiting_input through
// claude's idle spinner/cursor redraws but flips back to running as soon as
// real content streams.
func envSilenceRestoreByteThreshold() int64 {
	v := os.Getenv("ATTERM_TASK_SILENCE_RESTORE_BYTES")
	if v == "" {
		return defaultSilenceRestoreByteThreshold
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultSilenceRestoreByteThreshold
	}
	return n
}

// envSilenceResizeGraceMS reads the grace window (ms) during which output
// arriving after a PTY resize is treated as SIGWINCH-driven repaint, not
// real activity, so it does not push the silence-restore accumulator.
func envSilenceResizeGraceMS() int64 {
	v := os.Getenv("ATTERM_TASK_SILENCE_RESIZE_GRACE_MS")
	if v == "" {
		return defaultSilenceResizeGraceMS
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return defaultSilenceResizeGraceMS
	}
	return n
}

// silenceAppliesToLocked decides whether the silence heuristic should apply
// to this session in its current state. The alt-screen guard catches classic
// TUIs (vim, fzf, older Claude Code), but Claude Code v2.x — and similar
// inline-rendering AI clients — render their prompt directly into the normal
// screen, never flipping alt-screen on. For those we relax the requirement:
// any session classified as ai is treated as a candidate regardless of
// alt-screen state. test/build/deploy types are deliberately NOT included
// because they routinely run silently while genuinely working; flipping to
// waiting_input there would create false unread badges.
func (s *Session) silenceAppliesToLocked() bool {
	return s.altScreen || s.meta.Type == SessionTypeAI
}

func (s *Session) markSilenceActivityLocked(now time.Time) {
	s.lastOutputMono = now
	s.silenceActivityBytes = 0
	s.silenceActivityStartedMono = time.Time{}
}

func (s *Session) resetSilenceActivityBurstLocked() {
	s.silenceActivityBytes = 0
	s.silenceActivityStartedMono = time.Time{}
}

// resetSilenceRestoreLocked clears the restore accumulator together with its
// burst window. Every non-restore transition goes through here so the counter
// and the window it is measured over cannot drift apart.
func (s *Session) resetSilenceRestoreLocked() {
	s.silenceRestoreBytes = 0
	s.silenceRestoreStartedMono = time.Time{}
}

// noteSilenceRestoreLocked accumulates output arriving while the session sits
// in heuristic waiting_input and reports whether it now looks like real
// content rather than a TUI redraw.
//
// This is the mirror image of noteRunningSilenceActivityLocked and MUST stay
// symmetric with it: both sides ask "did at least silenceRestoreByteThreshold
// bytes arrive inside one silenceActivityBurstWindow?". An asymmetric pair is
// an oscillator. The restore side used to accumulate with no window at all, so
// a drip too thin to *hold* running (blinking cursor, spinner frame) still
// summed its way past the threshold given enough seconds, restored running,
// and the silence timer flipped it straight back — a session-lifetime
// live→now→live blink in the sidebar and desk widget.
func (s *Session) noteSilenceRestoreLocked(data []byte, now time.Time) bool {
	chunkBytes := int64(len(data))
	if s.silenceRestoreStartedMono.IsZero() ||
		now.Sub(s.silenceRestoreStartedMono) > silenceActivityBurstWindow {
		s.silenceRestoreStartedMono = now
		s.silenceRestoreBytes = chunkBytes
	} else {
		s.silenceRestoreBytes += chunkBytes
	}
	return s.silenceRestoreBytes >= s.silenceRestoreByteThreshold
}

func (s *Session) noteRunningSilenceActivityLocked(data []byte, now time.Time) {
	if s.meta.TaskState != proto.TaskStateRunning {
		return
	}
	if !s.silenceAppliesToLocked() {
		s.markSilenceActivityLocked(now)
		return
	}
	chunkBytes := int64(len(data))
	if chunkBytes >= s.silenceRestoreByteThreshold {
		s.markSilenceActivityLocked(now)
		return
	}
	if s.silenceActivityStartedMono.IsZero() ||
		now.Sub(s.silenceActivityStartedMono) > silenceActivityBurstWindow {
		s.silenceActivityStartedMono = now
		s.silenceActivityBytes = chunkBytes
	} else {
		s.silenceActivityBytes += chunkBytes
	}
	if s.silenceActivityBytes >= s.silenceRestoreByteThreshold {
		s.markSilenceActivityLocked(now)
	}
}

// rescheduleSilenceTimerLocked arms (or re-arms) the per-session silence
// timer. Caller must hold s.mu.Lock(). Stops any existing timer first; only
// arms a new one when detection is enabled, the session is running and not
// closed, and silenceAppliesToLocked() says the heuristic is in scope.
func (s *Session) rescheduleSilenceTimerLocked() {
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
	if !s.silenceDetectEnabled {
		silenceDebugLocked(s, "arm-skip: detect-disabled")
		return
	}
	if s.closed {
		return
	}
	if s.meta.TaskState != proto.TaskStateRunning {
		silenceDebugLocked(s, fmt.Sprintf("arm-skip: state=%q (not running)", s.meta.TaskState))
		return
	}
	if !s.silenceAppliesToLocked() {
		silenceDebugLocked(s, fmt.Sprintf("arm-skip: not applicable (alt=%v type=%q)",
			s.altScreen, s.meta.Type))
		return
	}
	d := time.Duration(s.silenceThresholdMS) * time.Millisecond
	if d <= 0 {
		return
	}
	if !s.lastOutputMono.IsZero() {
		elapsed := time.Since(s.lastOutputMono)
		if elapsed >= d {
			d = time.Millisecond
		} else {
			d -= elapsed
		}
	}
	silenceDebugLocked(s, fmt.Sprintf("arm: in %v (type=%q alt=%v)", d, s.meta.Type, s.altScreen))
	s.silenceTimer = time.AfterFunc(d, s.onSilenceFired)
}

// onSilenceFired runs in the timer's own goroutine after the configured
// silence threshold has elapsed since the last output. It takes the lock
// and re-checks every guard — state, altScreen, closed, and the actual
// silence duration — because anything could have changed between arming
// and firing. If the session is still genuinely silent in an alt-screen
// running task, flip to waiting_input, bump AttentionAt, mark the
// transition as "from silence", and broadcast META. If it isn't silent
// long enough yet (e.g. output arrived after the timer was scheduled but
// before LastOutputAt updates raced), simply re-arm and let the next fire
// settle it.
func (s *Session) onSilenceFired() {
	s.mu.Lock()
	if s.closed {
		silenceDebugLocked(s, "fire-skip: closed")
		s.mu.Unlock()
		return
	}
	if !s.silenceDetectEnabled {
		silenceDebugLocked(s, "fire-skip: detect-disabled")
		s.mu.Unlock()
		return
	}
	if s.meta.TaskState != proto.TaskStateRunning {
		silenceDebugLocked(s, fmt.Sprintf("fire-skip: state=%q (not running)", s.meta.TaskState))
		s.mu.Unlock()
		return
	}
	if !s.silenceAppliesToLocked() {
		silenceDebugLocked(s, fmt.Sprintf("fire-skip: not applicable (alt=%v type=%q)",
			s.altScreen, s.meta.Type))
		s.mu.Unlock()
		return
	}
	now := time.Now()
	threshold := time.Duration(s.silenceThresholdMS) * time.Millisecond
	if s.lastOutputMono.IsZero() || now.Sub(s.lastOutputMono) < threshold {
		silenceDebugLocked(s, fmt.Sprintf("fire-rearm: idle=%v < threshold=%v (mono-zero=%v)",
			now.Sub(s.lastOutputMono), threshold, s.lastOutputMono.IsZero()))
		s.rescheduleSilenceTimerLocked()
		s.mu.Unlock()
		return
	}
	silenceDebugLocked(s, fmt.Sprintf("fire-flip: state running -> waiting_input (idle=%v, type=%q, alt=%v)",
		now.Sub(s.lastOutputMono), s.meta.Type, s.altScreen))
	s.meta.TaskState = proto.TaskStateWaitingInput
	s.meta.AttentionAt = now.Unix()
	s.waitingFromSilence = true
	s.silenceRestoreBytes = 0
	s.resetSilenceActivityBurstLocked()
	s.fireTaskStateLocked(proto.TaskStateRunning, proto.TaskStateWaitingInput, TaskMeta{})
	metaHook := s.onMetaChanged
	s.mu.Unlock()
	s.broadcastCurrentMeta()
	if metaHook != nil {
		metaHook()
	}
}

var silenceDebugEnabled = os.Getenv("ATTERM_DEBUG_SILENCE") == "1"

// silenceDebugLocked emits a one-line trace when ATTERM_DEBUG_SILENCE=1
// is set in the environment. Cheap (literally one bool check) when off.
// Caller must hold s.mu so it can safely read meta fields.
func silenceDebugLocked(s *Session, msg string) {
	if !silenceDebugEnabled {
		return
	}
	logging.EmitForced(logging.LevelDebug, "silence", fmt.Sprintf(
		"sid=%s cmd=%q %s", s.ID.String()[:8], s.meta.CurrentCommand, msg))
}
