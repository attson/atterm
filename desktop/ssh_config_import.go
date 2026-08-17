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
	Entries []SSHHost `json:"entries"`
	Skipped []struct {
		Alias  string `json:"alias"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Note string `json:"note"`
}

// sshConfigImportNote footnotes the preview so the UI can show it under the
// list: this parser only understands a handful of ssh_config keywords, and
// silently dropping that context would make the import look complete when it
// is not.
const sshConfigImportNote = "仅导入 atterm 用得到的字段（主机名、端口、用户名、身份文件路径、跳板配置）；SSH 配置文件中的其它设置不会被识别或带入。"

// fsOpener resolves sshconfig.Include paths against the real filesystem.
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
	return os.Open(path)
}

func (fsOpener) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// PreviewSSHConfigImport parses ~/.ssh/config and returns what can be
// imported without writing anything. A missing or unreadable config file is
// a readable error, not an empty list — an empty list reads as "you have no
// hosts", which is misleading when the real problem is a permissions issue
// or atterm looking in the wrong place.
func (a *App) PreviewSSHConfigImport() (SSHConfigImportPreview, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return SSHConfigImportPreview{}, fmt.Errorf("找不到用户主目录：%w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	cfgPath := filepath.Join(sshDir, "config")

	f, err := os.Open(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SSHConfigImportPreview{}, fmt.Errorf("未找到 SSH 配置文件：%s", cfgPath)
		}
		return SSHConfigImportPreview{}, fmt.Errorf("无法读取 SSH 配置文件 %s：%w", cfgPath, err)
	}
	defer f.Close()

	entries, skipped, err := sshconfig.Parse(f, sshDir, fsOpener{})
	if err != nil {
		return SSHConfigImportPreview{}, fmt.Errorf("解析 SSH 配置文件 %s 失败：%w", cfgPath, err)
	}

	preview := SSHConfigImportPreview{
		Entries: make([]SSHHost, 0, len(entries)),
		Note:    sshConfigImportNote,
	}
	for _, e := range entries {
		preview.Entries = append(preview.Entries, hostFromEntry(e))
	}
	for _, s := range skipped {
		preview.Skipped = append(preview.Skipped, struct {
			Alias  string `json:"alias"`
			Reason string `json:"reason"`
		}{Alias: s.Alias, Reason: s.Reason})
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
// AuthKind/IdentityFile, the proxy fields) come from the incoming entry, so
// re-import reflects the current ssh_config, exactly like a fresh import
// would. ID, KeyID, Tags and Note are things the user added inside atterm —
// ~/.ssh/config has no concept of any of them — so they survive untouched,
// including when incoming's Tags is nil: this is "config says nothing about
// tags", never "clear the user's tags".
func mergeImportedHost(existing, incoming SSHHost) SSHHost {
	merged := incoming
	merged.ID = existing.ID
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
