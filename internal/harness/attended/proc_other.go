//go:build !darwin && !linux

package attended

// On other systems the Harness cannot be read, and every run counts as attended.
func parentOf(int) int    { return 0 }
func argsOf(int) []string { return nil }
