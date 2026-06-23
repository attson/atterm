package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log"
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
		log.Printf("recovery: claude resolve title=%q → sid=%s", want, matches[0].SessionID)
		return matches[0].SessionID, true
	default:
		sort.Slice(matches, func(i, j int) bool { return matches[i].ModTime.After(matches[j].ModTime) })
		if matches[0].ModTime.Equal(matches[1].ModTime) {
			log.Printf("recovery: claude resolve ambiguous title=%q (%d files, equal mtime) — skip", want, len(matches))
			return "", false
		}
		log.Printf("recovery: claude resolve title=%q → sid=%s (newest of %d)", want, matches[0].SessionID, len(matches))
		return matches[0].SessionID, true
	}
}

// claudeResolveInterval / claudeResolveBudget bound the title poll. A freshly
// launched claude has no ai-title record until it generates one, so we retry
// as the title stabilizes.
const (
	claudeResolveInterval = 1 * time.Second
	claudeResolveBudget   = 60 * time.Second
)

// startClaudeTitleResolve polls the live pane title and resolves the claude
// session id by aiTitle match. Calls onCapture once on success.
func startClaudeTitleResolve(ctx context.Context, sess *session.Session, dir string, onCapture func(sid string)) {
	cache := titleCache{}
	deadline := time.Now().Add(claudeResolveBudget)
	lastTitle := ""
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(claudeResolveInterval):
		}
		lastTitle = sess.Info().Title
		if sid, ok := resolveClaudeSessionID(dir, lastTitle, cache); ok {
			onCapture(sid)
			return
		}
	}
	log.Printf("recovery: claude resolve timeout in %s (last_title=%q)", dir, lastTitle)
}

// codexResolveInterval / codexResolveBudget bound the codex new-file watch.
const (
	codexResolveInterval = 200 * time.Millisecond
	codexResolveBudget   = 30 * time.Second
)

// startCodexFileResolve keeps the filename heuristic for codex (its jsonl has
// no ai-title record). It baselines the day-dir's rollout ids, then waits for a
// single new one to appear. Ambiguous (≥2 new) aborts.
func startCodexFileResolve(ctx context.Context, cwd string, onCapture func(sid string)) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("recovery: no home for codex resolve: %v", err)
		return
	}
	dir := codexWatchDir(cwd, time.Now(), home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("recovery: mkdir %s: %v — skip codex resolve", dir, err)
		return
	}
	before := codexRolloutSids(dir)
	deadline := time.Now().Add(codexResolveBudget)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(codexResolveInterval):
		}
		var fresh []string
		for sid := range codexRolloutSids(dir) {
			if _, seen := before[sid]; !seen {
				fresh = append(fresh, sid)
			}
		}
		switch {
		case len(fresh) == 1:
			onCapture(fresh[0])
			return
		case len(fresh) >= 2:
			log.Printf("recovery: codex resolve ambiguous (%d new in %s) — abort", len(fresh), dir)
			return
		}
	}
	log.Printf("recovery: codex resolve timeout in %s", dir)
}

// codexRolloutSids returns the set of session ids parsed from rollout-*.jsonl
// filenames in dir.
func codexRolloutSids(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if sid, ok := codexParseSid(e.Name()); ok {
			out[sid] = struct{}{}
		}
	}
	return out
}

// startAIResolve dispatches AI session-id resolution by CLI kind. claude uses
// title matching; codex keeps the filename heuristic; aider is a no-op (it
// resumes by cwd, no id involved).
func startAIResolve(ctx context.Context, sess *session.Session, cwd, kind string, onCapture func(sid string)) {
	switch kind {
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			log.Printf("recovery: no home for claude resolve: %v", err)
			return
		}
		dir := claudeWatchDir(cwd, time.Now(), home)
		log.Printf("recovery: ai resolve start kind=claude dir=%s", dir)
		startClaudeTitleResolve(ctx, sess, dir, onCapture)
	case "codex":
		log.Printf("recovery: ai resolve start kind=codex cwd=%s", cwd)
		startCodexFileResolve(ctx, cwd, onCapture)
	default:
		// aider and unknown kinds: nothing to resolve.
	}
}
