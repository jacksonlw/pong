package engine_test

import "golang.org/x/sys/unix"

func isZombie(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && kp.Proc.P_stat == 5 // SZOMB
}
