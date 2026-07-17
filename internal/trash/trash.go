// Package trash moves paths to the OS trash / recycle bin. Callers must not
// assume Send removed the file when ErrUnavailable is returned; the intended
// UI is to prompt the user for a hard-delete fallback in that case.
package trash

import (
	"errors"
	"os/exec"
)

// ErrUnavailable is returned when the host has no supported trash command.
var ErrUnavailable = errors.New("trash: no platform trash command available")

// execCommand and lookPath are package-level indirections so tests can stub
// out the shell-outs without spawning real processes.
var (
	execCommand = exec.Command
	lookPath    = exec.LookPath
)

// Send moves path to the OS trash.
func Send(path string) error {
	return sendPlatform(path)
}
