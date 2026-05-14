package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"path/filepath"
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

func TestParseClipboardFileURIsHandlesURIList(t *testing.T) {
	raw := []byte("# this is a comment\r\nfile:///home/x/a.png\r\nfile:///home/x/b%20c.jpg\r\n")
	got := parseClipboardFileURIs("text/uri-list", raw)
	want := []string{"/home/x/a.png", "/home/x/b c.jpg"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestParseClipboardFileURIsSkipsGnomeCopyHeader(t *testing.T) {
	raw := []byte("copy\nfile:///home/x/foo.png\nfile:///home/x/bar.png\n")
	got := parseClipboardFileURIs("x-special/gnome-copied-files", raw)
	want := []string{"/home/x/foo.png", "/home/x/bar.png"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestParseClipboardFileURIsIgnoresNonFileURIs(t *testing.T) {
	raw := []byte("https://example.com/foo.png\nfile:///ok.png\n")
	got := parseClipboardFileURIs("text/uri-list", raw)
	if len(got) != 1 || got[0] != "/ok.png" {
		t.Fatalf("got %v; want [/ok.png]", got)
	}
}

func TestClipboardImagePathContentType(t *testing.T) {
	cases := []struct {
		path string
		mime string
		ok   bool
	}{
		{"/a/b.PNG", "image/png", true},
		{"foo.jpg", "image/jpeg", true},
		{"foo.JPEG", "image/jpeg", true},
		{"foo.gif", "image/gif", true},
		{"foo.webp", "image/webp", true},
		{"foo.tiff", "image/tiff", true},
		{"foo.tif", "image/tiff", true},
		{"foo.txt", "", false},
		{"noext", "", false},
	}
	for _, c := range cases {
		mime, ok := clipboardImagePathContentType(c.path)
		if mime != c.mime || ok != c.ok {
			t.Fatalf("path=%q got (%q,%v); want (%q,%v)", c.path, mime, ok, c.mime, c.ok)
		}
	}
}

func TestResolveLinuxClipboardImageFromFileURIReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveLinuxClipboardImageFromFileURI(func(mime string) []byte {
		if mime == "text/uri-list" {
			return []byte("file://" + path + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if got == nil {
		t.Fatal("got = nil; want image data")
	}
	if got.ContentType != "image/png" {
		t.Fatalf("ContentType = %q; want image/png", got.ContentType)
	}
	if got.Filename != "shot.png" {
		t.Fatalf("Filename = %q; want shot.png", got.Filename)
	}
	if !bytes.Equal(got.Data, body) {
		t.Fatalf("Data = %v; want %v", got.Data, body)
	}
}

func TestResolveLinuxClipboardImageFromFileURISkipsNonImageEntries(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "note.txt")
	img := filepath.Join(dir, "ok.png")
	if err := os.WriteFile(txt, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(img, []byte{0x89}, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveLinuxClipboardImageFromFileURI(func(mime string) []byte {
		if mime == "text/uri-list" {
			return []byte("file://" + txt + "\nfile://" + img + "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if got == nil || got.Filename != "ok.png" {
		t.Fatalf("got = %+v; want ok.png", got)
	}
}

func TestResolveLinuxClipboardImageFromFileURIReturnsTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x89}, maxPasteImageBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := resolveLinuxClipboardImageFromFileURI(func(mime string) []byte {
		if mime == "text/uri-list" {
			return []byte("file://" + path + "\n")
		}
		return nil
	})
	if !errors.Is(err, errClipboardImageTooLarge) {
		t.Fatalf("err = %v; want errClipboardImageTooLarge", err)
	}
}

func TestResolveLinuxClipboardImageFromFileURINoMatchReturnsNoImage(t *testing.T) {
	_, err := resolveLinuxClipboardImageFromFileURI(func(string) []byte { return nil })
	if !errors.Is(err, errClipboardNoImage) {
		t.Fatalf("err = %v; want errClipboardNoImage", err)
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
