// Package hookinstall materializes the atterm-hook binary onto disk
// and patches ~/.claude/settings.json so claude-code triggers it on
// Notification events. Exposes Install / Uninstall / Check. All
// file-IO functions accept an explicit home parameter; production
// callers pass os.UserHomeDir().
package hookinstall
