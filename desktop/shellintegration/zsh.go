package shellintegration

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func prepareZsh(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("shellintegration: zsh cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration", "zsh-"+sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("shellintegration: zsh mkdir %s: %v", dir, err)
		return Plan{}
	}

	snippetPath := filepath.Join(dir, "atterm.zsh")
	if err := os.WriteFile(snippetPath, []byte(zshSnippet), 0o600); err != nil {
		log.Printf("shellintegration: zsh write snippet: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	wrapper := fmt.Sprintf(`# atterm zsh wrapper — sources user rc then injects OSC 133 hooks.
_atterm_orig="${ATTERM_ORIG_ZDOTDIR}"
if [[ -z "$_atterm_orig" ]]; then
  _atterm_orig="$HOME"
fi
if [[ -f "$_atterm_orig/.zshrc" ]]; then
  source "$_atterm_orig/.zshrc" || true
fi
unset _atterm_orig
source %q || true
`, snippetPath)

	wrapperPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o600); err != nil {
		log.Printf("shellintegration: zsh write wrapper: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	origZDOTDIR := os.Getenv("ZDOTDIR")

	return Plan{
		Shell: "zsh",
		ExtraEnv: []string{
			"ZDOTDIR=" + dir,
			"ATTERM_ORIG_ZDOTDIR=" + origZDOTDIR,
			"ATTERM_SHELL_INTEGRATION=1",
		},
		Cleanup: func() {
			if err := os.RemoveAll(dir); err != nil {
				log.Printf("shellintegration: zsh cleanup %s: %v", dir, err)
			}
		},
	}
}
