package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/attson/atterm/internal/sshconfig"
	"github.com/google/uuid"
)

// SSHConfigImportPreview is the result of parsing ~/.ssh/config for import:
// the hosts that can be imported (IDs are not assigned yet — ImportSSHHosts
// decides those, since the same alias may already exist), what the parser
// deliberately skipped and why, and a user-facing note about the parser's
// coverage limits (design doc §7.3 footnote).
type SSHConfigImportPreview struct {
	Entries []SSHHost                `json:"entries"`
	Skipped []SSHConfigImportSkipped `json:"skipped"`
	Note    string                   `json:"note"`
}

// SSHConfigImportSkipped is one host ~/.ssh/config named that the parser
// deliberately did not import (a Match block, an unreadable or cyclic
// Include, …) together with the user-facing reason.
//
// This is a named type, not an anonymous struct inlined into
// SSHConfigImportPreview, because wails' TypeScript generator does not guard
// anonymous structs on its slice-of-structs branch: it emits
// "export class  {" — invalid TypeScript — into wailsjs/go/models.ts on the
// next `wails build`/`wails dev`. The name also matches the hand-written TS
// shim (frontend/src/lib/api/_bindings.ts), so the two sides stay symmetric.
type SSHConfigImportSkipped struct {
	Alias  string `json:"alias"`
	Reason string `json:"reason"`
}

// sshConfigImportNote footnotes the preview so the UI can show it under the
// list: this parser only understands a handful of ssh_config keywords, and
// silently dropping that context would make the import look complete when it
// is not.
const sshConfigImportNote = "Only imports fields atterm uses (hostname, port, username, identity file path, jump host settings); other settings in the SSH config file are not recognized or imported."

// fsOpener resolves sshconfig.Include paths against the real filesystem.
//
// sshconfig joins paths with the POSIX "path" package, not "filepath" (see
// sshconfig.Opener's doc comment), so every path it hands to Open/Glob is a
// forward-slash string regardless of OS. filepath.FromSlash converts that
// back to native separators before touching the real filesystem — on POSIX
// this is a no-op; on Windows it turns "C:/Users/x/.ssh/conf.d" into
// "C:\Users\x\.ssh\conf.d" so os.Open and filepath.Glob/Match (which treat
// "\" as a separator, not an escape character) see what they expect.
//
// It also implements sshconfig.Lister, the optional capability Parse needs
// to expand glob-style Include patterns (e.g. "Include conf.d/*"). Real
// ~/.ssh/config files commonly split into an Include'd conf.d/ directory
// (ssh clients, config managers, and tools like Termius all generate configs
// this way); without Glob those users would see Include silently resolve to
// nothing, which is exactly the kind of silent gap the parser is designed to
// avoid (see sshconfig.Skipped's doc comment). filepath.Glob is sufficient
// here since Parse always calls it with an already-rooted, absolute pattern.
type fsOpener struct{}

func (fsOpener) Open(path string) (io.ReadCloser, error) {
	return os.Open(filepath.FromSlash(path))
}

func (fsOpener) Glob(pattern string) ([]string, error) {
	return filepath.Glob(filepath.FromSlash(pattern))
}

// PreviewSSHConfigImport parses ~/.ssh/config and returns what can be
// imported without writing anything. A missing or unreadable config file is
// a readable error, not an empty list — an empty list reads as "you have no
// hosts", which is misleading when the real problem is a permissions issue
// or atterm looking in the wrong place.
func (a *App) PreviewSSHConfigImport() (SSHConfigImportPreview, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return SSHConfigImportPreview{}, fmt.Errorf("could not find home directory: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	cfgPath := filepath.Join(sshDir, "config")

	f, err := os.Open(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SSHConfigImportPreview{}, fmt.Errorf("SSH config file not found: %s", cfgPath)
		}
		return SSHConfigImportPreview{}, fmt.Errorf("could not read SSH config file %s: %w", cfgPath, err)
	}
	defer f.Close()

	// sshconfig joins base with the POSIX "path" package (see fsOpener's doc
	// comment) — hand it a forward-slash string even though sshDir itself was
	// built with filepath.Join (native separators, needed for os.Open above).
	entries, skipped, err := sshconfig.Parse(f, filepath.ToSlash(sshDir), fsOpener{})
	if err != nil {
		return SSHConfigImportPreview{}, fmt.Errorf("failed to parse SSH config file %s: %w", cfgPath, err)
	}

	// Both slices are make()d, never left nil: a nil slice marshals to JSON
	// null, and the drawer reads .entries.length / .skipped.length directly.
	// Most configs have no Match block and no Include trouble, so Skipped is
	// empty on the *ordinary* success path — exactly the path that must not
	// hand the UI a null. Same reason ListSSHHosts converts nil → [].
	preview := SSHConfigImportPreview{
		Entries: make([]SSHHost, 0, len(entries)),
		Skipped: make([]SSHConfigImportSkipped, 0, len(skipped)),
		Note:    sshConfigImportNote,
	}
	for _, e := range entries {
		preview.Entries = append(preview.Entries, hostFromEntry(e))
	}
	for _, s := range skipped {
		preview.Skipped = append(preview.Skipped, SSHConfigImportSkipped{Alias: s.Alias, Reason: s.Reason})
	}
	return preview, nil
}

