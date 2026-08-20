// Package sshconfig parses OpenSSH client config files (ssh_config(5)) into a
// flat list of importable hosts. It is a pure Go package with no GUI or
// desktop dependency so it can be unit tested and reused by both the desktop
// backend and, eventually, a CLI.
//
// The two rules that make this parser non-trivial, and that everything else
// here is built around:
//
//  1. For any given keyword, the FIRST value obtained for a matching host
//     wins — not the last. A "Host *" block at the top of the file beats a
//     specific block further down. Getting this backwards silently produces
//     hosts that connect as the wrong user, on the wrong port, etc.
//  2. Wildcard blocks ("Host *", "Host 10.0.*") participate in evaluation —
//     their settings apply to every concrete host they match — but they do
//     not themselves produce an importable Entry, because there is no
//     concrete hostname to connect to. Skipping their participation entirely
//     would make the import disagree with what `ssh <alias>` actually does.
package sshconfig

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Entry is one importable host parsed out of an ssh_config file.
type Entry struct {
	Alias        string // the concrete (non-wildcard) Host pattern
	HostName     string // defaults to Alias when unset
	User         string
	Port         string
	IdentityFile string // path kept as-is; the file itself is never read
	ProxyJump    string
	ProxyCommand string
}

// Skipped records something the parser deliberately did not import — a Match
// block, an Include that could not be expanded, or a cycle/depth-cap hit —
// along with a user-facing reason to show in the import preview. Skipped is
// how the parser stays honest: a user with 20 hosts who sees 12 imported
// needs to know where the other 8 went, not just get fewer than expected.
type Skipped struct {
	Alias  string
	Reason string
}

// Opener resolves an Include path to its contents. Production passes a
// filesystem-backed implementation rooted at (and ideally restricted to)
// ~/.ssh; tests use an in-memory map.
//
// Paths passed to Open (and the base given to Parse) are POSIX-style,
// forward-slash strings — this package joins them with the "path" package,
// not "filepath", so it stays platform-independent and testable with plain
// string keys. A Windows-facing production Opener is responsible for
// translating to native path semantics itself (see internal/appdir, which
// uses filepath.Join for exactly this reason).
type Opener interface {
	Open(path string) (io.ReadCloser, error)
}

// Lister is an optional capability an Opener may additionally implement to
// support glob-style Include patterns (e.g. "Include conf.d/*") that need
// directory listing, which the base Opener interface cannot provide. Matches
// must be returned in any order; the parser sorts them itself so expansion is
// deterministic. Openers that only implement Open can still resolve Include
// patterns that name a literal file.
type Lister interface {
	Glob(pattern string) ([]string, error)
}

// maxIncludeDepth caps Include nesting so a config that includes itself (or a
// very long include chain) produces a Skipped entry instead of recursing
// without bound.
const maxIncludeDepth = 16

// Recognized keywords. Anything else encountered in the file is parsed but
// ignored — this parser only understands the fields atterm's host list uses;
// see the design doc's note that this is a deliberate, documented limitation,
// not a silent one.
const (
	kwHost         = "host"
	kwMatch        = "match"
	kwInclude      = "include"
	kwHostName     = "hostname"
	kwUser         = "user"
	kwPort         = "port"
	kwIdentityFile = "identityfile"
	kwProxyJump    = "proxyjump"
	kwProxyCommand = "proxycommand"
)

