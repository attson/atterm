package quicktunnel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// ManagedCloudflaredVersion is intentionally pinned. Updating it requires
	// reviewing Cloudflare's release assets and changing every digest below.
	ManagedCloudflaredVersion = "2026.10.0"
	managedDownloadLimit      = 64 << 20
	managedMetadataVersion    = 1
)

var (
	ErrInstallUnsupported = errors.New("cloudflared managed install is unsupported on this platform")
	ErrInstallInProgress  = errors.New("cloudflared managed install is already in progress")
)

type managedArtifact struct {
	OS            string
	Arch          string
	Name          string
	ArchiveSHA256 string
	Size          int64
	TGZ           bool
	URL           string
}

var managedArtifacts = []managedArtifact{
	{OS: "darwin", Arch: "amd64", Name: "cloudflared-darwin-amd64.tgz", ArchiveSHA256: "0560c9ab7281ac3f746055323623ed23bc0405b6dab9400474020cba33a978da", Size: 21741581, TGZ: true},
	{OS: "darwin", Arch: "arm64", Name: "cloudflared-darwin-arm64.tgz", ArchiveSHA256: "72edfd3eea463aef4d5cb89e2e209cecb048cc756c2b01915de2e0ad7cb39830", Size: 19809074, TGZ: true},
	{OS: "linux", Arch: "amd64", Name: "cloudflared-linux-amd64", ArchiveSHA256: "d33ff2d14475178d2012c2c56beba87389ac5ded27649519f198a7d3134a99db", Size: 40129756},
	{OS: "linux", Arch: "arm64", Name: "cloudflared-linux-arm64", ArchiveSHA256: "e6422b9d4f72d3194bc5a38676f13667c06666523217b842a877d72a80b5ac08", Size: 37687584},
	{OS: "windows", Arch: "amd64", Name: "cloudflared-windows-amd64.exe", ArchiveSHA256: "86aee4017b26625cee8484c113558f48effa4cd47f7aa05fcf425604e5d2b23c", Size: 55365048},
}

