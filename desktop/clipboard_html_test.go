package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRichClipboardHistoryPreservesHTMLBeforeImage(t *testing.T) {
	html := `<div data-atterm-input-history="1">中文<img src="data:image/png;base64,AQ=="></div>`
	got := readRichClipboardPastePayload(context.Background(),
		func() (string, error) { return "中文[Image #1]", nil },
		func(context.Context) (*clipboardImageData, error) {
			t.Fatal("history HTML must not become a single image")
			return nil, nil
		},
		func(context.Context) (string, error) { return html, nil },
	)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["kind"] != "text" || payload["text"] != "中文[Image #1]" || payload["html"] != html {
		t.Fatalf("rich clipboard payload lost content: %v", payload)
	}
}

func TestRichClipboardOrdinaryHTMLKeepsImagePriority(t *testing.T) {
	for _, html := range []string{"", "<p>ordinary rich text</p>"} {
		got := readRichClipboardPastePayload(context.Background(),
			func() (string, error) { return "text", nil },
			func(context.Context) (*clipboardImageData, error) {
				return &clipboardImageData{ContentType: "image/png", Data: []byte{1}}, nil
			},
			func(context.Context) (string, error) { return html, nil },
		)
		if got.Kind != "image" || got.HTML != "" {
			t.Fatalf("ordinary paste changed: %+v", got)
		}
	}
}

func TestRichClipboardHTMLReadFailureFallsBackToText(t *testing.T) {
	got := readRichClipboardPastePayload(context.Background(),
		func() (string, error) { return "plain", nil },
		func(context.Context) (*clipboardImageData, error) { return nil, errClipboardNoImage },
		func(context.Context) (string, error) { return "", errors.New("unsupported") },
	)
	if got.Kind != "text" || got.Text != "plain" || got.HTML != "" {
		t.Fatalf("fallback = %+v", got)
	}
}

func TestSetClipboardHTMLRejectsInvalidDataBeforeWriting(t *testing.T) {
	for _, tc := range []struct{ name, text, html string }{
		{"empty HTML", "text", ""},
		{"oversized HTML", "text", strings.Repeat("h", 24*1024*1024+1)},
		{"oversized text", strings.Repeat("t", 24*1024*1024+1), "<p>ok</p>"},
		{"invalid UTF8", "text", "\xff"},
		{"embedded NUL", "a\x00b", "<p>ok</p>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := (&App{}).SetClipboardHTML(tc.text, tc.html); err == nil {
				t.Fatal("invalid clipboard data accepted")
			}
		})
	}
}

func TestWindowsClipboardHTMLByteOffsetsIncludeUnicode(t *testing.T) {
	fragment := `<div data-atterm-input-history="1">你好🙂<img src="data:image/png;base64,AQ=="></div>`
	raw := windowsClipboardHTML(fragment)
	offset := func(key string) int {
		_, after, ok := strings.Cut(raw, key+":")
		if !ok {
			t.Fatalf("missing %s", key)
		}
		value, _, _ := strings.Cut(after, "\r\n")
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	start, end := offset("StartFragment"), offset("EndFragment")
	if raw[start:end] != fragment {
		t.Fatal("CF_HTML offsets do not address UTF-8 fragment")
	}
	if offset("EndHTML") != len(raw) {
		t.Fatal("CF_HTML end does not match byte length")
	}
	if got := unwrapWindowsClipboardHTML(raw); got != fragment {
		t.Fatalf("fragment = %q", got)
	}
}

func TestWindowsClipboardHTMLHandlesNonCFHTML(t *testing.T) {
	for _, raw := range []string{"<p>ordinary</p>", "StartFragment:9999999999\r\nEndFragment:1\r\n<p>malformed</p>"} {
		if got := unwrapWindowsClipboardHTML(raw); got != raw {
			t.Fatalf("malformed/raw HTML changed: %q", got)
		}
	}
}

func TestClipboardHTMLSubprocessOutputIsBounded(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestClipboardHTMLOutputProcess$")
	cmd.Env = append(os.Environ(), "ATTERM_TEST_CLIPBOARD_HTML_OUTPUT=1")
	_, err = clipboardHTMLOutput(cmd)
	if !errors.Is(err, errClipboardHTMLTooLarge) {
		t.Fatalf("unbounded command output: %v", err)
	}
}

func TestClipboardHTMLOutputProcess(t *testing.T) {
	if os.Getenv("ATTERM_TEST_CLIPBOARD_HTML_OUTPUT") != "1" {
		return
	}
	chunk := bytes.Repeat([]byte{'x'}, 1024*1024)
	for i := 0; i < 26; i++ {
		if _, err := os.Stdout.Write(chunk); err != nil {
			break
		}
	}
	os.Exit(0)
}
