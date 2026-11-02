package hookinstall

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var codexTrustEventNames = map[string]string{
	"SessionStart":      "session_start",
	"UserPromptSubmit":  "user_prompt_submit",
	"PreToolUse":        "pre_tool_use",
	"PostToolUse":       "post_tool_use",
	"Stop":              "stop",
	"PermissionRequest": "permission_request",
}

func codexConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

func checkCodexHealth(home, link string) (settingsOK, trustOK bool, settingsErr, trustErr string) {
	raw, err := os.ReadFile(codexHooksPath(home))
	if err != nil {
		return false, false, fmt.Sprintf("Codex hooks.json unavailable: %v", err), ""
	}
	doc, _ := readCodexHooksDocument(raw)
	if doc.Hooks == nil {
		return false, false, "Codex hooks.json invalid", ""
	}

	wantCommand := attermHookCommand(link, "codex")
	expectedTrust := make([]string, 0, len(codexHookEvents))
	for _, event := range codexHookEvents {
		entries := doc.Hooks[event]
		found := false
		for entryIdx, entry := range entries {
			if !isAttermCodexEntry(entry) {
				continue
			}
			for hookIdx, hook := range entry.Hooks {
				if hook.Command != wantCommand {
					return false, false, event + " Codex hook points at stale binary path", ""
				}
				if _, err := os.Stat(hookCommandBinary(hook.Command)); err != nil {
					return false, false, event + " Codex hook binary missing", ""
				}
				trustEvent, ok := codexTrustEventNames[event]
				if !ok {
					return false, false, "unknown Codex hook event " + event, ""
				}
				expectedTrust = append(expectedTrust, fmt.Sprintf("%s:%s:%d:%d",
					codexHooksPath(home), trustEvent, entryIdx, hookIdx))
				found = true
			}
		}
		if !found {
			return false, false, event + " Codex hook entry missing", ""
		}
	}

	trusted, err := readCodexTrustedHooks(codexConfigPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return true, false, "", "Codex hook trust is pending; start Codex and approve the atterm hook"
		}
		return true, false, "", fmt.Sprintf("Codex hook trust unavailable: %v", err)
	}
	for _, key := range expectedTrust {
		if !trusted[key] {
			return true, false, "", "Codex hook trust is pending; start Codex and approve the atterm hook"
		}
	}
	return true, true, "", ""
}

func readCodexTrustedHooks(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	trusted := make(map[string]bool)
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, `[hooks.state."`) && strings.HasSuffix(line, `"]`) {
			quoted := strings.TrimSuffix(strings.TrimPrefix(line, "[hooks.state."), "]")
			key, err := strconv.Unquote(quoted)
			if err != nil {
				current = ""
				continue
			}
			current = key
			continue
		}
		if current == "" || !strings.HasPrefix(line, "trusted_hash") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if ok && strings.Trim(strings.TrimSpace(value), `"`) != "" {
			trusted[current] = true
		}
	}
	return trusted, sc.Err()
}
