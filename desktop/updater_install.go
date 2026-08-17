package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

func installPathFromExecutable(exe, goos string) string {
	if goos == "darwin" {
		// Walk parents until we hit a directory ending in ".app". Robust
		// against alternate install locations (~/Applications, /Applications,
		// /opt/atterm/Applications/...).
		dir := exe
		for {
			parent := filepath.Dir(dir)
			if parent == dir {
				return exe // not in a bundle; fall back to executable path
			}
			if filepath.Ext(parent) == ".app" {
				return parent
			}
			dir = parent
		}
	}
	return exe
}

// helperResource picks the embedded script + extension for the running OS.
func helperResource(goos string) (path string, ext string, ok bool) {
	switch goos {
	case "darwin":
		return "scripts/install-darwin.sh", ".sh", true
	case "linux":
		return "scripts/install-linux.sh", ".sh", true
	case "windows":
		return "scripts/install-windows.ps1", ".ps1", true
	}
	return "", "", false
}

// extractHelper writes the embedded helper for the current OS to a fresh
// temp file. POSIX scripts get +x.
func (u *Updater) extractHelper() (string, error) {
	src, ext, ok := helperResource(runtime.GOOS)
	if !ok {
		return "", fmt.Errorf("no install helper for %s", runtime.GOOS)
	}
	body, err := installScripts.ReadFile(src)
	if err != nil {
		return "", err
	}
	dir, err := u.updatesDir()
	if err != nil {
		return "", err
	}
	helperPath := filepath.Join(dir, "install-helper-"+strconv.Itoa(os.Getpid())+ext)
	mode := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		mode = 0o755
	}
	if err := os.WriteFile(helperPath, body, mode); err != nil {
		return "", err
	}
	return helperPath, nil
}

// archivePath returns the cache path of the most recently downloaded asset.
func (u *Updater) archivePath() (string, error) {
	dir, err := u.updatesDir()
	if err != nil {
		return "", err
	}
	u.mu.Lock()
	latest := u.state.Latest
	u.mu.Unlock()
	if latest == "" {
		return "", fmt.Errorf("no version downloaded")
	}
	name, err := assetNameForPlatform(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, latest+"-"+name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("download not found at %s: %w", p, err)
	}
	return p, nil
}

// InstallAndQuit spawns the install helper detached, then returns. The
// caller (App binding layer) is responsible for calling Wails runtime.Quit
// next so the helper can take over after our process exits.
//
// Returns immediately on error; the app stays alive in that case so the
// UI can surface what went wrong.
func (u *Updater) InstallAndQuit() error {
	if u.devOrEmpty() {
		return fmt.Errorf("auto-update disabled in dev builds")
	}
	u.mu.Lock()
	if !u.state.Ready {
		u.mu.Unlock()
		return fmt.Errorf("nothing downloaded yet")
	}
	u.mu.Unlock()

	src, err := u.archivePath()
	if err != nil {
		u.recordError(err)
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		u.recordError(err)
		return err
	}
	dst := installPathFromExecutable(exe, runtime.GOOS)

	helperPath, err := u.extractHelper()
	if err != nil {
		u.recordError(err)
		return err
	}

	pid := strconv.Itoa(os.Getpid())
	cmd := buildHelperCommand(helperPath, pid, src, dst)

	if err := cmd.Start(); err != nil {
		u.recordError(err)
		return err
	}
	// Detach: don't Wait(). The helper waits on our PID via kill -0 / Get-Process
	// loop and survives our exit because the relaunch path inside the helper
	// uses platform-native detachment (`open` on darwin, `setsid &` on linux,
	// `Start-Process` on windows).
	return nil
}

// buildHelperCommand returns the platform-appropriate exec.Cmd for invoking
// the install helper script.
func buildHelperCommand(helperPath, pid, src, dst string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin", "linux":
		return exec.Command("/bin/bash", helperPath, pid, src, dst)
	case "windows":
		return exec.Command(
			"powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass",
			"-File", helperPath,
			"-ProcessId", pid,
			"-Src", src,
			"-Dst", dst,
		)
	}
	return exec.Command("false")
}