// Parse reads an ssh_config file from r and returns the hosts it can
// statically resolve, plus a list of things it deliberately skipped. base is
// the FIXED directory every relative Include path resolves against —
// production passes ~/.ssh — matching ssh_config(5): "Files without absolute
// paths are assumed to be in ~/.ssh". This does not shift as Includes nest:
// a file reached via one Include that itself contains a relative Include
// still resolves against the original base, not against its own directory.
// base is a POSIX-style (forward-slash) string; see the Opener doc comment.
// opener resolves Include targets; Parse never touches a real filesystem
// directly so it stays fully testable.
func Parse(r io.Reader, base string, opener Opener) ([]Entry, []Skipped, error) {
	var skipped []Skipped
	visited := map[string]bool{}
	rawLines, err := flatten(r, base, opener, 0, visited, &skipped)
	if err != nil {
		return nil, skipped, err
	}

	blocks := groupBlocks(rawLines)

	// aliasOrder holds every concrete (non-wildcard, non-negated) Host
	// pattern in first-seen order; Match blocks are recorded as Skipped here
	// too since this is the single pass over every non-synthetic block.
	var aliasOrder []string
	seenAlias := map[string]bool{}
	for _, b := range blocks {
		if b.synthetic {
			continue
		}
		if b.isMatch {
			// Match conditions (host/user/exec/...) depend on runtime state
			// we don't have, and Match exec would require running an
			// arbitrary command. There is no safe static answer, so this is
			// reported, not silently dropped.
			skipped = append(skipped, Skipped{
				Alias:  b.rawMatch,
				Reason: "Match block depends on runtime conditions, not imported",
			})
			continue
		}
		if !hasPositivePattern(b.patterns) {
			// "Host" with no patterns at all, or "Host !a !b" where every
			// pattern is negated, can never match anything (matchPatterns
			// requires at least one non-negated match) — it's dead weight
			// that silently contributes nothing. Say so instead of letting
			// it disappear; this is distinct from a normal "Host *" block,
			// which has exactly one non-negated (wildcard) pattern and is
			// working as intended.
			skipped = append(skipped, Skipped{
				Alias:  patternsText(b.patterns),
				Reason: "Host line has no usable pattern (empty, or all negated), not imported",
			})
			continue
		}
		for _, p := range b.patterns {
			if p.negate || p.text == "" || hostPatternHasWildcard(p.text) {
				continue // no concrete hostname to import
			}
			if !seenAlias[p.text] {
				seenAlias[p.text] = true
				aliasOrder = append(aliasOrder, p.text)
			}
		}
	}

	entries := make([]Entry, 0, len(aliasOrder))
	for _, alias := range aliasOrder {
		entries = append(entries, resolveEntry(alias, blocks))
	}
	return entries, skipped, nil
}

// resolveEntry applies first-wins evaluation for one concrete alias across
// every block (in file order, including Include expansions spliced inline)
// whose patterns match it. A block matches an alias regardless of whether it
// is a wildcard block — that's the whole point of rule 2 above.
func resolveEntry(alias string, blocks []*block) Entry {
	e := Entry{Alias: alias}
	// Pointers into e so the field-lookup below can be generic instead of a
	// six-way switch, while still writing directly into the result.
	fields := map[string]*string{
		kwHostName:     &e.HostName,
		kwUser:         &e.User,
		kwPort:         &e.Port,
		kwIdentityFile: &e.IdentityFile,
		kwProxyJump:    &e.ProxyJump,
		kwProxyCommand: &e.ProxyCommand,
	}
	for _, b := range blocks {
		if b.isMatch || !matchPatterns(b.patterns, alias) {
			continue
		}
		for _, d := range b.lines {
			ptr, ok := fields[d.key]
			if !ok || *ptr != "" {
				// Unknown keyword, or first-wins: already set. Note this
				// treats "" as "not yet set", so a directive that explicitly
				// assigns an empty value (e.g. a hypothetical `User ""`)
				// does not itself "stick" against a later block's
				// non-empty value for the same keyword — a narrow deviation
				// from strict first-obtained-value semantics that only
				// matters for a config nobody writes in practice.
				continue
			}
			*ptr = d.value
		}
	}
	if e.HostName == "" {
		e.HostName = alias
	}
	return e
}

// --- block model ---

// pattern is one space-separated token from a Host line, e.g. "web1", "*",
// or "!excluded".
type pattern struct {
	negate bool
	text   string
}

// block is one Host/Match stanza (or the implicit stanza formed by any
// directives that appear before the first Host/Match line) together with the
// directives collected under it, in file order.
type block struct {
	patterns  []pattern // Host patterns; unused for Match blocks
	isMatch   bool
	rawMatch  string // raw text after "Match", used only for the Skipped reason
	synthetic bool   // true for the implicit pre-Host block; never importable
	lines     []directive
}

type directive struct {
	key   string // already lowercased
	value string
}

// matchPatterns implements ssh_config's Host pattern-list algorithm: a block
// applies if at least one non-negated pattern matches and no negated pattern
// matches. A negated match wins outright, even if an earlier pattern in the
// same line also matched.
func matchPatterns(patterns []pattern, alias string) bool {
	matched := false
	for _, p := range patterns {
		if !globMatch(p.text, alias) {
			continue
		}
		if p.negate {
			return false
		}
		matched = true
	}
	return matched
}

