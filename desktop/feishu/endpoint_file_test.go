package feishu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEndpointFile_WriteAndRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("HOME", t.TempDir())
	}
	url := "http://127.0.0.1:12345/atterm-hook/notify"
	if err := WriteEndpointFile(url); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadEndpointFile()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != url {
		t.Fatalf("got %q want %q", got, url)
	}
	if err := DeleteEndpointFile(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(endpointFilePathForTest(t)); !os.IsNotExist(err) {
		t.Fatalf("expected gone")
	}
}

func TestEndpointFile_ReadMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		t.Setenv("HOME", t.TempDir())
	}
	if _, err := ReadEndpointFile(); err == nil {
		t.Fatalf("expected error on missing file")
	}
}

func endpointFilePathForTest(t *testing.T) string {
	t.Helper()
	p, err := endpointFilePath()
	if err != nil {
		t.Fatal(err)
	}
	_ = filepath.Clean(p)
	return p
}
