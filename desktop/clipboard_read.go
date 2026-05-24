package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ClipboardPastePayload struct {
	Kind        string `json:"kind"`
	Text        string `json:"text,omitempty"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type clipboardImageData struct {
	Filename    string
	ContentType string
	Data        []byte
}

var (
	errClipboardNoImage       = errors.New("clipboard has no image")
	errClipboardImageTooLarge = errors.New("clipboard image too large")
	// errClipboardNoLinuxTools is returned by readLinuxClipboardImage when
	// none of wl-paste/xclip/xsel are on $PATH. Surfaced verbatim to the
	// frontend via Reason so the user gets an actionable hint instead of a
	// silent fall-through to text or a noisy stderr log on every paste.
	errClipboardNoLinuxTools = errors.New("install xclip, wl-paste, or xsel to paste images")
)

func clipboardPastePayload(text string, image *clipboardImageData, imageErr error) ClipboardPastePayload {
	switch {
	case image != nil:
		return ClipboardPastePayload{
			Kind:        "image",
			Filename:    image.Filename,
			ContentType: image.ContentType,
			DataBase64:  base64.StdEncoding.EncodeToString(image.Data),
		}
	case errors.Is(imageErr, errClipboardImageTooLarge):
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: imageErr.Error(),
		}
	}

	if text != "" {
		return ClipboardPastePayload{
			Kind: "text",
			Text: text,
		}
	}
	if imageErr != nil && !errors.Is(imageErr, errClipboardNoImage) {
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: imageErr.Error(),
		}
	}
	return ClipboardPastePayload{
		Kind:   "none",
		Reason: "clipboard has no text or image",
	}
}

func readClipboardPastePayload(
	ctx context.Context,
	textReader func() (string, error),
	imageReader func(context.Context) (*clipboardImageData, error),
) ClipboardPastePayload {
	image, imageErr := imageReader(ctx)
	if image != nil {
		return clipboardPastePayload("", image, nil)
	}
	if errors.Is(imageErr, errClipboardImageTooLarge) {
		return clipboardPastePayload("", nil, imageErr)
	}

	text, textErr := textReader()
	switch {
	case textErr == nil:
		return clipboardPastePayload(text, nil, imageErr)
	case imageErr != nil && !errors.Is(imageErr, errClipboardNoImage):
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: imageErr.Error(),
		}
	case textErr != nil:
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: textErr.Error(),
		}
	default:
		return ClipboardPastePayload{
			Kind:   "none",
			Reason: "clipboard has no text or image",
		}
	}
}

func readClipboardPastePayloadFromRuntime(ctx context.Context) ClipboardPastePayload {
	return readClipboardPastePayload(
		ctx,
		func() (string, error) {
			return wailsruntime.ClipboardGetText(ctx)
		},
		nativeClipboardImage,
	)
}

func nativeClipboardImage(ctx context.Context) (*clipboardImageData, error) {
	switch runtime.GOOS {
	case "darwin":
		return readMacClipboardImage(ctx)
	case "linux":
		return readLinuxClipboardImage(ctx)
	case "windows":
		return readWindowsClipboardImage(ctx)
	default:
		return nil, errClipboardNoImage
	}
}

func readClipboardImageFile(path, contentType, filename string) (*clipboardImageData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errClipboardNoImage
	}
	if len(data) > maxPasteImageBytes {
		return nil, errClipboardImageTooLarge
	}
	return &clipboardImageData{
		Filename:    filename,
		ContentType: contentType,
		Data:        data,
	}, nil
}

func readMacClipboardImage(ctx context.Context) (*clipboardImageData, error) {
	path, cleanup, err := writeMacClipboardImageToTemp(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return readClipboardImageFile(path, "image/png", "clipboard-image.png")
}

func writeMacClipboardImageToTemp(ctx context.Context) (string, func(), error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("atterm-clipboard-%d.png", time.Now().UnixNano()))
	script := strings.Join([]string{
		`set outPath to POSIX file "` + appleScriptString(path) + `"`,
		`try`,
		`set pngData to the clipboard as «class PNGf»`,
		`on error`,
		`error "NO_IMAGE"`,
		`end try`,
		`set outFile to open for access outPath with write permission`,
		`set eof outFile to 0`,
		`write pngData to outFile starting at eof`,
		`close access outFile`,
	}, "\n")
	if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
		return "", nil, errClipboardNoImage
	}
	return path, func() {
		_ = os.Remove(path)
	}, nil
}

func readWindowsClipboardImage(ctx context.Context) (*clipboardImageData, error) {
	path, cleanup, err := writeWindowsClipboardImageToTemp(ctx)
	if err == nil {
		defer cleanup()
		return readClipboardImageFile(path, "image/png", "clipboard-image.png")
	}
	paths, err := readWindowsClipboardFileDropPaths(ctx)
	if err != nil {
		return nil, errClipboardNoImage
	}
	return resolveClipboardImageFromPaths(paths)
}

func writeWindowsClipboardImageToTemp(ctx context.Context) (string, func(), error) {
	powershell, err := windowsPowerShell()
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("atterm-clipboard-%d.png", time.Now().UnixNano()))
	psPath := powerShellSingleQuotedString(path)
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"if (-not [System.Windows.Forms.Clipboard]::ContainsImage()) { exit 3 }",
		"$image = [System.Windows.Forms.Clipboard]::GetImage()",
		"try { $image.Save(" + psPath + ", [System.Drawing.Imaging.ImageFormat]::Png) } finally { $image.Dispose() }",
	}, "; ")
	if err := exec.CommandContext(ctx, powershell, "-NoProfile", "-STA", "-Command", script).Run(); err != nil {
		return "", nil, errClipboardNoImage
	}
	return path, func() {
		_ = os.Remove(path)
	}, nil
}

func readWindowsClipboardFileDropPaths(ctx context.Context) ([]string, error) {
	powershell, err := windowsPowerShell()
	if err != nil {
		return nil, err
	}
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"$files = [System.Windows.Forms.Clipboard]::GetFileDropList()",
		"if ($null -eq $files -or $files.Count -eq 0) { exit 3 }",
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8",
		"foreach ($file in $files) { [Console]::WriteLine($file) }",
	}, "; ")
	out, err := exec.CommandContext(ctx, powershell, "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		return nil, errClipboardNoImage
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return nil, errClipboardNoImage
	}
	return paths, nil
}

func linuxClipboardImageReadSpecs(contentType string) []commandSpec {
	return []commandSpec{
		{name: "wl-paste", args: []string{"--no-newline", "--type", contentType}},
		{name: "xclip", args: []string{"-selection", "clipboard", "-t", contentType, "-o"}},
		{name: "xsel", args: []string{"--clipboard", "--output", "--mime-type", contentType}},
	}
}

func readLinuxClipboardImage(ctx context.Context) (*clipboardImageData, error) {
	foundTool := false
	readMime := func(mime string) []byte {
		for _, spec := range linuxClipboardImageReadSpecs(mime) {
			if _, err := exec.LookPath(spec.name); err != nil {
				continue
			}
			foundTool = true
			out, err := exec.CommandContext(ctx, spec.name, spec.args...).Output()
			if err != nil || len(out) == 0 {
				continue
			}
			return out
		}
		return nil
	}

	for _, contentType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/tiff"} {
		out := readMime(contentType)
		if len(out) == 0 {
			continue
		}
		if len(out) > maxPasteImageBytes {
			return nil, errClipboardImageTooLarge
		}
		return &clipboardImageData{
			Filename:    "clipboard-image" + pasteImageExt(contentType, "clipboard-image"),
			ContentType: contentType,
			Data:        out,
		}, nil
	}

	// Fallback: file managers (Nautilus, Dolphin, ...) put a file reference
	// on the clipboard instead of the bitmap. Resolve the file path and read
	// it ourselves so "copy image file → paste" works the same as "screenshot
	// → paste".
	if img, err := resolveLinuxClipboardImageFromFileURI(readMime); err != nil {
		if errors.Is(err, errClipboardImageTooLarge) {
			return nil, err
		}
	} else if img != nil {
		return img, nil
	}

	if !foundTool {
		return nil, errClipboardNoLinuxTools
	}
	return nil, errClipboardNoImage
}

// resolveLinuxClipboardImageFromFileURI reads file-reference clipboard targets
// (text/uri-list, x-special/gnome-copied-files), picks the first entry whose
// extension looks like an image, and returns its bytes.
func resolveLinuxClipboardImageFromFileURI(readMime func(string) []byte) (*clipboardImageData, error) {
	for _, target := range []string{"text/uri-list", "x-special/gnome-copied-files"} {
		raw := readMime(target)
		if len(raw) == 0 {
			continue
		}
		if img, err := resolveClipboardImageFromPaths(parseClipboardFileURIs(target, raw)); err != nil {
			if errors.Is(err, errClipboardImageTooLarge) {
				return nil, err
			}
		} else if img != nil {
			return img, nil
		}
	}
	return nil, errClipboardNoImage
}

func resolveClipboardImageFromPaths(paths []string) (*clipboardImageData, error) {
	for _, path := range paths {
		contentType, ok := clipboardImagePathContentType(path)
		if !ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) > maxPasteImageBytes {
			return nil, errClipboardImageTooLarge
		}
		return &clipboardImageData{
			Filename:    filepath.Base(path),
			ContentType: contentType,
			Data:        data,
		}, nil
	}
	return nil, errClipboardNoImage
}

// parseClipboardFileURIs decodes a clipboard payload of the given target type
// into a list of local filesystem paths. Handles both the standard text/uri-list
// format (RFC 2483, one URI per line, '#' comments) and GNOME's
// x-special/gnome-copied-files (prepends a "copy"/"cut" header line).
func parseClipboardFileURIs(target string, raw []byte) []string {
	var paths []string
	gnome := target == "x-special/gnome-copied-files"
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if gnome && i == 0 && (line == "copy" || line == "cut") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "file://") {
			continue
		}
		path, err := fileURIToPath(line)
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func fileURIToPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %q", uri)
	}
	if parsed.Path == "" {
		return "", fmt.Errorf("file URI has no path: %q", uri)
	}
	return parsed.Path, nil
}

func clipboardImagePathContentType(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".tif", ".tiff":
		return "image/tiff", true
	}
	return "", false
}
