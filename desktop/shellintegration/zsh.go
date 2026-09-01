package shellintegration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/logging"
)

func prepareZsh(sessionID string) Plan {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		logging.Warn("shell-integration", "zsh cache dir unavailable: %v", err)
		return Plan{}
	}
	dir := filepath.Join(cacheDir, "atterm", "shell-integration", "zsh-"+sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logging.Warn("shell-integration", "zsh mkdir %s: %v", dir, err)
		return Plan{}
	}

	snippetPath := filepath.Join(dir, "atterm.zsh")
	if err := os.WriteFile(snippetPath, []byte(zshSnippet), 0o600); err != nil {
		logging.Warn("shell-integration", "zsh write snippet: %v", err)
		_ = os.RemoveAll(dir)
		return Plan{}
	}

	// zsh resolves EVERY startup file against $ZDOTDIR, not just .zshrc:
	// .zshenv → .zprofile → .zshrc → .zlogin. Pointing ZDOTDIR at the shim
	// dir therefore silently skips the user's ~/.zshenv and ~/.zprofile —
	// on macOS ~/.zprofile is where `brew shellenv` usually lives, so
	// PATH-dependent tools (atuin, mise, …) turn into "command not found"
	// inside atterm while working fine in Terminal.app. Chain-load both
	// with the same restore-source-reshim dance the .zshrc wrapper does.
	// .zlogin needs no shim: the .zshrc wrapper leaves ZDOTDIR restored to
	// the original, so zsh finds the user's own .zlogin afterwards.
	for _, name := range []string{".zshenv", ".zprofile"} {
		chain := fmt.Sprintf(`# atterm zsh shim — chain-load the user's %[1]s, then re-shim ZDOTDIR
# so the remaining startup files still route through this dir.
_atterm_shim_dir=%[2]q
if [[ -n "${ATTERM_ORIG_ZDOTDIR}" ]]; then
  ZDOTDIR="${ATTERM_ORIG_ZDOTDIR}"
else
  unset ZDOTDIR
fi
_atterm_user_file="${ZDOTDIR:-$HOME}/%[1]s"
if [[ -f "$_atterm_user_file" ]]; then
  source "$_atterm_user_file" || true
fi
# The user file may itself change ZDOTDIR; capture what it left behind so
# the later shim files chain to the right place.
export ATTERM_ORIG_ZDOTDIR="${ZDOTDIR:-}"
ZDOTDIR="$_atterm_shim_dir"
unset _atterm_user_file _atterm_shim_dir
`, name, dir)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(chain), 0o600); err != nil {
			logging.Warn("shell-integration", "zsh write %s: %v", name, err)
			_ = os.RemoveAll(dir)
			return Plan{}
		}
	}

	wrapper := fmt.Sprintf(`# atterm zsh wrapper — sources user rc then injects OSC 133 hooks.
_atterm_shim_dir=%q
_atterm_orig="${ATTERM_ORIG_ZDOTDIR}"

# Restore ZDOTDIR before sourcing the user's rc so any `+"`${ZDOTDIR:-$HOME}`"+`
# references (and the user's own $ZDOTDIR reads) land on the original value
# rather than our per-session shim dir.
if [[ -n "$_atterm_orig" ]]; then
  ZDOTDIR="$_atterm_orig"
else
  unset ZDOTDIR
fi

# macOS /etc/zshrc runs BEFORE this wrapper and anchors
#   HISTFILE=${ZDOTDIR:-$HOME}/.zsh_history
# to our shim ZDOTDIR. That points HISTFILE at a per-session temp file no
# other window can see and that Cleanup deletes on exit — history appears
# isolated per window and is then lost. Repair HISTFILE when it still lives
# under our shim dir; if the user set it to something else, leave it alone.
if [[ "${HISTFILE:-}" == "$_atterm_shim_dir"/* ]]; then
  HISTFILE="${ZDOTDIR:-$HOME}/.zsh_history"
fi

_atterm_user_rc="${ZDOTDIR:-$HOME}/.zshrc"
if [[ -f "$_atterm_user_rc" ]]; then
  source "$_atterm_user_rc" || true
fi
unset _atterm_user_rc _atterm_orig _atterm_shim_dir
source %q || true
`, dir, snippetPath)

	wrapperPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o600); err != nil {
		logging.Warn("shell-integration", "zsh write wrapper: %v", err)
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
				logging.Warn("shell-integration", "zsh cleanup %s: %v", dir, err)
			}
		},
	}
}
