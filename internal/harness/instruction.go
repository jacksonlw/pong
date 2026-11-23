package harness

import (
	"os"
	"strings"
)

// RefreshInstruction replaces the block old that Pong added to the
// instruction file at path with new, keeping the rest of the file and its
// mode. It reports whether it replaced it: not when the file no longer holds
// old, as when the human edited Pong's block by hand.
func RefreshInstruction(path, old, new string) (bool, error) {
	if old == new {
		return false, nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), old) {
		return false, err
	}
	return true, os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), fi.Mode().Perm())
}
