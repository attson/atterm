package main

import (
	"path/filepath"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestPasteImageExtPrefersContentType(t *testing.T) {
	if got := pasteImageExt("image/png", "clip.jpeg"); got != ".png" {
		t.Fatalf("ext = %q; want .png", got)
	}
	if got := pasteImageExt("image/webp", "clip.png"); got != ".webp" {
		t.Fatalf("ext = %q; want .webp", got)
	}
}

func TestSavePastedImageWritesPrivateCacheFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))

	path, err := savePastedImage(uuid.New(), proto.PasteImagePayload{
		Filename:    "ignored.jpg",
		ContentType: "image/png",
		Data:        []byte{0x89, 'P', 'N', 'G'},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("path = %q; want .png extension", path)
	}
}

func TestAppleScriptStringEscapesPath(t *testing.T) {
	got := appleScriptString(`/tmp/a"b\c.png`)
	want := `/tmp/a\"b\\c.png`
	if got != want {
		t.Fatalf("escaped = %q; want %q", got, want)
	}
}

func TestPowerShellSingleQuotedStringEscapesPath(t *testing.T) {
	got := powerShellSingleQuotedString(`C:\tmp\a'b.png`)
	want := `'C:\tmp\a''b.png'`
	if got != want {
		t.Fatalf("escaped = %q; want %q", got, want)
	}
}

func TestLinuxClipboardCommandsPreferWaylandThenX11(t *testing.T) {
	cmds := linuxClipboardCommands("/tmp/clip.png", "image/png")
	if len(cmds) != 3 {
		t.Fatalf("commands = %d; want 3", len(cmds))
	}
	if cmds[0].name != "wl-copy" || cmds[0].args[0] != "--type" || cmds[0].args[1] != "image/png" {
		t.Fatalf("first command = %#v; want wl-copy --type image/png", cmds[0])
	}
	if cmds[1].name != "xclip" {
		t.Fatalf("second command = %#v; want xclip", cmds[1])
	}
	if cmds[2].name != "xsel" {
		t.Fatalf("third command = %#v; want xsel", cmds[2])
	}
}
