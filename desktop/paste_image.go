package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

const maxPasteImageBytes = 10 * 1024 * 1024

func (h *desktopPtyHost) PasteImage(ctx context.Context, sessionID uuid.UUID, p proto.PasteImagePayload) error {
	log.Printf("desktop-paste: request %s", pasteImageLogDetails(sessionID, p))
	path, err := savePastedImage(sessionID, p)
	if err != nil {
		log.Printf("desktop-paste: save_failed %s error=%v", pasteImageLogDetails(sessionID, p), err)
		return err
	}
	log.Printf("desktop-paste: saved %s path=%q", pasteImageLogDetails(sessionID, p), path)
	if err := setNativeClipboardImage(ctx, path, p.ContentType); err == nil {
		_, err = h.Write([]byte("\x16"))
		if err != nil {
			log.Printf("desktop-paste: native_write_failed session=%s path=%q error=%v", sessionID, path, err)
		} else {
			log.Printf("desktop-paste: native_clipboard_ok session=%s path=%q", sessionID, path)
		}
		return err
	} else {
		log.Printf("desktop-paste: native_clipboard_failed %s path=%q error=%v", pasteImageLogDetails(sessionID, p), path, err)
	}
	_, err = h.Write([]byte(path))
	if err != nil {
		log.Printf("desktop-paste: fallback_path_write_failed session=%s path=%q error=%v", sessionID, path, err)
	} else {
		log.Printf("desktop-paste: fallback_path_write_ok session=%s path=%q", sessionID, path)
	}
	return err
}

func pasteImageLogDetails(sessionID uuid.UUID, p proto.PasteImagePayload) string {
	return fmt.Sprintf("session=%s filename=%q content_type=%q image_bytes=%d", sessionID, p.Filename, p.ContentType, len(p.Data))
}

func setNativeClipboardImage(ctx context.Context, path, contentType string) error {
	switch runtime.GOOS {
	case "darwin":
		return setMacClipboardImage(ctx, path, contentType)
	case "linux":
		return setLinuxClipboardImage(ctx, path, contentType)
	case "windows":
		return setWindowsClipboardImage(ctx, path)
	default:
		return fmt.Errorf("unsupported OS %q", runtime.GOOS)
	}
}

func savePastedImage(sessionID uuid.UUID, p proto.PasteImagePayload) (string, error) {
	if !strings.HasPrefix(strings.ToLower(p.ContentType), "image/") {
		return "", fmt.Errorf("paste image: unsupported content type %q", p.ContentType)
	}
	if len(p.Data) == 0 {
		return "", fmt.Errorf("paste image: empty image")
	}
	if len(p.Data) > maxPasteImageBytes {
		return "", fmt.Errorf("paste image: image too large")
	}
	base, err := appdir.CacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "paste-images", sessionID.String())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d%s", time.Now().UnixNano(), pasteImageExt(p.ContentType, p.Filename))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, p.Data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func pasteImageExt(contentType, filename string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".tif", ".tiff":
		return ext
	default:
		return ".img"
	}
}

func setMacClipboardImage(ctx context.Context, path, contentType string) error {
	class, ok := macClipboardImageClass(contentType)
	if !ok {
		return fmt.Errorf("unsupported mac clipboard image type %q", contentType)
	}
	script := fmt.Sprintf(`set the clipboard to (read (POSIX file "%s") as %s)`, appleScriptString(path), class)
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	return cmd.Run()
}

type commandSpec struct {
	name string
	args []string
}

func setLinuxClipboardImage(ctx context.Context, path, contentType string) error {
	var lastErr error
	for _, spec := range linuxClipboardCommands(path, contentType) {
		if _, err := exec.LookPath(spec.name); err != nil {
			lastErr = err
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, spec.name, spec.args...)
		cmd.Stdin = f
		err = cmd.Run()
		_ = f.Close()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no Linux clipboard image tool found")
	}
	return lastErr
}

func linuxClipboardCommands(_ string, contentType string) []commandSpec {
	return []commandSpec{
		{name: "wl-copy", args: []string{"--type", contentType}},
		{name: "xclip", args: []string{"-selection", "clipboard", "-t", contentType, "-i"}},
		{name: "xsel", args: []string{"--clipboard", "--input", "--mime-type", contentType}},
	}
}

func setWindowsClipboardImage(ctx context.Context, path string) error {
	powershell, err := windowsPowerShell()
	if err != nil {
		return err
	}
	psPath := powerShellSingleQuotedString(path)
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"$image = [System.Drawing.Image]::FromFile(" + psPath + ")",
		"try { [System.Windows.Forms.Clipboard]::SetImage($image) } finally { $image.Dispose() }",
	}, "; ")
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-STA", "-Command", script)
	return cmd.Run()
}

func windowsPowerShell() (string, error) {
	for _, name := range []string{"powershell.exe", "powershell", "pwsh.exe", "pwsh"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("powershell not found")
}

func macClipboardImageClass(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return "«class PNGf»", true
	case "image/jpeg", "image/jpg":
		return "JPEG picture", true
	case "image/gif":
		return "GIF picture", true
	case "image/tiff":
		return "TIFF picture", true
	default:
		return "", false
	}
}

func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

func powerShellSingleQuotedString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

