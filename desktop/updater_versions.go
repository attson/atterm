package main

import (
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// parseVersionTag splits a "vMAJOR.MINOR.PATCH" tag into its minor line
// ("vMAJOR.MINOR") and patch number. ok is false for any tag that is not a
// well-formed three-part v-prefixed version (dev, drafts, malformed).
func parseVersionTag(tag string) (minor string, patch int, ok bool) {
	if !strings.HasPrefix(tag, "v") {
		return "", 0, false
	}
	parts := strings.Split(tag[1:], ".")
	if len(parts) != 3 {
		return "", 0, false
	}
	p, err := strconv.Atoi(parts[2])
	if err != nil {
		return "", 0, false
	}
	if _, err := strconv.Atoi(parts[0]); err != nil {
		return "", 0, false
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return "", 0, false
	}
	return "v" + parts[0] + "." + parts[1], p, true
}

// VersionLine is one update line (minor version) the user can choose, with
// the latest release on that line. JSON tags mirror the frontend binding.
type VersionLine struct {
	Minor    string `json:"minor"`  // "v0.2"
	Latest   string `json:"latest"` // "v0.2.155"
	Notes    string `json:"notes"`
	AssetURL string `json:"asset_url"`
}

// lineCandidate is an intermediate fetched-release tuple fed to groupLines.
type lineCandidate struct {
	tag      string
	assetURL string
	notes    string
}

// groupLines applies the "upgrade-only" rule:
//   - group candidates by minor line, keep the highest patch per line
//   - keep a line iff its minor > current's minor, OR (same minor AND its
//     latest patch > current's patch)
//   - when current is dev/unparseable, every line's latest is kept
//
// Result is sorted by minor descending (highest line first).
func groupLines(cands []lineCandidate, current string) []VersionLine {
	type best struct {
		patch    int
		tag      string
		assetURL string
		notes    string
	}
	byMinor := map[string]best{}
	for _, c := range cands {
		minor, patch, ok := parseVersionTag(c.tag)
		if !ok {
			continue
		}
		if b, exists := byMinor[minor]; !exists || patch > b.patch {
			byMinor[minor] = best{patch: patch, tag: c.tag, assetURL: c.assetURL, notes: c.notes}
		}
	}

	curMinor, curPatch, curOK := parseVersionTag(current)

	var out []VersionLine
	for minor, b := range byMinor {
		keep := false
		if !curOK {
			keep = true // dev / unparseable current: show everything
		} else if semver.Compare(minor, curMinor) > 0 {
			keep = true // higher line
		} else if minor == curMinor && b.patch > curPatch {
			keep = true // same line, newer patch
		}
		if keep {
			out = append(out, VersionLine{
				Minor: minor, Latest: b.tag, Notes: b.notes, AssetURL: b.assetURL,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return semver.Compare(out[i].Minor, out[j].Minor) > 0
	})
	return out
}
