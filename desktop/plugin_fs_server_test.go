package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func encodePath(p string) string {
	return base64.URLEncoding.EncodeToString([]byte(p))
}

func TestServeHTTP_ReturnsFileBytes(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(resolved), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "hello world" {
		t.Errorf("body=%q", rr.Body.String())
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control=%q want no-store", got)
	}
}

func TestServeHTTP_RejectsNonGet(t *testing.T) {
	fs, _ := makeFS(t)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(m, "/pluginfs/"+encodePath("/whatever"), nil)
		rr := httptest.NewRecorder()
		fs.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status=%d want 405", m, rr.Code)
		}
	}
}

func TestServeHTTP_HeadIsAllowed(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "hello.txt")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodHead, "/pluginfs/"+encodePath(resolved), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("HEAD should have empty body, got %d bytes", rr.Body.Len())
	}
}

func TestServeHTTP_RejectsBadPrefix(t *testing.T) {
	fs, _ := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/anything-else", nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestServeHTTP_RejectsBadBase64(t *testing.T) {
	fs, _ := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/!!!not-base64!!!", nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rr.Code)
	}
}

func TestServeHTTP_RejectsOutsideRoot(t *testing.T) {
	fs, _ := makeFS(t)
	outside := t.TempDir()
	path := filepath.Join(outside, "leak.txt")
	_ = os.WriteFile(path, []byte("nope"), 0o644)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(path), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}

func TestServeHTTP_RejectsDenyPattern(t *testing.T) {
	fs, home := makeFS(t)
	ssh := filepath.Join(home, ".ssh")
	_ = os.Mkdir(ssh, 0o700)
	keyPath := filepath.Join(ssh, "id_rsa")
	_ = os.WriteFile(keyPath, []byte("secret"), 0o600)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(keyPath), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}

func TestServeHTTP_RejectsMissingFile(t *testing.T) {
	fs, home := makeFS(t)
	home, _ = filepath.EvalSymlinks(home)
	missing := filepath.Join(home, "nope.txt")
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(missing), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d want 404", rr.Code)
	}
}

func TestServeHTTP_SupportsRange(t *testing.T) {
	fs, home := makeFS(t)
	path := filepath.Join(home, "big.bin")
	body := strings.Repeat("ABCDEFGHIJ", 100) // 1000 bytes
	_ = os.WriteFile(path, []byte(body), 0o644)
	resolved, _ := filepath.EvalSymlinks(path)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(resolved), nil)
	req.Header.Set("Range", "bytes=10-19")
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("status=%d want 206", rr.Code)
	}
	if rr.Body.String() != "ABCDEFGHIJ" {
		t.Errorf("body=%q", rr.Body.String())
	}
}

func TestServeHTTP_RejectsDirectory(t *testing.T) {
	fs, home := makeFS(t)
	req := httptest.NewRequest(http.MethodGet, "/pluginfs/"+encodePath(home), nil)
	rr := httptest.NewRecorder()
	fs.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}
