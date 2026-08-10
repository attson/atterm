//go:build windows

package main

import (
	"context"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

var (
	modUser32          = syscall.NewLazyDLL("user32.dll")
	procFindWindowW    = modUser32.NewProc("FindWindowW")
	procGetWindowLongW = modUser32.NewProc("GetWindowLongW")
	procSetWindowLongW = modUser32.NewProc("SetWindowLongW")
)

const (
	wsExToolWindow  = 0x00000080
	wsExAppWindow   = 0x00040000
	petWindowsTitle = "AT Term Pet"
)

// gwlExStyle is GWL_EXSTYLE. It must be a typed variable rather than an
// untyped constant: uintptr(-20) is a compile-time overflow, while converting
// a negative int32 value at runtime wraps as the Win32 API expects.
var gwlExStyle int32 = -20

// applyPetPlatformOptions configures the companion window for Windows.
func applyPetPlatformOptions(opts *options.App) {
	opts.Windows = &windows.Options{
		WebviewIsTransparent: true,
		WindowIsTranslucent:  true,
		DisableWindowIcon:    true,
	}
}

// applyPetPostStartup runs from OnStartup, once the window exists.
func applyPetPostStartup(_ context.Context) {
	// Even at OnStartup the HWND may not be registered yet, so poll briefly
	// for it by title before swapping the extended style.
	go stripPetFromTaskbar()
}

// stripPetFromTaskbar marks the pet window as a tool window so it stays out of
// the taskbar and out of Alt-Tab. Wails v2 has no option for this, so it is
// applied directly via SetWindowLong once the HWND exists.
//
// Best-effort: if the window is never found the pet still works, it just also
// shows up in Alt-Tab.
func stripPetFromTaskbar() {
	title, err := syscall.UTF16PtrFromString(petWindowsTitle)
	if err != nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd != 0 {
			style, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
			style = (style | uintptr(wsExToolWindow)) &^ uintptr(wsExAppWindow)
			procSetWindowLongW.Call(hwnd, uintptr(gwlExStyle), style)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
