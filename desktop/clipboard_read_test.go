package main

import (
	"context"
	"encoding/base64"
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