// globMatch implements ssh_config's Host/Match pattern syntax directly,
// rather than delegating to path.Match. ssh_config(5) PATTERNS is explicit
// that a pattern is "zero or more non-whitespace characters, '*' ... or '?'"
// — nothing else is special. path.Match additionally treats '[' as opening a
// character class and returns ErrBadPattern on a malformed one, so a literal
// '[' in a host alias could silently mismatch (or error) under path.Match
// even though real ssh would match it as a plain character. Since the whole
// point of this parser is to agree with what `ssh <alias>` actually does,
// that deviation is not acceptable.
//
// This is the standard greedy '*'/'?' matcher (as used by fnmatch-style
// globbing restricted to these two wildcards): '?' consumes exactly one rune,
// '*' consumes zero or more, backtracking to the most recent '*' on a
// mismatch.
func globMatch(pattern, alias string) bool {
	p := []rune(pattern)
	a := []rune(alias)
	var pi, ai int
	starIdx, matchIdx := -1, 0
	for ai < len(a) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == a[ai]):
			pi++
			ai++
		case pi < len(p) && p[pi] == '*':
			starIdx = pi
			matchIdx = ai
			pi++
		case starIdx != -1:
			pi = starIdx + 1
			matchIdx++
			ai = matchIdx
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// hostPatternHasWildcard reports whether s contains an ssh_config Host/Match
// pattern metacharacter ('*' or '?' — see globMatch). Unlike the glob(3)
// syntax used for Include path expansion (includeGlobHasWildcard), '[' is not
// special in a Host pattern.
func hostPatternHasWildcard(s string) bool {
	return strings.ContainsAny(s, "*?")
}

// includeGlobHasWildcard reports whether s should be treated as a filesystem
// glob for Include path expansion, which — unlike Host/Match patterns — does
// support '[' character classes (glob(3) semantics).
func includeGlobHasWildcard(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// groupBlocks turns the flattened, Include-expanded line stream into blocks.
// Directives that appear before the first Host/Match line form a synthetic
// "applies to everything" block, matching real ssh_config behavior where
// top-of-file settings act as implicit global defaults.
func groupBlocks(lines []rawLine) []*block {
	synthetic := &block{patterns: []pattern{{text: "*"}}, synthetic: true}
	blocks := []*block{synthetic}
	cur := synthetic
	for _, l := range lines {
		switch l.key {
		case kwHost:
			cur = &block{patterns: parsePatterns(l.rest)}
			blocks = append(blocks, cur)
		case kwMatch:
			cur = &block{isMatch: true, rawMatch: l.rest}
			blocks = append(blocks, cur)
		default:
			value := strings.Join(splitFields(l.rest), " ")
			cur.lines = append(cur.lines, directive{key: l.key, value: value})
		}
	}
	return blocks
}

// hasPositivePattern reports whether at least one pattern in the list is
// non-negated. A block with none (empty pattern list, or every pattern
// negated) can never satisfy matchPatterns for any alias.
func hasPositivePattern(patterns []pattern) bool {
	for _, p := range patterns {
		if !p.negate {
			return true
		}
	}
	return false
}

// patternsText reconstructs a display string for a pattern list (re-adding
// the "!" for negated entries), for use as a Skipped.Alias when the block
// itself has no host to name.
func patternsText(patterns []pattern) string {
	if len(patterns) == 0 {
		return "(empty Host line)"
	}
	parts := make([]string, len(patterns))
	for i, p := range patterns {
		if p.negate {
			parts[i] = "!" + p.text
		} else {
			parts[i] = p.text
		}
	}
	return strings.Join(parts, " ")
}

func parsePatterns(rest string) []pattern {
	fields := splitFields(rest)
	pats := make([]pattern, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "!") {
			pats = append(pats, pattern{negate: true, text: f[1:]})
		} else {
			pats = append(pats, pattern{text: f})
		}
	}
	return pats
}

// --- line-level parsing ---

// rawLine is one directive line after comment-stripping and key/value
// splitting, with Include lines already resolved away by flatten.
type rawLine struct {
	key  string // lowercased
	rest string // raw remainder (may itself contain multiple fields)
}

