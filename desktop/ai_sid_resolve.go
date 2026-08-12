package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/attson/atterm/internal/session"
)

// AI session-id resolution by title match.
//
// Restore needs the AI CLI's own session id so it can inject
// `claude --resume <id>`. The old approach watched the data dir for a *.jsonl
// whose mtime advanced (see the retired sniff in ai_sid_sniff.go) — it timed
// out on resumed/idle sessions and aborted on same-cwd concurrency.
//
// Claude writes a record `{"type":"ai-title","aiTitle":"<title>","sessionId":
// "<uuid>"}` into its jsonl; aiTitle is exactly the title atterm shows for the
// pane. So we resolve the id by matching the pane's (normalized) title against
// the aiTitle of every jsonl in the pane's cwd dir. This is robust to resumed
// sessions (the file already exists — we don't care) and disambiguates
// multiple claude sessions sharing a cwd (the title is the discriminator).

// aiTitleEntry is one resolved jsonl: its session id, its latest aiTitle, and
// the file's mtime (used only as a last-resort tiebreak).
type aiTitleEntry struct {
	SessionID string
	AITitle   string
	ModTime   time.Time
}

// cachedTitle memoizes a single jsonl's scan result, keyed by file mtime so an
// unchanged file is never re-scanned across poll ticks.
type cachedTitle struct {
	mod   time.Time
	sid   string
	title string
	has   bool
}

type titleCache map[string]cachedTitle