// hostFromEntry maps a parsed ssh_config entry onto the app's SSHHost shape.
// It deliberately never reads IdentityFile's target: the path is recorded
// verbatim, AuthKind is set to "key" when a path is present, and KeyID is
// left empty so NewSshSessionByID's existing errKeyMissing path prompts the
// user to import the key explicitly (see design doc §5.2). A host with no
// IdentityFile defaults to "password", matching every other manually-added
// host — atterm has no way to try an ssh-agent key implicitly.
func hostFromEntry(e sshconfig.Entry) SSHHost {
	h := SSHHost{
		Alias:        e.Alias,
		Host:         e.HostName,
		Port:         e.Port,
		User:         e.User,
		IdentityFile: e.IdentityFile,
		ProxyJump:    e.ProxyJump,
		ProxyCommand: e.ProxyCommand,
		AuthKind:     "password",
	}
	if e.IdentityFile != "" {
		h.AuthKind = "key"
	}
	return h
}

// mergeImportedHost folds a freshly-parsed entry into an already-saved host
// that matched by Alias. Config-derived fields (Host, Port, User,
// IdentityFile, the proxy fields) come from the incoming entry, so re-import
// reflects the current ssh_config, exactly like a fresh import would.
//
// AuthKind and KeyID are a coupled pair, not two independent fields: KeyID
// only means anything when AuthKind=="key", and NewSshSessionByID branches
// on AuthKind to decide which credential to load. Preserving KeyID while
// letting AuthKind come from incoming would desync them — e.g. a user drops
// the IdentityFile line from ~/.ssh/config (switches to an agent, tidies up,
// whatever) and re-imports: hostFromEntry now says AuthKind="password", but
// KeyID from the old import is still sitting there unused, and the host
// that used to connect via key now fails errCredentialMissing instead.
// ~/.ssh/config's IdentityFile is a *hint* atterm uses only to seed AuthKind
// on first import; attaching a key via the UI afterwards is a deliberate,
// user-owned action of the same kind as Tags/Note, so the pair is preserved
// the same way. IdentityFile itself keeps updating from incoming — it's
// informational (recorded verbatim, never read) and doesn't drive the auth
// branch.
//
// ID, AuthKind, KeyID, Tags and Note are all things the user added or
// established inside atterm — ~/.ssh/config has no concept of any of them —
// so they survive untouched, including when incoming's Tags is nil: this is
// "config says nothing about tags", never "clear the user's tags".
func mergeImportedHost(existing, incoming SSHHost) SSHHost {
	merged := incoming
	merged.ID = existing.ID
	merged.AuthKind = existing.AuthKind
	merged.KeyID = existing.KeyID
	merged.Tags = existing.Tags
	merged.Note = existing.Note
	return merged
}

// ImportSSHHosts writes the given (user-selected, from PreviewSSHConfigImport)
// hosts into the store. It matches existing hosts by Alias: a hit merges via
// mergeImportedHost, a miss gets a new ID and is appended. This never touches
// the keychain — import carries no credentials, by design (design doc §5.2,
// §6); the caller is expected to have left AuthKind/IdentityFile as returned
// by the preview and to add a credential afterwards through the normal
// add-key / set-password flow. Returns the number of hosts written.
func (a *App) ImportSSHHosts(hosts []SSHHost) (int, error) {
	if a.cfgStore == nil {
		return 0, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()

	byAlias := make(map[string]int, len(cfg.SSHHosts))
	for i, h := range cfg.SSHHosts {
		if h.Alias != "" {
			byAlias[h.Alias] = i
		}
	}

	n := 0
	for _, incoming := range hosts {
		if idx, ok := byAlias[incoming.Alias]; ok && incoming.Alias != "" {
			cfg.SSHHosts[idx] = mergeImportedHost(cfg.SSHHosts[idx], incoming)
		} else {
			incoming.ID = uuid.New().String()
			cfg.SSHHosts = append(cfg.SSHHosts, incoming)
			if incoming.Alias != "" {
				byAlias[incoming.Alias] = len(cfg.SSHHosts) - 1
			}
		}
		n++
	}

	if err := a.cfgStore.Set(cfg); err != nil {
		return 0, err
	}
	a.markSSHHostsDirty()
	return n, nil
}
