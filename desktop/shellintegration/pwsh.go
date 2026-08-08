package shellintegration

import (
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/logging"
)

func preparePwsh(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		logging.Warn("shell-integration", "pwsh cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logging.Warn("shell-integration", "pwsh mkdir %s: %v", dir, err)
		return Plan{}
	}
	scriptPath := filepath.Join(dir, "atterm-"+sessionID+".ps1")
	if err := os.WriteFile(scriptPath, []byte(pwshSnippet), 0o600); err != nil {
		logging.Warn("shell-integration", "pwsh write %s: %v", scriptPath, err)
		return Plan{}
	}
	return Plan{
		Shell: "pwsh",
		// -NoExit -Command "& '<path>'" - : the trailing '-' forces pwsh to drop to
		// the interactive prompt after the script; without it, pwsh exits even with -NoExit.
		ExtraArgs: []string{
			"-NoExit",
			"-Command",
			"& '" + scriptPath + "'",
			"-",
		},
		ExtraEnv: []string{"ATTERM_SHELL_INTEGRATION=1"},
		Cleanup: func() {
			if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
				logging.Warn("shell-integration", "pwsh cleanup %s: %v", scriptPath, err)
			}
		},
	}
}
