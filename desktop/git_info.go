package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GitInfo is one sidebar row's git summary for a session cwd: the checked-out
// branch (short sha when detached) and the uncommitted line counts vs HEAD.
type GitInfo struct {
	Cwd     string `json:"cwd"`
	Branch  string `json:"branch"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

// GetGitInfo resolves git status for each cwd. Non-repo cwds (and any git
// failure) are silently omitted — the sidebar simply shows no git line.
// Deduplicates and processes at most 64 cwds per call; the frontend polls
// this every few seconds, so per-cwd work is bounded by short timeouts.
func (a *App) GetGitInfo(cwds []string) []GitInfo {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil
	}
	seen := make(map[string]bool, len(cwds))
	out := []GitInfo{}
	processed := 0
	for _, cwd := range cwds {
		if cwd == "" || seen[cwd] {
			continue
		}
		seen[cwd] = true
		if processed >= 64 {
			break
		}
		processed++
		if info, ok := gitInfoForCwd(gitPath, cwd); ok {
			out = append(out, info)
		}
	}
	return out
}

func runGit(gitPath, cwd string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitPath, append([]string{"-C", cwd}, args...)...)
	b, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func gitInfoForCwd(gitPath, cwd string) (GitInfo, bool) {
	branch, ok := runGit(gitPath, cwd, "symbolic-ref", "--short", "-q", "HEAD")
	if !ok || branch == "" {
		// Detached HEAD names no branch; fall back to the short sha. A cwd
		// outside any repo fails here too — that is the "omit" case.
		sha, shaOK := runGit(gitPath, cwd, "rev-parse", "--short", "HEAD")
		if !shaOK || sha == "" {
			return GitInfo{}, false
		}
		branch = sha
	}
	info := GitInfo{Cwd: cwd, Branch: branch}
	// Staged + unstaged line counts vs HEAD. Binary files show "-" columns
	// and are skipped; untracked files are not counted (matching the usual
	// `git diff` scope).
	if numstat, ok := runGit(gitPath, cwd, "diff", "--numstat", "HEAD"); ok {
		for _, line := range strings.Split(numstat, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			if add, err := strconv.Atoi(fields[0]); err == nil {
				info.Added += add
			}
			if del, err := strconv.Atoi(fields[1]); err == nil {
				info.Deleted += del
			}
		}
	}
	return info, true
}
