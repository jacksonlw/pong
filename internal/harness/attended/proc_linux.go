package attended

import (
	"os"
	"strconv"
	"strings"
)

// parentOf returns the parent of process pid, or of this process for 0.
func parentOf(pid int) int {
	if pid == 0 {
		return os.Getppid()
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// The name is in parentheses and may hold spaces: the fields after it are fixed.
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 2 {
		return 0
	}
	ppid, _ := strconv.Atoi(f[1])
	return ppid
}

// argsOf returns process pid's arguments as the kernel holds them.
func argsOf(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}
