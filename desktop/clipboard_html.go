package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxClipboardHTMLBytes = 24 * 1024 * 1024

var (
	errClipboardHTMLUnsupported = errors.New("rich clipboard writing is unsupported on this platform")
	errClipboardHTMLTooLarge    = errors.New("clipboard HTML too large")
)

// SetClipboardHTML publishes both formats in one clipboard item so external
// editors and AT Term can select the representation they understand.
func (a *App) SetClipboardHTML(text, html string) error {
	if len(html) == 0 {
		return errors.New("clipboard HTML is empty")
	}
	if len(html) > maxClipboardHTMLBytes || len(text) > maxClipboardHTMLBytes {
		return errClipboardHTMLTooLarge
	}
	if !utf8.ValidString(text) || !utf8.ValidString(html) || strings.ContainsRune(text, 0) || strings.ContainsRune(html, 0) {
		return errors.New("clipboard content must be UTF-8 without NUL characters")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	switch runtime.GOOS {
	case "darwin":
		return writeMacClipboardHTML(text, html)
	case "windows":
		return writeWindowsClipboardHTML(ctx, text, html)
	default:
		return errClipboardHTMLUnsupported
	}
}

func nativeClipboardHTML(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		return readMacClipboardHTML()
	case "windows":
		return readWindowsClipboardHTML(ctx)
	case "linux":
		// xsel only supports text, even when a --mime-type flag is supplied.
		for _, spec := range linuxClipboardImageReadSpecs("text/html")[:2] {
			if _, err := exec.LookPath(spec.name); err != nil {
				continue
			}
			out, err := clipboardHTMLOutput(exec.CommandContext(ctx, spec.name, spec.args...))
			if err == nil && len(out) > 0 {
				return string(out), nil
			}
			if errors.Is(err, errClipboardHTMLTooLarge) {
				return "", err
			}
		}
	}
	return "", nil
}

type clipboardHTMLBuffer struct {
	buffer   bytes.Buffer
	tooLarge bool
}

func (b *clipboardHTMLBuffer) Write(p []byte) (int, error) {
	// CF_HTML adds offsets and a document wrapper to the fragment's size.
	if len(p) > maxClipboardHTMLBytes+1024-b.buffer.Len() {
		b.tooLarge = true
		return 0, errClipboardHTMLTooLarge
	}
	return b.buffer.Write(p)
}

func clipboardHTMLOutput(cmd *exec.Cmd) ([]byte, error) {
	var output clipboardHTMLBuffer
	cmd.Stdout = &output
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if output.tooLarge {
		return nil, errClipboardHTMLTooLarge
	}
	if err != nil {
		return nil, err
	}
	return output.buffer.Bytes(), nil
}

func writeWindowsClipboardHTML(ctx context.Context, text, html string) error {
	powershell, err := windowsPowerShell()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	payload, err := json.Marshal(struct{ Text, HTML string }{text, windowsClipboardHTML(html)})
	if err != nil {
		return err
	}
	// Stdin avoids command-line size limits and keeps clipboard contents out of
	// process listings. One DataObject prevents HTML from replacing plain text.
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"Add-Type -AssemblyName System.Windows.Forms",
		"[Console]::InputEncoding = [System.Text.Encoding]::UTF8",
		"$payload = [Console]::In.ReadToEnd() | ConvertFrom-Json",
		"$data = New-Object System.Windows.Forms.DataObject",
		"$data.SetText([string]$payload.Text, [System.Windows.Forms.TextDataFormat]::UnicodeText)",
		"$data.SetText([string]$payload.HTML, [System.Windows.Forms.TextDataFormat]::Html)",
		"[System.Windows.Forms.Clipboard]::SetDataObject($data, $true)",
	}, "; ")
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write rich clipboard: %w", err)
	}
	return nil
}

func readWindowsClipboardHTML(ctx context.Context) (string, error) {
	powershell, err := windowsPowerShell()
	if err != nil {
		return "", err
	}
	script := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"Add-Type -AssemblyName System.Windows.Forms",
		"[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)",
		"$html = [System.Windows.Forms.Clipboard]::GetText([System.Windows.Forms.TextDataFormat]::Html)",
		"[Console]::Write($html)",
	}, "; ")
	out, err := clipboardHTMLOutput(exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-STA", "-Command", script))
	if err != nil {
		return "", err
	}
	return unwrapWindowsClipboardHTML(string(out)), nil
}

func windowsClipboardHTML(fragment string) string {
	const header = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	const prefix = "<html><body><!--StartFragment-->"
	const suffix = "<!--EndFragment--></body></html>"
	startHTML := len(fmt.Sprintf(header, 0, 0, 0, 0))
	startFragment := startHTML + len(prefix)
	endFragment := startFragment + len(fragment)
	return fmt.Sprintf(header, startHTML, endFragment+len(suffix), startFragment, endFragment) + prefix + fragment + suffix
}

func unwrapWindowsClipboardHTML(raw string) string {
	offset := func(key string) (int, error) {
		_, rest, ok := strings.Cut(raw, key+":")
		if !ok {
			return 0, errors.New("CF_HTML offset absent")
		}
		line, _, _ := strings.Cut(rest, "\n")
		return strconv.Atoi(strings.TrimSpace(line))
	}
	start, startErr := offset("StartFragment")
	end, endErr := offset("EndFragment")
	if startErr == nil && endErr == nil && start >= 0 && end >= start && end <= len(raw) {
		return raw[start:end]
	}
	return raw
}
