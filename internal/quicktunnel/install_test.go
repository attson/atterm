package quicktunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestInstallerInstallsPinnedExecutable(t *testing.T) {
	t.Parallel()

	binary := []byte("verified cloudflared executable")
	installer := testInstaller(t, binary, false)
	status, err := installer.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Supported || !status.Installed || status.Version != ManagedCloudflaredVersion {
		t.Fatalf("install status = %+v", status)
	}
	got, err := os.ReadFile(installer.ExecutablePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("installed executable = %q", got)
	}
	if err := installer.VerifyExecutable(installer.ExecutablePath()); err != nil {
		t.Fatalf("VerifyExecutable: %v", err)
	}
}

func TestInstallerExtractsOnlyCloudflaredFromTGZ(t *testing.T) {
	t.Parallel()

	binary := []byte("darwin cloudflared")
	archive := makeTGZ(t, map[string][]byte{
		"README.md":   []byte("ignored"),
		"cloudflared": binary,
	})
	installer := testInstaller(t, archive, true)
	if _, err := installer.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(installer.ExecutablePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("installed executable = %q", got)
	}
}

func TestInstallerRejectsSizeAndChecksumMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Installer)
	}{
		{name: "size", mutate: func(installer *Installer) { installer.artifact.Size++ }},
		{name: "checksum", mutate: func(installer *Installer) { installer.artifact.ArchiveSHA256 = string(make([]byte, 64)) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installer := testInstaller(t, []byte("asset"), false)
			tc.mutate(installer)
			if _, err := installer.Install(context.Background()); err == nil {
				t.Fatal("Install succeeded with an invalid pinned manifest")
			}
			if _, err := os.Stat(installer.ExecutablePath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published executable after failed verification: %v", err)
			}
		})
	}
}

func TestInstallerReportsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	installer := newInstaller(t.TempDir(), "plan9", "mips")
	status, err := installer.Status()
	if err != nil || status.Supported || status.Installed {
		t.Fatalf("Status = %+v, %v", status, err)
	}
	if _, err := installer.Install(context.Background()); !errors.Is(err, ErrInstallUnsupported) {
		t.Fatalf("Install error = %v, want ErrInstallUnsupported", err)
	}
}

func TestInstallerRejectsTamperedExecutable(t *testing.T) {
	t.Parallel()

	installer := testInstaller(t, []byte("original"), false)
	if _, err := installer.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer.ExecutablePath(), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed {
		t.Fatal("tampered executable remained installed")
	}
	if err := installer.VerifyExecutable(installer.ExecutablePath()); err == nil {
		t.Fatal("tampered executable passed verification")
	}
}

func TestInstallerRejectsConcurrentInstall(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "asset")
	}))
	defer server.Close()
	installer := newInstaller(t.TempDir(), "linux", "amd64")
	installer.artifact = managedArtifact{OS: "linux", Arch: "amd64", Name: "asset", URL: server.URL, Size: 5, ArchiveSHA256: digestHex([]byte("asset"))}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = installer.Install(context.Background())
	}()
	<-started
	if _, err := installer.Install(context.Background()); !errors.Is(err, ErrInstallInProgress) {
		t.Fatalf("concurrent Install error = %v, want ErrInstallInProgress", err)
	}
	close(release)
	wg.Wait()
}

func testInstaller(t *testing.T, body []byte, tgz bool) *Installer {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", stringInt64(int64(len(body))))
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	installer := newInstaller(t.TempDir(), "linux", "amd64")
	installer.artifact = managedArtifact{
		OS: "linux", Arch: "amd64", Name: "cloudflared-test", URL: server.URL,
		Size: int64(len(body)), ArchiveSHA256: digestHex(body), TGZ: tgz,
	}
	return installer
}

func makeTGZ(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func digestHex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func stringInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}

func TestInstallerPathStaysUnderRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	installer := newInstaller(root, "windows", "amd64")
	want := filepath.Join(root, ManagedCloudflaredVersion, "cloudflared.exe")
	if got := installer.ExecutablePath(); got != want {
		t.Fatalf("ExecutablePath = %q, want %q", got, want)
	}
}

func TestManagedArtifactManifest(t *testing.T) {
	t.Parallel()
	want := map[string]struct {
		name   string
		size   int64
		sha256 string
	}{
		"darwin/amd64":  {"cloudflared-darwin-amd64.tgz", 21741581, "0560c9ab7281ac3f746055323623ed23bc0405b6dab9400474020cba33a978da"},
		"darwin/arm64":  {"cloudflared-darwin-arm64.tgz", 19809074, "72edfd3eea463aef4d5cb89e2e209cecb048cc756c2b01915de2e0ad7cb39830"},
		"linux/amd64":   {"cloudflared-linux-amd64", 40129756, "d33ff2d14475178d2012c2c56beba87389ac5ded27649519f198a7d3134a99db"},
		"linux/arm64":   {"cloudflared-linux-arm64", 37687584, "e6422b9d4f72d3194bc5a38676f13667c06666523217b842a877d72a80b5ac08"},
		"windows/amd64": {"cloudflared-windows-amd64.exe", 55365048, "86aee4017b26625cee8484c113558f48effa4cd47f7aa05fcf425604e5d2b23c"},
	}
	if len(managedArtifacts) != len(want) {
		t.Fatalf("managed artifact count = %d, want %d", len(managedArtifacts), len(want))
	}
	for _, artifact := range managedArtifacts {
		expected, ok := want[artifact.OS+"/"+artifact.Arch]
		if !ok {
			t.Fatalf("unexpected artifact %+v", artifact)
		}
		if artifact.Name != expected.name || artifact.Size != expected.size || artifact.ArchiveSHA256 != expected.sha256 {
			t.Fatalf("artifact %s/%s = %+v, want %+v", artifact.OS, artifact.Arch, artifact, expected)
		}
	}
}
