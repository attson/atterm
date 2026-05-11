package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		die("usage: go run .github/scripts/sign-release-checksums.go <artifact-dir>")
	}
	dir := os.Args[1]
	key := strings.TrimSpace(os.Getenv("ATTERM_UPDATE_SIGNING_PRIVATE_KEY"))
	if key == "" {
		die("ATTERM_UPDATE_SIGNING_PRIVATE_KEY is required")
	}
	privBytes, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		die("decode signing key: %v", err)
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		die("signing key has %d bytes; want %d", len(privBytes), ed25519.PrivateKeySize)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		die("read artifact dir: %v", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "SHA256SUMS" || name == "SHA256SUMS.sig" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		die("no artifacts to sign in %s", dir)
	}

	var sums strings.Builder
	for _, name := range names {
		sum, err := sha256File(filepath.Join(dir, name))
		if err != nil {
			die("hash %s: %v", name, err)
		}
		fmt.Fprintf(&sums, "%s  %s\n", sum, name)
	}
	sumsBytes := []byte(sums.String())
	sig := ed25519.Sign(ed25519.PrivateKey(privBytes), sumsBytes)

	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), sumsBytes, 0o644); err != nil {
		die("write SHA256SUMS: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS.sig"), sig, 0o644); err != nil {
		die("write SHA256SUMS.sig: %v", err)
	}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
