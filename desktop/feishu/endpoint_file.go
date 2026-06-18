// Hook-endpoint discovery file.
//
//	~/.config/atterm/hook-endpoint           (POSIX)
//	%APPDATA%\atterm\hook-endpoint           (Windows)
package feishu

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func endpointFilePath() (string, error) {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "atterm", "hook-endpoint"), nil
		}
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "atterm", "hook-endpoint"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "atterm", "hook-endpoint"), nil
}

func WriteEndpointFile(url string) error {
	path, err := endpointFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(url), 0o600)
}

func ReadEndpointFile() (string, error) {
	path, err := endpointFilePath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func DeleteEndpointFile() error {
	path, err := endpointFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
