package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Keep this in sync with frontend/src/lib/sshKeyFile.ts. Private keys are
// normally only a few KiB; the cap prevents a mistaken drop from pulling an
// arbitrary large file into the WebView and then into Vue's reactive state.
const maxSSHKeyFileBytes = 256 * 1024

var sshPrivateKeyHeader = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)

// SSHKeyFileContent is the narrow result returned for one user-dropped key
// file. It never enters the SSH host store or an uplink until the user
// explicitly saves the key in the drawer.
type SSHKeyFileContent struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ReadSSHKeyFile reads a single path delivered by Wails' native file-drop
// callback. A dedicated binding is intentional: PluginFS rejects ~/.ssh and
// broadening that general-purpose browser would expose credentials to plugins.
func (a *App) ReadSSHKeyFile(path string) (SSHKeyFileContent, error) {
	if !filepath.IsAbs(path) {
		return SSHKeyFileContent{}, fmt.Errorf("SSH key path must be absolute")
	}

	f, err := os.Open(path)
	if err != nil {
		return SSHKeyFileContent{}, fmt.Errorf("could not open SSH key file")
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return SSHKeyFileContent{}, fmt.Errorf("could not inspect SSH key file")
	}
	if !info.Mode().IsRegular() {
		return SSHKeyFileContent{}, fmt.Errorf("SSH key path is not a regular file")
	}
	if info.Size() > maxSSHKeyFileBytes {
		return SSHKeyFileContent{}, fmt.Errorf("SSH key file exceeds %d bytes", maxSSHKeyFileBytes)
	}

	content, err := io.ReadAll(io.LimitReader(f, maxSSHKeyFileBytes+1))
	if err != nil {
		return SSHKeyFileContent{}, fmt.Errorf("could not read SSH key file")
	}
	if len(content) > maxSSHKeyFileBytes {
		return SSHKeyFileContent{}, fmt.Errorf("SSH key file exceeds %d bytes", maxSSHKeyFileBytes)
	}
	if !sshPrivateKeyHeader.Match(content) {
		return SSHKeyFileContent{}, fmt.Errorf("selected file is not a private key")
	}
	return SSHKeyFileContent{Name: filepath.Base(path), Content: string(content)}, nil
}
