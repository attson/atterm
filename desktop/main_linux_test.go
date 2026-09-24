//go:build linux

package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

func TestLinuxPlatformOptionsSetWindowIdentityWithoutChangingGPUPolicy(t *testing.T) {
	opts := platformOptions()
	if opts.Linux == nil {
		t.Fatal("Linux options are nil")
	}
	if len(opts.Linux.Icon) == 0 {
		t.Fatal("Linux window icon is empty")
	}
	if opts.Linux.ProgramName != "AT-Term" {
		t.Fatalf("ProgramName = %q; want AT-Term", opts.Linux.ProgramName)
	}
	if opts.Linux.WebviewGpuPolicy != linux.WebviewGpuPolicyNever {
		t.Fatalf("WebviewGpuPolicy = %v; want Never", opts.Linux.WebviewGpuPolicy)
	}
}
