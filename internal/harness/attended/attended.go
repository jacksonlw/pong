// Package attended tells a run that has a human in the loop from one that has
// none. Pong fits into how a Harness already asks its human; it does not put a
// human into a run that never had one. A headless run (`claude -p`, `codex
// exec`) asks nobody, so Pong's hooks and tools stay out of it: no Session, no
// Pings, no tools offered to the agent. Pong starts no such run itself (ADR
// 0013).
package attended

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Harness returns the command line of the Harness process that ran this one:
// the nearest ancestor that is not a shell. Tests replace it.
var Harness = func() []string {
	_, args := harness()
	return args
}

// HarnessPID returns the id of that Harness process, or 0 when it cannot be
// read. The Engine ends the Session once the process is gone. Tests replace it.
var HarnessPID = func() int {
	pid, _ := harness()
	return pid
}

func harness() (int, []string) {
	pid := os.Getppid()
	for range 4 {
		out, err := exec.Command("ps", "-o", "ppid=,args=", "-p", strconv.Itoa(pid)).Output()
		f := strings.Fields(string(out))
		if err != nil || len(f) < 2 {
			return 0, nil
		}
		switch strings.TrimPrefix(filepath.Base(f[1]), "-") {
		case "sh", "bash", "zsh", "dash", "fish":
			if pid, err = strconv.Atoi(f[0]); err != nil {
				return 0, nil
			}
		default:
			return pid, f[1:]
		}
	}
	return 0, nil
}

// Unattended reports whether this process serves a headless run. When the
// Harness process cannot be read, the run counts as attended.
func Unattended() bool { return unattended(Harness()) }

func unattended(args []string) bool {
	head := args[:min(len(args), 3)]
	if slices.ContainsFunc(head, func(a string) bool { return strings.Contains(filepath.Base(a), "codex") }) {
		// The Codex app runs `codex app-server`; only `codex exec` is headless.
		return slices.Contains(head, "exec")
	}
	return slices.Contains(args, "-p") || slices.Contains(args, "--print")
}