// flatten reads r and returns its directive lines in file order, splicing in
// the contents of any Include directive at the point it occurs (so first-wins
// evaluation sees included blocks in the right position). depth and visited
// together bound recursion: depth enforces maxIncludeDepth, visited (paths
// currently on the inclusion stack) catches cycles. Both failure modes
// produce a Skipped, not an error and not a hang.
func flatten(r io.Reader, base string, opener Opener, depth int, visited map[string]bool, skipped *[]Skipped) ([]rawLine, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var out []rawLine
	for scanner.Scan() {
		key, rest, ok := splitKeyValue(scanner.Text())
		if !ok {
			continue
		}
		if key != kwInclude {
			out = append(out, rawLine{key: key, rest: rest})
			continue
		}

		if depth >= maxIncludeDepth {
			*skipped = append(*skipped, Skipped{
				Alias:  rest,
				Reason: fmt.Sprintf("Include nesting exceeds the limit (%d levels), skipped", maxIncludeDepth),
			})
			continue
		}

		for _, pat := range splitFields(rest) {
			paths, err := resolveIncludePaths(pat, base, opener)
			if err != nil {
				*skipped = append(*skipped, Skipped{
					Alias:  pat,
					Reason: fmt.Sprintf("could not expand Include path %s: %v", pat, err),
				})
				continue
			}
			for _, p := range paths {
				if visited[p] {
					*skipped = append(*skipped, Skipped{
						Alias:  p,
						Reason: "Include cycle detected, skipped",
					})
					continue
				}
				rc, err := opener.Open(p)
				if err != nil {
					*skipped = append(*skipped, Skipped{
						Alias:  p,
						Reason: fmt.Sprintf("could not read Include file %s: %v", p, err),
					})
					continue
				}
				visited[p] = true
				// base is passed through unchanged, not path.Dir(p): ssh_config
				// resolves relative Include paths against a FIXED root
				// (~/.ssh), not against whichever file did the including. A
				// second-level relative Include inside an already-included
				// file must still resolve against the original base.
				sub, err := flatten(rc, base, opener, depth+1, visited, skipped)
				rc.Close()
				delete(visited, p) // only cycles (ancestors on the current stack) are rejected
				if err != nil {
					return nil, err
				}
				out = append(out, sub...)
			}
		}
	}
	return out, scanner.Err()
}

// resolveIncludePaths turns one Include argument into concrete, sorted paths.
// Absolute patterns are used as-is; relative ones resolve against base. A
// pattern with no glob metacharacters is returned unchanged (existence is
// checked later, by Open) so the common case needs no listing capability at
// all. A pattern that does contain glob metacharacters requires the Opener to
// additionally implement Lister; without it there is no way to enumerate
// matches through the Opener interface, so that is reported as an error
// rather than silently expanding to nothing.
func resolveIncludePaths(pat, base string, opener Opener) ([]string, error) {
	resolved := pat
	if !path.IsAbs(resolved) {
		resolved = path.Join(base, resolved)
	}
	if !includeGlobHasWildcard(resolved) {
		return []string{resolved}, nil
	}
	lister, ok := opener.(Lister)
	if !ok {
		return nil, fmt.Errorf("opener does not support directory listing, cannot expand wildcard")
	}
	matches, err := lister.Glob(resolved)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// splitKeyValue strips an inline comment, then splits a directive line into
// its lowercased keyword and raw remainder. Both "Key Value" and "Key=Value"
// (with optional surrounding whitespace around '=') are accepted, matching
// real ssh_config syntax.
func splitKeyValue(line string) (key, rest string, ok bool) {
	line = strings.TrimSpace(stripComment(line))
	if line == "" {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), "", true
	}
	key = strings.ToLower(line[:i])
	rest = strings.TrimSpace(line[i:])
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimSpace(rest)
	return key, rest, true
}

// stripComment truncates s at the first unquoted '#' that begins a comment
// (at the start of the line or preceded by whitespace), so a '#' inside a
// quoted value is not mistaken for one.
func stripComment(s string) string {
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return s[:i]
			}
		}
	}
	return s
}

// splitFields splits s on whitespace, honoring double-quoted segments (which
// may contain spaces and are unquoted in the result). It is used both for
// multi-pattern Host/Include lines and, joined back with single spaces, to
// unquote and normalize scalar values.
func splitFields(s string) []string {
	var fields []string
	var cur strings.Builder
	inQuote := false
	has := false
	flush := func() {
		if has {
			fields = append(fields, cur.String())
			cur.Reset()
			has = false
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			has = true // a quoted empty token still counts as a field
		case !inQuote && (r == ' ' || r == '\t'):
			flush()
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	return fields
}
