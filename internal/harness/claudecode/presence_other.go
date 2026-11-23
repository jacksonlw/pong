//go:build !darwin

package claudecode

import "time"

func inputIdle() (time.Duration, bool) { return 0, false }
