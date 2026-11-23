package claudecode

import "time"

// AwayAfter is how long this computer has had no keyboard or pointer input
// before Pong takes its human to be away from it. Only then does a question
// or plan prompt wait for a Pong; otherwise Claude Code shows its own dialog
// at once, as it would without Pong.
const AwayAfter = 5 * time.Minute

// away reports whether the human is away from this computer. Tests replace
// it; where the idle time cannot be read, they are taken to be away, which
// holds the prompt for a Pong as before.
var away = func() bool {
	idle, ok := inputIdle()
	return !ok || idle >= AwayAfter
}
