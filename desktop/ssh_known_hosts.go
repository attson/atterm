package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KnownHostEntry is one line of ~/.ssh/known_hosts surfaced to the frontend.
type KnownHostEntry struct {
	Host        string `json:"host"`
	Fingerprint string `json:"fingerprint"`
}

func (a *App) knownHostsPath() string {
	if a.sshKnownHostsPath != "" {
		return a.sshKnownHostsPath
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".ssh", "known_hosts")
	}
	return ""
}

// ListKnownHosts parses the known_hosts file into entries. A missing file
// yields an empty list (not an error). Lines whose key cannot be parsed (e.g.
// hashed entries) are still listed by their first field, with an empty
// fingerprint.
func (a *App) ListKnownHosts() ([]KnownHostEntry, error) {
	path := a.knownHostsPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []KnownHostEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	out := []KnownHostEntry{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		host := fields[0]
		fp := ""
		if _, _, key, _, _, err := ssh.ParseKnownHosts([]byte(line)); err == nil && key != nil {
			fp = ssh.FingerprintSHA256(key)
		}
		out = append(out, KnownHostEntry{Host: host, Fingerprint: fp})
	}
	return out, sc.Err()
}

// RemoveKnownHost drops every line whose first field matches host and rewrites
// the file. A missing file or no match is a no-op (idempotent). Hashed
// (|1|...) entries are matched by their raw first field only.
func (a *App) RemoveKnownHost(host string) error {
	path := a.knownHostsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == host {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0600)
}
