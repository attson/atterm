//go:build windows

package ptyhost

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenConPTYWindowsEcho(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	host, err := Open(ctx, Config{
		Argv: []string{"cmd.exe", "/d", "/q", "/k", "prompt $G"},
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer host.Close()

	if _, err := readUntilWindowsTestMarker(ctx, host, ">"); err != nil {
		t.Fatalf("wait for cmd prompt: %v", err)
	}
	if _, err := host.Write([]byte("echo atterm-conpty-ok\r\n")); err != nil {
		t.Fatalf("Write echo: %v", err)
	}

	out, err := readUntilWindowsTestMarker(ctx, host, "atterm-conpty-ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "atterm-conpty-ok") {
		t.Fatalf("output = %q; want echo text", out)
	}
	if _, err := host.Write([]byte("exit\r\n")); err != nil {
		t.Fatalf("Write exit: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- host.Wait() }()
	select {
	case <-ctx.Done():
		t.Fatalf("Wait timed out after echo output %q", out)
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
}

func readUntilWindowsTestMarker(ctx context.Context, host *Host, marker string) (string, error) {
	var out bytes.Buffer
	buf := make([]byte, 1024)
	for {
		readDone := make(chan struct {
			n   int
			err error
		}, 1)
		go func() {
			n, err := host.Read(buf)
			readDone <- struct {
				n   int
				err error
			}{n: n, err: err}
		}()
		select {
		case <-ctx.Done():
			return out.String(), ctx.Err()
		case result := <-readDone:
			if result.n > 0 {
				out.Write(buf[:result.n])
				if strings.Contains(out.String(), marker) {
					return out.String(), nil
				}
			}
			if result.err != nil {
				return out.String(), result.err
			}
		}
	}
}
