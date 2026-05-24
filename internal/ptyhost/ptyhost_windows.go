//go:build windows

// Package ptyhost is a thin, side-effect-free wrapper around a child process
// running under a pseudo-terminal. Windows uses ConPTY because creack/pty
// intentionally returns ErrUnsupported on this platform.
package ptyhost

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Config configures a PTY child process.
type Config struct {
	Argv []string // argv[0] is the binary, rest are args
	Env  []string // optional; nil means inherit os.Environ()
	Cwd  string   // optional working directory
	Cols uint16   // initial PTY cols (0 -> default 80)
	Rows uint16   // initial PTY rows (0 -> default 24)
}

// Host is a running PTY child.
type Host struct {
	process windows.Handle
	pid     int

	hpc windows.Handle
	in  *os.File
	out *os.File

	done      chan struct{}
	closeOnce sync.Once
	waitOnce  sync.Once
	waitErr   error
	exitCode  int
}

// Open launches the child under a Windows ConPTY and returns a Host. The
// caller owns the lifecycle: read until Read returns EOF, then call Wait and
// Close to release resources.
func Open(ctx context.Context, cfg Config) (*Host, error) {
	if len(cfg.Argv) == 0 {
		return nil, errors.New("ptyhost: empty argv")
	}
	cols, rows := cfg.Cols, cfg.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	inRead, inWrite, outRead, outWrite, err := newConPtyPipes()
	if err != nil {
		return nil, err
	}
	keepFiles := false
	inFile := os.NewFile(uintptr(inWrite), "atterm-conpty-input")
	outFile := os.NewFile(uintptr(outRead), "atterm-conpty-output")
	defer func() {
		if !keepFiles {
			_ = inFile.Close()
			_ = outFile.Close()
		}
	}()

	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)},
		inRead,
		outWrite,
		0,
		&hpc,
	); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(outWrite)
		return nil, err
	}
	// The pseudoconsole owns these pipe ends after creation; this process
	// keeps only the opposite ends exposed through Host.Read/Write.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)

	process, pid, err := startConPtyProcess(cfg, hpc)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		return nil, err
	}

	keepFiles = true
	h := &Host{
		process:  process,
		pid:      int(pid),
		hpc:      hpc,
		in:       inFile,
		out:      outFile,
		done:     make(chan struct{}),
		exitCode: -1,
	}
	go h.cancelOnContext(ctx)
	return h, nil
}

func newConPtyPipes() (inRead, inWrite, outRead, outWrite windows.Handle, err error) {
	if err = windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return 0, 0, 0, 0, err
	}
	if err = windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead)
		_ = windows.CloseHandle(inWrite)
		return 0, 0, 0, 0, err
	}
	return inRead, inWrite, outRead, outWrite, nil
}

func startConPtyProcess(cfg Config, hpc windows.Handle) (windows.Handle, uint32, error) {
	attr, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, 0, err
	}
	defer attr.Delete()
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE expects the HPCON handle value, not
	// a pointer to a Go variable holding that value.
	if err := attr.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		pseudoConsoleAttributeValue(hpc),
		unsafe.Sizeof(hpc),
	); err != nil {
		return 0, 0, err
	}

	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cfg.Argv))
	if err != nil {
		return 0, 0, err
	}
	var cwd *uint16
	if cfg.Cwd != "" {
		cwd, err = windows.UTF16PtrFromString(cfg.Cwd)
		if err != nil {
			return 0, 0, err
		}
	}
	env := cfg.Env
	if env == nil {
		env = os.Environ()
	}
	envBlock := makeWindowsEnvBlock(env)
	var envPtr *uint16
	if len(envBlock) > 0 {
		envPtr = &envBlock[0]
	}

	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
		},
		ProcThreadAttributeList: attr.List(),
	}
	pi := new(windows.ProcessInformation)
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(
		nil,
		cmdline,
		nil,
		nil,
		false,
		flags,
		envPtr,
		cwd,
		&si.StartupInfo,
		pi,
	); err != nil {
		return 0, 0, err
	}
	_ = windows.CloseHandle(pi.Thread)
	return pi.Process, pi.ProcessId, nil
}

func pseudoConsoleAttributeValue(hpc windows.Handle) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&hpc))
}

func makeWindowsEnvBlock(env []string) []uint16 {
	if len(env) == 0 {
		return nil
	}
	dedup := make(map[string]string, len(env))
	keys := make([]string, 0, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		key := kv[:i]
		folded := strings.ToUpper(key)
		if _, ok := dedup[folded]; !ok {
			keys = append(keys, folded)
		}
		dedup[folded] = kv
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(dedup[k])
		b.WriteByte(0)
	}
	b.WriteByte(0)
	return utf16.Encode([]rune(b.String()))
}

func (h *Host) cancelOnContext(ctx context.Context) {
	select {
	case <-ctx.Done():
		_ = h.Close()
	case <-h.done:
	}
}

// Read pulls bytes from the PTY master. Returns io.EOF when the ConPTY output
// pipe closes.
func (h *Host) Read(p []byte) (int, error) {
	n, err := h.out.Read(p)
	if errors.Is(err, os.ErrClosed) {
		return n, io.EOF
	}
	return n, err
}

// Write pushes bytes into the PTY master (i.e. as if the user typed them).
func (h *Host) Write(p []byte) (int, error) {
	n, err := h.in.Write(p)
	if errors.Is(err, os.ErrClosed) {
		return n, io.ErrClosedPipe
	}
	return n, err
}

// Resize changes the ConPTY window size.
func (h *Host) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	return windows.ResizePseudoConsole(h.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// Size reports the current PTY size. ConPTY has resize but no corresponding
// query API, so callers should track the last requested size themselves.
func (h *Host) Size() (cols, rows uint16, err error) {
	return 0, 0, syscall.EWINDOWS
}

// File returns the ConPTY output pipe. Prefer Read/Write where possible.
func (h *Host) File() *os.File { return h.out }

// Pid returns the child process id, or 0 if the process is not running.
func (h *Host) Pid() int { return h.pid }

// Cwd returns the child process's current working directory, or empty string
// when it cannot be determined on Windows.
func (h *Host) Cwd() string { return "" }

// Wait blocks until the child exits and returns the OS wait error, if any.
func (h *Host) Wait() error {
	h.waitOnce.Do(func() {
		_, h.waitErr = windows.WaitForSingleObject(h.process, windows.INFINITE)
		var code uint32
		if err := windows.GetExitCodeProcess(h.process, &code); err == nil {
			h.exitCode = int(code)
		}
		_ = windows.CloseHandle(h.process)
		close(h.done)
	})
	return h.waitErr
}

// ExitCode returns the child's exit status. Only meaningful after Wait.
func (h *Host) ExitCode() int { return h.exitCode }

// Close releases the ConPTY master side and terminates the child if it has not
// already exited. Safe to call multiple times.
func (h *Host) Close() error {
	h.closeOnce.Do(func() {
		_ = h.in.Close()
		_ = h.out.Close()
		windows.ClosePseudoConsole(h.hpc)
		_ = windows.TerminateProcess(h.process, 1)
	})
	return nil
}
