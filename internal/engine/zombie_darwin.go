package engine

import "golang.org/x/sys/unix"

// zombie reports whether pid has exited and waits for its parent to reap it.
func zombie(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && kp.Proc.P_stat == 5 // SZOMB
}
