//go:build windows

package quicktunnel

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminateProcess(cmd *exec.Cmd) error {
	return taskkill(cmd, false)
}

func killProcess(cmd *exec.Cmd) error {
	return taskkill(cmd, true)
}

func taskkill(cmd *exec.Cmd, force bool) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	args := []string{"/PID", strconv.Itoa(cmd.Process.Pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	return exec.Command("taskkill", args...).Run()
}
