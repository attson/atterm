package termius

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

// RequireStopped checks before reading either the profile or its credentials.
// Closing an Electron window can leave the database owner running in the tray.
func RequireStopped() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names, err := processNames(ctx)
	if err != nil {
		return errors.New("termius_process_check_failed")
	}
	for _, name := range names {
		if isTermiusProcess(name) {
			return errors.New("termius_running")
		}
	}
	return nil
}

func isTermiusProcess(name string) bool {
	name = strings.ToLower(filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")))
	name = strings.TrimSuffix(name, ".exe")
	return name == "termius" || name == "termius-app" || name == "termius helper" || strings.HasPrefix(name, "termius helper (")
}