// scanJsonlForAITitle scans one *.jsonl for ai-title records and returns the
// LAST one's (sessionId, aiTitle) — the title evolves over a session, so the
// most recent record reflects what the pane currently shows. ok is false when
// no ai-title record with a non-empty title exists.
//
// A substring pre-filter avoids JSON-decoding every line; the scanner buffer
// is enlarged because a single jsonl line can be megabytes.
func scanJsonlForAITitle(path string) (sid, title string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	needle := []byte(`"ai-title"`)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, needle) {
			continue
		}
		var rec struct {
			Type      string `json:"type"`
			AITitle   string `json:"aiTitle"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "ai-title" {
			continue
		}
		if rec.AITitle == "" {
			continue
		}
		sid, title, ok = rec.SessionID, rec.AITitle, true // last-wins
	}
	return sid, title, ok
}

// readClaudeTitleEntries returns one entry per *.jsonl in dir that has a
// resolvable aiTitle. cache (may be nil) skips re-scanning files whose mtime
// is unchanged. SessionID falls back to the filename stem when the ai-title
// record omitted it.
func readClaudeTitleEntries(dir string, cache titleCache) []aiTitleEntry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []aiTitleEntry
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		mod := info.ModTime()

		c, hit := cache[path]
		if !hit || !c.mod.Equal(mod) {
			sid, title, has := scanJsonlForAITitle(path)
			if has && sid == "" {
				if stem, ok := claudeParseSid(e.Name()); ok {
					sid = stem
				}
			}
			c = cachedTitle{mod: mod, sid: sid, title: title, has: has && sid != ""}
			if cache != nil {
				cache[path] = c
			}
		}
		if !c.has || c.title == "" {
			continue
		}
		out = append(out, aiTitleEntry{SessionID: c.sid, AITitle: c.title, ModTime: mod})
	}
	return out
}

// spinnerDecoration reports whether r is leading visual chrome claude prepends
// to its title — Braille spinner frames (U+2800–U+28FF), sparkle/bullet
// glyphs, or whitespace.
func spinnerDecoration(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	if r >= 0x2800 && r <= 0x28FF {
		return true
	}
	switch r {
	case '✻', '✶', '✳', '✽', '✲', '✱', '✦', '✧', '⋆', '★', '☆', '·', '•', '◦', '▪', '▫', '*':
		return true
	}
	return false
}

// normalizeAITitle canonicalizes a title for comparison: strip ANSI CSI runs
// and C0/DEL control bytes, drop leading spinner/sparkle decoration, collapse
// internal whitespace, trim. The generic "Claude Code" placeholder maps to ""
// so it never matches any file.
func normalizeAITitle(s string) string {
	// Strip ANSI CSI escape sequences (ESC [ ... final-byte).
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++ // consume final byte
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	// Drop C0 controls and DEL.
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, b.String())
	// Drop leading spinner/sparkle decoration.
	cleaned = strings.TrimLeftFunc(cleaned, spinnerDecoration)
	// Collapse internal whitespace and trim.
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if strings.EqualFold(cleaned, "Claude Code") {
		return ""
	}
	return cleaned
}

// resolveClaudeSessionID returns the session id whose aiTitle matches
// paneTitle. Requires a unique non-empty match; >1 match falls back to the
// newest mtime, but identical mtimes are treated as ambiguous (no result).
func resolveClaudeSessionID(dir, paneTitle string, cache titleCache) (string, bool) {
	want := normalizeAITitle(paneTitle)
	if want == "" {
		return "", false
	}
	entries := readClaudeTitleEntries(dir, cache)
	var matches []aiTitleEntry
	for _, e := range entries {
		if normalizeAITitle(e.AITitle) == want {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return "", false
	case 1:
		return matches[0].SessionID, true
	default:
		sort.Slice(matches, func(i, j int) bool { return matches[i].ModTime.After(matches[j].ModTime) })
		if matches[0].ModTime.Equal(matches[1].ModTime) {
			return "", false
		}
		return matches[0].SessionID, true
	}
}

// resolveFreshClaudeSessionID captures a brand-new session by its file: a
// freshly launched claude creates a new <uuid>.jsonl in its cwd dir and keeps
// writing startup records, so its mtime advances past `since`. If exactly one
// jsonl is active since then, its filename stem IS the session id — available
// immediately, before claude generates any ai-title. Returns false on zero (no
// fresh file yet) or ≥2 active files (ambiguous — left to title matching to
// disambiguate same-cwd concurrency). This must never guess: exactly-one only.
func resolveFreshClaudeSessionID(dir string, since time.Time) (string, bool) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var sids []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		if sid, ok := claudeParseSid(e.Name()); ok {
			sids = append(sids, sid)
		}
	}
	if len(sids) == 1 {
		return sids[0], true
	}
	return "", false
}

// readClaudeJsonlMtimes maps each <uuid>.jsonl's session id to its mtime.
func readClaudeJsonlMtimes(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sid, ok := claudeParseSid(e.Name())
		if !ok {
			continue
		}
		if info, err := e.Info(); err == nil {
			out[sid] = info.ModTime()
		}
	}
	return out
}

// advancedSids returns the session ids whose jsonl was written (mtime advanced,
// or the file newly appeared) between prev and cur — i.e. the conversation(s)
// claude is actively writing right now. claude writes metadata (mode /
// permission-mode / last-prompt) on a /resume switch even before the user
// types, so this catches a conversation switch promptly.
func advancedSids(prev, cur map[string]time.Time) []string {
	var out []string
	for sid, mt := range cur {
		if p, ok := prev[sid]; !ok || mt.After(p) {
			out = append(out, sid)
		}
	}
	return out
}

// claudeResolveInterval is the poll cadence for the continuous tracker.
// claudeFreshGrace widens the "active since" window slightly so a fresh file
// whose creation burst started just before the goroutine was scheduled is
// still seen.
const (
	claudeResolveInterval = 1 * time.Second
	claudeFreshGrace      = 3 * time.Second
)

// chooseNextSidContinuous picks the sid to emit on a continuous-tracking tick
// (the pane already has lastEmitted != ""). Returns "" to keep the current sid.
//
//   - adv         — session ids whose jsonl mtime advanced since the last tick
//   - titleMatch  — sid resolved by the pane's current OSC title, or "" if
//     the title didn't yield a unique match
//   - lastEmitted — the sid this pane most recently committed to
//
// The tricky case is "exactly one jsonl advanced and it isn't ours." It could
// be a /resume on THIS pane (claude writes metadata into the switched-to file
// even before the user types), OR it could be cross-talk from another claude
// pane in the same cwd writing into ITS own conversation. The first version of
// this tracker emitted unconditionally on a single advance, which under
// same-cwd concurrency overwrote pane B's tracked sid with pane A's writes —
// after a restart both panes resumed pane A's conversation. We now require the
// pane's title to also resolve to the advanced sid; a same-cwd peer's write
// doesn't touch this pane's title, so the switch is rejected. A real /resume
// updates the OSC title to the new conversation's title, so the switch
// completes within ~one tick.
//
// Multiple simultaneous advances (≥2) means concurrent writes from same-cwd
// peers — fall back to title match, which is the only authoritative signal.
func chooseNextSidContinuous(adv []string, titleMatch, lastEmitted string) string {
	switch len(adv) {
	case 0:
		return ""
	case 1:
		if adv[0] == lastEmitted {
			return ""
		}
		if adv[0] != "" && adv[0] == titleMatch {
			return adv[0]
		}
		return ""
	default:
		return titleMatch
	}
}

// startClaudeTitleResolve continuously tracks the session's ACTIVE claude
// conversation id for the session's lifetime (until ctx is cancelled on PTY
// exit), calling onCapture whenever it changes. Running once and stopping would
// miss a /resume switch to another conversation within the same process — the
// active jsonl changes and the captured id would go stale.
//
// Per tick:
//   - Initial capture (nothing emitted yet): title match (precise) else
//     fresh-file (a brand-new title-less session, by its just-created jsonl).
//   - Then track the conversation claude is actively WRITING: the jsonl whose
//     mtime advanced since the last tick. See chooseNextSidContinuous for the
//     decision rules — in particular, same-cwd cross-talk is rejected by
//     requiring the OSC title to agree before switching.
//
// The watch dir is recomputed from the session's LIVE cwd each tick (meta.Cwd
// can lag a recent `cd` at classification time).
func startClaudeTitleResolve(ctx context.Context, sess *session.Session, home string, onCapture func(sid string)) {
	cache := titleCache{}
	since := time.Now().Add(-claudeFreshGrace)
	lastEmitted := ""
	emit := func(sid string) {
		if sid == "" || sid == lastEmitted {
			return
		}
		lastEmitted = sid
		logInfo("recovery", "claude active conversation → sid=%s", sid)
		onCapture(sid)
	}
	// resolveLogged de-dupes the per-tick title→sid resolve trace: the tracker
	// polls once a second and resolveClaudeSessionID re-derives the same sid
	// every tick, so log only when the resolved sid actually changes. DEBUG
	// because it is diagnostic, not an event the user needs at INFO.
	lastResolved := ""
	resolveLogged := func(sid string) {
		if sid == lastResolved {
			return
		}
		lastResolved = sid
		logDebug("recovery", "claude resolve → sid=%s", sid)
	}
	var prev map[string]time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(claudeResolveInterval):
		}
		info := sess.Info()
		if info.Cwd == "" {
			continue
		}
		dir := claudeWatchDir(info.Cwd, time.Now(), home)
		cur := readClaudeJsonlMtimes(dir)

		if lastEmitted == "" {
			if sid, ok := resolveClaudeSessionID(dir, info.Title, cache); ok {
				resolveLogged(sid)
				emit(sid)
			} else if sid, ok := resolveFreshClaudeSessionID(dir, since); ok {
				emit(sid)
			}
			prev = cur
			continue
		}

		adv := advancedSids(prev, cur)
		prev = cur
		titleMatch := ""
		if sid, ok := resolveClaudeSessionID(dir, info.Title, cache); ok {
			resolveLogged(sid)
			titleMatch = sid
		}
		if sid := chooseNextSidContinuous(adv, titleMatch, lastEmitted); sid != "" {
			emit(sid)
		}
	}
}

// codexResolveInterval / codexResolveBudget bound the codex new-file watch.
const (
	codexResolveInterval = 200 * time.Millisecond
	codexResolveBudget   = 30 * time.Second
	codexTitleInterval   = 1 * time.Second
	codexTitleMaxRunes   = 80
)

// startCodexFileResolve keeps the filename heuristic for codex (its jsonl has
// no ai-title record). It baselines the day-dir's rollout ids, then waits for a
// single new one to appear, or a single existing same-cwd rollout to advance
// after the user selects it from Codex's resume picker. Ambiguous (≥2) aborts.
// Once captured, it keeps polling the rollout for the latest real user message
// and mirrors that into the session title; Codex's OSC title is only spinner +
// cwd basename.
func startCodexFileResolve(ctx context.Context, sess *session.Session, cwd string, onCapture func(sid string)) {
	home, err := os.UserHomeDir()
	if err != nil {
		logWarn("ai-sid", "no home for codex resolve: %v", err)
		return
	}
	dir := codexWatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logWarn("ai-sid", "mkdir %s: %v — skip codex resolve", dir, err)
		return
	}
	before := codexRolloutFileInfos(dir, cwd)
	deadline := time.Now().Add(codexResolveBudget)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(codexResolveInterval):
		}
		var fresh []string
		var advanced []string
		files := codexRolloutFileInfos(dir, cwd)
		for sid, file := range files {
			prev, seen := before[sid]
			if !seen {
				fresh = append(fresh, sid)
				continue
			}
			if file.ModTime.After(prev.ModTime) {
				advanced = append(advanced, sid)
			}
		}
		switch {
		case len(fresh) == 1:
			sid := fresh[0]
			onCapture(sid)
			trackCodexUserTitle(ctx, sess, files[sid].Path)
			return
		case len(fresh) >= 2:
			logWarn("ai-sid", "codex resolve ambiguous (%d new in %s) — abort", len(fresh), dir)
			return
		case len(advanced) == 1:
			sid := advanced[0]
			onCapture(sid)
			trackCodexUserTitle(ctx, sess, files[sid].Path)
			return
		case len(advanced) >= 2:
			logWarn("ai-sid", "codex resolve ambiguous (%d advanced in %s) — abort", len(advanced), dir)
			return
		}
		before = files
	}
	logWarn("ai-sid", "codex resolve timeout in %s", dir)
}

func trackCodexUserTitle(ctx context.Context, sess *session.Session, path string) {
	last := ""
	var lastModTime time.Time
	var lastSize int64 = -1
	for {
		if stat, err := os.Stat(path); err == nil {
			if stat.Size() != lastSize || !stat.ModTime().Equal(lastModTime) {
				lastSize = stat.Size()
				lastModTime = stat.ModTime()
				if title, ok := scanCodexJsonlForUserTitle(path); ok {
					last = title
				}
			}
		}
		// Codex's OSC title can overwrite this between rollout writes. Reapply
		// the cached user title without rescanning an unchanged, potentially
		// very large rollout file.
		if last != "" && sess.Info().Title != last {
			sess.UpdateCwdTitle("", last)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(codexTitleInterval):
		}
	}
}

func startCodexKnownTitleResolve(ctx context.Context, sess *session.Session, cwd, sid string) {
	if sid == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		logWarn("ai-sid", "no home for known codex title resolve: %v", err)
		return
	}
	path, ok := codexRolloutPathBySid(home, cwd, sid)
	if !ok {
		logWarn("ai-sid", "known codex sid %s not found under %s", sid, filepath.Join(home, ".codex", "sessions"))
		return
	}
	trackCodexUserTitle(ctx, sess, path)
}

func scanCodexJsonlForUserTitle(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	title := ""
	for sc.Scan() {
		line := sc.Bytes()
		// Codex has used three user-message shapes over time. Keep a cheap
		// substring gate so unrelated multi-megabyte tool outputs are never
		// unmarshaled merely to discover that they cannot carry a title.
		if !bytes.Contains(line, []byte(`"user_message"`)) &&
			!bytes.Contains(line, []byte(`"UserMessage"`)) &&
			!bytes.Contains(line, []byte(`"role":"user"`)) {
			continue
		}
		var rec struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		message := codexUserMessage(rec.Type, rec.Payload)
		if message == "" || isCodexInjectedUserContext(message) {
			continue
		}
		if next := normalizeCodexUserTitle(message); next != "" {
			// Last real user message wins. Current Codex writes both a
			// response_item and an item_completed record for the same input;
			// accepting both is harmless and lets either shape work alone.
			title = next
		}
	}
	return title, title != ""
}

type codexMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func codexUserMessage(recordType string, raw json.RawMessage) string {
	switch recordType {
	case "event_msg":
		var payload struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Item    struct {
				Type    string                `json:"type"`
				Content []codexMessageContent `json:"content"`
			} `json:"item"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			return ""
		}
		switch {
		case payload.Type == "user_message":
			return payload.Message
		case payload.Type == "item_completed" && payload.Item.Type == "UserMessage":
			return joinCodexMessageContent(payload.Item.Content)
		}
	case "response_item":
		var payload struct {
			Type    string                `json:"type"`
			Role    string                `json:"role"`
			Content []codexMessageContent `json:"content"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			return ""
		}
		if payload.Type == "message" && payload.Role == "user" {
			return joinCodexMessageContent(payload.Content)
		}
	}
	return ""
}

func joinCodexMessageContent(content []codexMessageContent) string {
	parts := make([]string, 0, len(content))
	for _, part := range content {
		if part.Type != "input_text" && part.Type != "text" {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func isCodexInjectedUserContext(message string) bool {
	trimmed := strings.TrimSpace(message)
	return strings.HasPrefix(trimmed, "<environment_context>") &&
		strings.HasSuffix(trimmed, "</environment_context>")
}

func normalizeCodexUserTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= codexTitleMaxRunes {
		return s
	}
	return string(r[:codexTitleMaxRunes]) + "..."
}

// codexRolloutSids returns the set of session ids parsed from rollout-*.jsonl
// filenames in dir. Subagent rollouts are intentionally ignored: Codex writes
// them under the same directory shape, but `codex resume <agent-id>` is not the
// user-facing conversation recovery path.
func codexRolloutSids(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	for sid := range codexRolloutFiles(dir) {
		out[sid] = struct{}{}
	}
	return out
}

func codexRolloutFiles(dir string) map[string]string {
	out := map[string]string{}
	for sid, info := range codexRolloutFileInfos(dir, "") {
		out[sid] = info.Path
	}
	return out
}

func codexRolloutPathBySid(home, cwd, sid string) (string, bool) {
	root := filepath.Join(home, ".codex", "sessions")
	var matches []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		got, ok := codexParseSid(d.Name())
		if !ok || got != sid {
			return nil
		}
		if codexRolloutIsResumableForCwd(path, sid, cwd) {
			matches = append(matches, path)
		}
		return nil
	})
	if len(matches) != 1 {
		if len(matches) > 1 {
			logWarn("ai-sid", "known codex sid %s ambiguous (%d rollout files)", sid, len(matches))
		}
		return "", false
	}
	return matches[0], true
}

type codexRolloutFileInfo struct {
	Path    string
	ModTime time.Time
}

func codexRolloutFileInfos(dir, cwd string) map[string]codexRolloutFileInfo {
	out := map[string]codexRolloutFileInfo{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if sid, ok := codexParseSid(e.Name()); ok {
			info, err := e.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if codexRolloutIsResumableForCwd(path, sid, cwd) {
				out[sid] = codexRolloutFileInfo{Path: path, ModTime: info.ModTime()}
			}
		}
	}
	return out
}

func codexRolloutIsResumable(path, sid string) bool {
	return codexRolloutIsResumableForCwd(path, sid, "")
}

func codexRolloutIsResumableForCwd(path, sid, cwd string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return false
	}
	var first struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string `json:"id"`
			ThreadSource string `json:"thread_source"`
			Cwd          string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(sc.Bytes(), &first); err != nil {
		return false
	}
	if first.Type != "session_meta" || first.Payload.ID != sid {
		return false
	}
	if first.Payload.ThreadSource != "" && first.Payload.ThreadSource != "user" {
		return false
	}
	if cwd != "" && first.Payload.Cwd != "" && filepath.Clean(first.Payload.Cwd) != filepath.Clean(cwd) {
		return false
	}
	return true
}

// startAIResolve dispatches AI session-id resolution by CLI kind. claude uses
// title matching; codex keeps the filename heuristic; aider is a no-op (it
// resumes by cwd, no id involved).
func startAIResolve(ctx context.Context, sess *session.Session, cwd, kind string, onCapture func(sid string)) {
	switch kind {
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			logWarn("ai-sid", "no home for claude resolve: %v", err)
			return
		}
		logDebug("ai-sid", "ai resolve start kind=claude (cwd tracked live, initial=%s)", cwd)
		startClaudeTitleResolve(ctx, sess, home, onCapture)
	case "codex":
		logDebug("ai-sid", "ai resolve start kind=codex cwd=%s", cwd)
		startCodexFileResolve(ctx, sess, cwd, onCapture)
	default:
		// aider and unknown kinds: nothing to resolve.
	}
}
