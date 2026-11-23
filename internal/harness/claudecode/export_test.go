package claudecode

// Tests take the human to be away unless they say otherwise, so question and
// plan prompts wait for a Pong as the tests expect.
func init() { away = func() bool { return true } }

// SetAway makes the human away or at this computer, until the returned
// function restores it.
func SetAway(v bool) (restore func()) {
	old := away
	away = func() bool { return v }
	return func() { away = old }
}
