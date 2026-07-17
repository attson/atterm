//go:build linux

package trash

import "fmt"

// sendPlatform prefers `gio trash` (Nautilus / GLib) and falls back to
// `kioclient5 move ... trash:/` for KDE hosts. Everything else surfaces
// ErrUnavailable so the caller can offer a hard-delete fallback.
func sendPlatform(path string) error {
	if _, err := lookPath("gio"); err == nil {
		cmd := execCommand("gio", "trash", path)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("trash: gio: %w", err)
		}
		return nil
	}
	if _, err := lookPath("kioclient5"); err == nil {
		cmd := execCommand("kioclient5", "move", path, "trash:/")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("trash: kioclient5: %w", err)
		}
		return nil
	}
	return ErrUnavailable
}
