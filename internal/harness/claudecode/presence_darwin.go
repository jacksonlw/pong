package claudecode

import (
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// inputIdle is how long since the last keyboard or pointer input, from the
// HID system's idle time in nanoseconds.
func inputIdle() (time.Duration, bool) {
	out, err := exec.Command("/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return 0, false
	}
	m := hidIdle.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(ns), true
}
