//go:build windows

package ptyhost

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestOpenConPTYWindowsEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	host, err := Open(ctx, Config{
		Argv: []string{"cmd.exe", "/c", "echo atterm-conpty-ok"},
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var out bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&out, host)
		readDone <- err
	}()

	if err := host.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	_ = host.Close()

	select {
	case <-ctx.Done():
		t.Fatalf("read timed out; output so far: %q", out.String())
	case err := <-readDone:
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "file already closed") {
			t.Fatalf("Read: %v", err)
		}
	}
	if !strings.Contains(out.String(), "atterm-conpty-ok") {
		t.Fatalf("output = %q; want echo text", out.String())
	}
}
