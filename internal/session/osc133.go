package session

import (
	"bytes"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/attson/atterm/internal/proto"
)

func (s *Session) applyOSC133Locked(data []byte, now time.Time) bool {
	events := s.consumeOSC133Locked(data)
	changed := false
	for _, payload := range events {
		if payload == "" {
			continue
		}
		switch payload[0] {
		case 'A', 'B':
			// Prompt start/end: the shell is ready for input. Fire the
			// first-prompt hook once (used to inject a restored AI resume).
			if !s.firstPromptFired && s.onFirstPrompt != nil {
				s.firstPromptFired = true
				s.onFirstPrompt()
			}
		case 'C':
			command := strings.TrimSpace(strings.TrimPrefix(payload, "C;"))
			exitNil := (*int)(nil)
			prevStateC := s.meta.TaskState
			if s.meta.TaskState != proto.TaskStateRunning {
				s.meta.TaskState = proto.TaskStateRunning
				changed = true
			}
			if s.waitingFromSilence {
				s.waitingFromSilence = false
				s.resetSilenceRestoreLocked()
				changed = true
			}
			if s.meta.CurrentCommand != command {
				s.meta.CurrentCommand = command
				changed = true
			}
			newType := ClassifyCommand(command)
			if newType == SessionTypeAI && s.onAIClassified != nil {
				// Every OSC 133 C is a top-level shell command boundary. Report
				// every AI launch, even while the sticky session type is already
				// ai, so desktop can replace the previous CLI's recovery resolver.
				s.onAIClassified(command, s.meta.Cwd)
			}
			// Sticky non-shell classification: shell does not overwrite
			// test/build/deploy tags. AI is the exception once its command has
			// explicitly ended and the shell starts a new ordinary command.
			if s.meta.Type == SessionTypeAI && newType == SessionTypeShell && s.aiCommandFinished {
				s.meta.Type = SessionTypeShell
				changed = true
			} else if newType != SessionTypeShell && s.meta.Type != newType {
				s.meta.Type = newType
				changed = true
			}
			s.aiCommandFinished = false
			started := now.Unix()
			s.cmdStarted = now
			s.markSilenceActivityLocked(now)
			if s.meta.CommandStartedAt != started {
				s.meta.CommandStartedAt = started
				changed = true
			}
			if s.meta.CommandEndedAt != 0 {
				s.meta.CommandEndedAt = 0
				changed = true
			}
			if s.meta.CommandDurationMS != 0 {
				s.meta.CommandDurationMS = 0
				changed = true
			}
			if s.meta.CommandExitCode != nil {
				s.meta.CommandExitCode = exitNil
				changed = true
			}
			s.fireTaskStateLocked(prevStateC, proto.TaskStateRunning, TaskMeta{Label: command})
		case 'D':
			if s.meta.TaskState != proto.TaskStateRunning && s.meta.CommandStartedAt == 0 {
				continue
			}
			prevStateD := s.meta.TaskState
			exitCode := parseOSC133Exit(payload)
			state := proto.TaskStateCompleted
			if exitCode != 0 {
				state = proto.TaskStateFailed
			}
			if s.meta.TaskState != state {
				s.meta.TaskState = state
				changed = true
			}
			ended := now.Unix()
			if s.meta.CommandEndedAt != ended {
				s.meta.CommandEndedAt = ended
				changed = true
			}
			startedAt := s.cmdStarted
			if startedAt.IsZero() {
				startedAt = time.Unix(s.meta.CommandStartedAt, 0)
			}
			duration := int(now.Sub(startedAt).Milliseconds())
			if duration < 0 {
				duration = 0
			}
			if s.meta.CommandDurationMS != duration {
				s.meta.CommandDurationMS = duration
				changed = true
			}
			if s.meta.CommandExitCode == nil || *s.meta.CommandExitCode != exitCode {
				v := exitCode
				s.meta.CommandExitCode = &v
				changed = true
			}
			s.aiCommandFinished = ClassifyCommand(s.meta.CurrentCommand) == SessionTypeAI
			// Capture a structured summary of this command's tail output.
			// Always populate Summary on D so clients can show the most
			// recent context; ErrorLines is filled only when the command
			// failed (extractErrorLines on lines we already split).
			s.meta.Summary = computeSummary(s.scroll, now, exitCode != 0)
			if isAttentionType(s.meta.Type) {
				s.meta.AttentionAt = now.Unix()
			}
			if s.waitingFromSilence {
				s.waitingFromSilence = false
				s.resetSilenceRestoreLocked()
				s.resetSilenceActivityBurstLocked()
			}
			// The AI CLI just exited; its hooks are gone with it. Hand the
			// session back to the heuristic, or it would sit on whatever state
			// the last hook reported forever.
			s.clearHookDrivenLocked()
			if s.silenceTimer != nil {
				s.silenceTimer.Stop()
				s.silenceTimer = nil
			}
			changed = true
			var recentOutput string
			if s.meta.Summary != nil {
				recentOutput = s.meta.Summary.RecentOutput
			}
			s.fireTaskStateLocked(prevStateD, state, TaskMeta{
				ExitCode:     exitCode,
				ElapsedMS:    duration,
				Label:        s.meta.CurrentCommand,
				RecentOutput: recentOutput,
			})
		}
	}
	return changed
}

