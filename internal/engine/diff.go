package engine

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// maxDiff is the most of a diff attached whole; a longer one is attached as
// its summary of files changed.
const maxDiff = 200 << 10

// uncommittedDiff is the uncommitted change to tracked files in folder, for
// a finished Ping: agents rarely attach their change, so without it the human
// sees finished work with nothing of what changed. It is "" when folder is
// not in a git repository, has no change, or git does not answer in time.
func uncommittedDiff(folder string) string {
	if folder == "" {
		return ""
	}
	// "@" is git's name for the last commit.
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", folder, "diff", "--no-color", "--no-ext-diff", "@"}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimRight(string(out), "\n")
	}
	if d := run(); len(d) <= maxDiff {
		return d
	}
	return run("--stat")
}
