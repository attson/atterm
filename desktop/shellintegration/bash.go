package shellintegration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/logging"
)

func prepareBash(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		logging.Warn("shell-integration", "bash cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration", "bash-"+sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logging.Warn("shell-integration", "bash mkdir %s: %v", dir, err)
		return Plan{}
	}

	snippetPath := filepath.Join(dir, "atterm.bash")
	if err := os.WriteFile(snippetPath, []byte(bashSnippet), 0o600); err != nil {
		logging.Warn("shell-integration", "bash write snippet: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	rcfile := fmt.Sprintf(`# atterm bash rcfile — sources user rc then injects OSC 133 hooks.
[[ -f ~/.bashrc ]] && source ~/.bashrc
source %q || true
`, snippetPath)

	rcPath := filepath.Join(dir, "atterm.rc")
	if err := os.WriteFile(rcPath, []byte(rcfile), 0o600); err != nil {
		logging.Warn("shell-integration", "bash write rcfile: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	return Plan{
		Shell:     "bash",
		ExtraArgs: []string{"--rcfile", rcPath, "-i"},
		ExtraEnv:  []string{"ATTERM_SHELL_INTEGRATION=1"},
		Cleanup: func() {
			if err := os.RemoveAll(dir); err != nil {
				logging.Warn("shell-integration", "bash cleanup %s: %v", dir, err)
			}
		},
	}
}
