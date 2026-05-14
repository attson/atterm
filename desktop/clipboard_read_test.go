package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestClipboardPastePayloadPrefersImageOverText(t *testing.T) {
	got := clipboardPastePayload("echo hi\n", &clipboardImageData{
		Filename:    "clipboard-image.png",
		ContentType: "image/png",
		Data:        []byte{0x89, 'P', 'N', 'G'},
	}, nil)

	if got.Kind != "image" {
		t.Fatalf("Kind = %q; want image", got.Kind)
	}
	if got.Text != "" {
		t.Fatalf("Text = %q; want empty", got.Text)
	}
	if got.DataBase64 != base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("DataBase64 = %q; want encoded PNG bytes", got.DataBase64)
	}
}

func TestClipboardPastePayloadFallsBackToTextWhenImageUnavailable(t *testing.T) {
	got := clipboardPastePayload("echo hi\n", nil, errClipboardNoImage)

	if got.Kind != "text" {
		t.Fatalf("Kind = %q; want text", got.Kind)
	}
	if got.Text != "echo hi\n" {
		t.Fatalf("Text = %q; want original text", got.Text)
	}
}

func TestClipboardPastePayloadRejectsOversizedImageBeforeTextFallback(t *testing.T) {
	got := clipboardPastePayload("echo hi\n", nil, errClipboardImageTooLarge)

	if got.Kind != "none" {
		t.Fatalf("Kind = %q; want none", got.Kind)
	}
	if !strings.Contains(got.Reason, "too large") {
		t.Fatalf("Reason = %q; want size hint", got.Reason)
	}
}

func TestReadClipboardPastePayloadUsesImagePriority(t *testing.T) {
	got := readClipboardPastePayload(
		context.Background(),
		func() (string, error) { return "echo hi\n", nil },
		func(context.Context) (*clipboardImageData, error) {
			return &clipboardImageData{
				Filename:    "clipboard-image.png",
				ContentType: "image/png",
				Data:        []byte{0x89, 'P', 'N', 'G'},
			}, nil
		},
	)

	if got.Kind != "image" {
		t.Fatalf("Kind = %q; want image", got.Kind)
	}
}

func TestReadLinuxClipboardImageReportsMissingToolsSilently(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	var buf bytes.Buffer
	orig := log.Default().Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	_, err := readLinuxClipboardImage(context.Background())
	if !errors.Is(err, errClipboardNoLinuxTools) {
		t.Fatalf("err = %v; want errClipboardNoLinuxTools", err)
	}
	if buf.Len() > 0 {
		t.Fatalf("log output = %q; want silent", buf.String())
	}
	msg := errClipboardNoLinuxTools.Error()
	for _, want := range []string{"xclip", "wl-paste", "xsel"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message %q missing %q", msg, want)
		}
	}
}

func TestClipboardPastePayloadSurfacesMissingLinuxToolsAsReason(t *testing.T) {
	got := clipboardPastePayload("", nil, errClipboardNoLinuxTools)

	if got.Kind != "none" {
		t.Fatalf("Kind = %q; want none", got.Kind)
	}
	if !strings.Contains(got.Reason, "xclip") {
		t.Fatalf("Reason = %q; want install hint", got.Reason)
	}
}

func TestClipboardPastePayloadStillReturnsTextWhenLinuxToolsMissing(t *testing.T) {
	got := clipboardPastePayload("echo hi\n", nil, errClipboardNoLinuxTools)

	if got.Kind != "text" {
		t.Fatalf("Kind = %q; want text (text paste shouldn't be blocked by missing image tools)", got.Kind)
	}
	if got.Text != "echo hi\n" {
		t.Fatalf("Text = %q; want original text", got.Text)
	}
}

func TestLinuxClipboardImageReadSpecsPreferWaylandThenX11(t *testing.T) {
	specs := linuxClipboardImageReadSpecs("image/png")

	if len(specs) != 3 {
		t.Fatalf("specs = %d; want 3", len(specs))
	}
	if specs[0].name != "wl-paste" {
		t.Fatalf("first spec = %#v; want wl-paste", specs[0])
	}
	if specs[1].name != "xclip" {
		t.Fatalf("second spec = %#v; want xclip", specs[1])
	}
	if specs[2].name != "xsel" {
		t.Fatalf("third spec = %#v; want xsel", specs[2])
	}
}