type managedInstallMetadata struct {
	V                int    `json:"v"`
	Version          string `json:"version"`
	AssetName        string `json:"asset_name"`
	ArchiveSHA256    string `json:"archive_sha256"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

// InstallStatus is safe to expose to Settings. Executable stays Go-owned so
// the renderer never chooses a process path.
type InstallStatus struct {
	Supported bool   `json:"supported"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	AssetName string `json:"asset_name,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

// Installer performs only explicit, pinned installs into an app-owned root.
type Installer struct {
	root     string
	client   *http.Client
	artifact managedArtifact

	mu         sync.Mutex
	installing bool
}

// NewInstaller selects the pinned artifact for the current runtime.
func NewInstaller(root string) *Installer {
	return newInstaller(root, runtime.GOOS, runtime.GOARCH)
}

func newInstaller(root, goos, goarch string) *Installer {
	installer := &Installer{root: root, client: &http.Client{Timeout: 2 * time.Minute}}
	for _, artifact := range managedArtifacts {
		if artifact.OS == goos && artifact.Arch == goarch {
			if artifact.URL == "" {
				artifact.URL = "https://github.com/cloudflare/cloudflared/releases/download/" + ManagedCloudflaredVersion + "/" + artifact.Name
			}
			installer.artifact = artifact
			break
		}
	}
	return installer
}

// Status verifies both install metadata and executable bytes. A partial or
// modified install is reported as absent and is never selected for execution.
func (i *Installer) Status() (InstallStatus, error) {
	status := InstallStatus{Supported: i != nil && i.artifact.Name != "", Version: ManagedCloudflaredVersion}
	if !status.Supported {
		return status, nil
	}
	status.AssetName = i.artifact.Name
	status.SHA256 = i.artifact.ArchiveSHA256
	metadata, err := i.readMetadata()
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return status, nil
	}
	if metadata.V != managedMetadataVersion || metadata.Version != ManagedCloudflaredVersion ||
		metadata.AssetName != i.artifact.Name || metadata.ArchiveSHA256 != i.artifact.ArchiveSHA256 ||
		len(metadata.ExecutableSHA256) != sha256.Size*2 {
		return status, nil
	}
	digest, err := fileSHA256(i.ExecutablePath())
	if err != nil || digest != metadata.ExecutableSHA256 {
		return status, nil
	}
	status.Installed = true
	return status, nil
}

// ExecutablePath returns the deterministic app-owned location. Callers must
// still call VerifyExecutable before starting a process.
func (i *Installer) ExecutablePath() string {
	name := "cloudflared"
	if i != nil && i.artifact.OS == "windows" {
		name += ".exe"
	}
	return filepath.Join(i.root, ManagedCloudflaredVersion, name)
}

// VerifyExecutable rejects missing, partial, or locally modified managed
// binaries. It is suitable for Manager.Config.VerifyExecutable.
func (i *Installer) VerifyExecutable(path string) error {
	if i == nil || path != i.ExecutablePath() {
		return errors.New("cloudflared managed executable path mismatch")
	}
	status, err := i.Status()
	if err != nil {
		return err
	}
	if !status.Installed {
		return errors.New("cloudflared managed executable is not verified")
	}
	return nil
}

// Install downloads only the pinned asset and publishes it after checksum
// verification. No call site invokes this method implicitly from Start.
func (i *Installer) Install(ctx context.Context) (InstallStatus, error) {
	if i == nil || i.artifact.Name == "" {
		return InstallStatus{Version: ManagedCloudflaredVersion}, ErrInstallUnsupported
	}
	i.mu.Lock()
	if i.installing {
		i.mu.Unlock()
		return InstallStatus{}, ErrInstallInProgress
	}
	i.installing = true
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		i.installing = false
		i.mu.Unlock()
	}()

	if status, _ := i.Status(); status.Installed {
		return status, nil
	}
	versionDir := filepath.Dir(i.ExecutablePath())
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		return InstallStatus{}, fmt.Errorf("create cloudflared install directory: %w", err)
	}
	archive, err := os.CreateTemp(versionDir, ".cloudflared-download-*")
	if err != nil {
		return InstallStatus{}, err
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	if err := i.download(ctx, archive); err != nil {
		_ = archive.Close()
		return InstallStatus{}, err
	}
	if err := archive.Close(); err != nil {
		return InstallStatus{}, err
	}
	if digest, err := fileSHA256(archivePath); err != nil || digest != i.artifact.ArchiveSHA256 {
		return InstallStatus{}, errors.New("cloudflared asset checksum verification failed")
	}

	executableTemp, err := os.CreateTemp(versionDir, ".cloudflared-executable-*")
	if err != nil {
		return InstallStatus{}, err
	}
	executableTempPath := executableTemp.Name()
	defer os.Remove(executableTempPath)
	if err := i.writeExecutable(archivePath, executableTemp); err != nil {
		_ = executableTemp.Close()
		return InstallStatus{}, err
	}
	if err := executableTemp.Sync(); err != nil {
		_ = executableTemp.Close()
		return InstallStatus{}, err
	}
	if err := executableTemp.Close(); err != nil {
		return InstallStatus{}, err
	}
	if err := os.Chmod(executableTempPath, 0o700); err != nil {
		return InstallStatus{}, err
	}
	executableDigest, err := fileSHA256(executableTempPath)
	if err != nil {
		return InstallStatus{}, err
	}
	metadata := managedInstallMetadata{
		V: managedMetadataVersion, Version: ManagedCloudflaredVersion, AssetName: i.artifact.Name,
		ArchiveSHA256: i.artifact.ArchiveSHA256, ExecutableSHA256: executableDigest,
	}
	metadataRaw, err := json.Marshal(metadata)
	if err != nil {
		return InstallStatus{}, err
	}
	metadataTemp, err := os.CreateTemp(versionDir, ".cloudflared-metadata-*")
	if err != nil {
		return InstallStatus{}, err
	}
	metadataTempPath := metadataTemp.Name()
	defer os.Remove(metadataTempPath)
	if err := metadataTemp.Chmod(0o600); err != nil {
		_ = metadataTemp.Close()
		return InstallStatus{}, err
	}
	if _, err := metadataTemp.Write(metadataRaw); err != nil {
		_ = metadataTemp.Close()
		return InstallStatus{}, err
	}
	if err := metadataTemp.Sync(); err != nil {
		_ = metadataTemp.Close()
		return InstallStatus{}, err
	}
	if err := metadataTemp.Close(); err != nil {
		return InstallStatus{}, err
	}
	_ = os.Remove(i.ExecutablePath())
	if err := os.Rename(executableTempPath, i.ExecutablePath()); err != nil {
		return InstallStatus{}, err
	}
	_ = os.Remove(i.metadataPath())
	if err := os.Rename(metadataTempPath, i.metadataPath()); err != nil {
		_ = os.Remove(i.ExecutablePath())
		return InstallStatus{}, err
	}
	status, err := i.Status()
	if err != nil || !status.Installed {
		return InstallStatus{}, errors.New("cloudflared managed install verification failed")
	}
	return status, nil
}

func (i *Installer) download(ctx context.Context, dst *os.File) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, i.artifact.URL, nil)
	if err != nil {
		return err
	}
	response, err := i.client.Do(request)
	if err != nil {
		return fmt.Errorf("download cloudflared: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download cloudflared: HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != i.artifact.Size {
		return errors.New("cloudflared asset size does not match pinned manifest")
	}
	written, err := io.Copy(dst, io.LimitReader(response.Body, managedDownloadLimit+1))
	if err != nil {
		return fmt.Errorf("download cloudflared: %w", err)
	}
	if written != i.artifact.Size || written > managedDownloadLimit {
		return errors.New("cloudflared asset size does not match pinned manifest")
	}
	return dst.Sync()
}

func (i *Installer) writeExecutable(archivePath string, dst *os.File) error {
	if !i.artifact.TGZ {
		source, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer source.Close()
		_, err = io.Copy(dst, io.LimitReader(source, managedDownloadLimit+1))
		return err
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return errors.New("cloudflared archive is invalid")
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("cloudflared archive is invalid")
		}
		if strings.TrimPrefix(filepath.ToSlash(header.Name), "./") != "cloudflared" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > managedDownloadLimit {
			return errors.New("cloudflared archive has an invalid executable entry")
		}
		if written, err := io.CopyN(dst, reader, header.Size); err != nil || written != header.Size {
			return errors.New("cloudflared archive executable is truncated")
		}
		found = true
	}
	if !found {
		return errors.New("cloudflared archive is missing its executable")
	}
	return nil
}

func (i *Installer) readMetadata() (managedInstallMetadata, error) {
	raw, err := os.ReadFile(i.metadataPath())
	if err != nil {
		return managedInstallMetadata{}, err
	}
	var metadata managedInstallMetadata
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return managedInstallMetadata{}, err
	}
	return metadata, nil
}

func (i *Installer) metadataPath() string {
	return filepath.Join(i.root, ManagedCloudflaredVersion, "install.json")
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
