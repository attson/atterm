//go:build linux

package trash

import (
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestSendLinuxPrefersGio(t *testing.T) {
	oldLookPath := lookPath
	oldExec := execCommand
	var got []string
	lookPath = func(bin string) (string, error) {
		if bin == "gio" {
			return "/usr/bin/gio", nil
		}
		return "", errors.New("not found")
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	t.Cleanup(func() {
		lookPath = oldLookPath
		execCommand = oldExec
	})
	if err := Send("/tmp/x.txt"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	want := []string{"gio", "trash", "/tmp/x.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exec args = %v, want %v", got, want)
	}
}

func TestSendLinuxFallsBackToKioclient5(t *testing.T) {
	oldLookPath := lookPath
	oldExec := execCommand
	var got []string
	lookPath = func(bin string) (string, error) {
		if bin == "kioclient5" {
			return "/usr/bin/kioclient5", nil
		}
		return "", errors.New("not found")
	}
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return exec.Command("true")
	}
	t.Cleanup(func() {
		lookPath = oldLookPath
		execCommand = oldExec
	})
	if err := Send("/tmp/x.txt"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	want := []string{"kioclient5", "move", "/tmp/x.txt", "trash:/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exec args = %v, want %v", got, want)
	}
}

func TestSendLinuxUnavailable(t *testing.T) {
	oldLookPath := lookPath
	lookPath = func(bin string) (string, error) { return "", errors.New("no") }
	t.Cleanup(func() { lookPath = oldLookPath })
	if err := Send("/tmp/x.txt"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
