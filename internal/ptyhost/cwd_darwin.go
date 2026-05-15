//go:build darwin

package ptyhost

/*
#include <libproc.h>
#include <sys/proc_info.h>
*/
import "C"

import "unsafe"

// readCwd resolves the child process's current working directory via the
// libproc API, which is the macOS equivalent of /proc/<pid>/cwd. Apple
// exposes proc_pidinfo with PROC_PIDVNODEPATHINFO precisely for this.
func readCwd(pid int) string {
	var info C.struct_proc_vnodepathinfo
	ret := C.proc_pidinfo(C.int(pid), C.PROC_PIDVNODEPATHINFO, 0,
		unsafe.Pointer(&info), C.int(unsafe.Sizeof(info)))
	if int(ret) < int(unsafe.Sizeof(info)) {
		return ""
	}
	return C.GoString(&info.pvi_cdir.vip_path[0])
}