// applyOSCTitleLocked scans data for OSC 0/1/2 (icon/window title) sequences
// and updates s.meta.Title in place when the title changes. Returns true
// when the caller should broadcast a META frame. Same lock window as
// applyOSC133Locked; independent of osc133Buf. See osc_title.go for the
// underlying scanner.
func (s *Session) applyOSCTitleLocked(data []byte) bool {
	combined := append(append([]byte(nil), s.oscTitleBuf...), data...)
	titles, consumed, ok := scanOSCTitles(combined)
	tail := combined[consumed:]
	const maxBuf = maxOSCTitlePayload + 8 // payload cap + introducer slack
	if len(tail) > maxBuf {
		tail = tail[len(tail)-maxBuf:]
	}
	s.oscTitleBuf = append(s.oscTitleBuf[:0], tail...)
	if !ok || len(titles) == 0 {
		return false
	}
	// Last-writer-wins: OSC 0/1/2 collapse to one field.
	newTitle := titles[len(titles)-1]
	if newTitle == s.meta.Title {
		return false
	}
	if s.shouldIgnoreCodexFallbackTitleLocked(newTitle) {
		return false
	}
	s.meta.Title = newTitle
	return true
}

func (s *Session) shouldIgnoreCodexFallbackTitleLocked(newTitle string) bool {
	if s.meta.Title == "" {
		return false
	}
	first, _ := commandExecutableTokens(s.meta.CurrentCommand)
	if first != "codex" {
		return false
	}
	newStripped := stripCodexAnimatedTitlePrefix(newTitle)
	currentStripped := stripCodexAnimatedTitlePrefix(s.meta.Title)
	if newStripped == "codex" {
		return currentStripped != "codex"
	}
	cwdBase := basenameFromCwd(s.meta.Cwd)
	return cwdBase != "" && newStripped == cwdBase && currentStripped != cwdBase
}

func basenameFromCwd(cwd string) string {
	stripped := strings.TrimRight(cwd, "/")
	if stripped == "" {
		return ""
	}
	parts := strings.Split(stripped, "/")
	return parts[len(parts)-1]
}

func stripCodexAnimatedTitlePrefix(title string) string {
	return strings.TrimSpace(strings.TrimLeftFunc(title, func(r rune) bool {
		return unicode.IsSpace(r) ||
			strings.ContainsRune(":：;·•∙.∷⋮⋯", r) ||
			(r >= '\u2800' && r <= '\u28ff')
	}))
}

func (s *Session) consumeOSC133Locked(data []byte) []string {
	buf := append(append([]byte(nil), s.osc133Buf...), data...)
	s.osc133Buf = s.osc133Buf[:0]
	var out []string
	prefix := []byte("\x1b]133;")
	for {
		idx := bytes.Index(buf, prefix)
		if idx < 0 {
			s.osc133Buf = keepOSC133Tail(s.osc133Buf, buf)
			return out
		}
		buf = buf[idx+len(prefix):]
		termStart, termLen := oscTerminator(buf)
		if termStart < 0 {
			// Keep the incomplete OSC so a split BEL/ST terminator can finish it
			// on the next chunk. Cap the buffer to avoid unbounded growth on
			// malformed terminal output.
			incomplete := append(prefix, buf...)
			const maxOSC = 4096
			if len(incomplete) > maxOSC {
				incomplete = incomplete[len(incomplete)-maxOSC:]
			}
			s.osc133Buf = append(s.osc133Buf, incomplete...)
			return out
		}
		out = append(out, string(buf[:termStart]))
		buf = buf[termStart+termLen:]
	}
}

func keepOSC133Tail(dst, buf []byte) []byte {
	const keep = len("\x1b]133;") - 1
	if len(buf) > keep {
		buf = buf[len(buf)-keep:]
	}
	return append(dst, buf...)
}

func oscTerminator(buf []byte) (int, int) {
	for i, b := range buf {
		if b == 0x07 {
			return i, 1
		}
		if b == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
			return i, 2
		}
	}
	return -1, 0
}

func parseOSC133Exit(payload string) int {
	idx := strings.IndexByte(payload, ';')
	if idx < 0 {
		return 0
	}
	raw := strings.TrimSpace(payload[idx+1:])
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return n
}

func looksLikeWaitingInput(data []byte) bool {
	text := strings.ToLower(string(data))
	for _, pattern := range []string{
		"[y/n]",
		"continue?",
		"proceed?",
		"confirm",
		"press enter",
		"password:",
	} {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
