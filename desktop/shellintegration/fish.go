package shellintegration

import (
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/logging"
)

func prepareFish() Plan {
	confDir, err := fishConfDir()
	if err != nil {
		logging.Warn("shell-integration", "fish conf dir unavailable: %v", err)
		return Plan{}
	}
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		logging.Warn("shell-integration", "fish mkdir %s: %v", confDir, err)
		return Plan{}
	}
	target := filepath.Join(confDir, "atterm-integration.fish")
	if err := os.WriteFile(target, []byte(fishSnippet), 0o644); err != nil {
		logging.Warn("shell-integration", "fish write %s: %v", target, err)
		return Plan{}
	}
	return Plan{
		Shell:    "fish",
		ExtraEnv: []string{"ATTERM_SHELL_INTEGRATION=1"},
	}
}

func fishConfDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "fish", "conf.d"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fish", "conf.d"), nil
}
