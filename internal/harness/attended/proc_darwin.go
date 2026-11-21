package attended

import (
	"bytes"
	"encoding/binary"
	"os"

	"golang.org/x/sys/unix"
)

// parentOf returns the parent of process pid, or of this process for 0.
func parentOf(pid int) int {
	if pid == 0 {
		return os.Getppid()
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0
	}
	return int(kp.Eproc.Ppid)
}

// argsOf returns process pid's arguments as the kernel holds them
// (kern.procargs2: argc, the executable's path, padding, then argv).
func argsOf(pid int) []string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(b) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	i := bytes.IndexByte(b, 0) // the executable's path
	if i < 0 {
		return nil
	}
	b = b[i:]
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	args := make([]string, 0, argc)
	for len(args) < argc && len(b) > 0 {
		j := bytes.IndexByte(b, 0)
		if j < 0 {
			j = len(b)
		}
		args = append(args, string(b[:j]))
		b = b[min(j+1, len(b)):]
	}
	return args
}
