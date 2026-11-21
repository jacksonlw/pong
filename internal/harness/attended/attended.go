// Package attended tells a run that has a human in the loop from one that has
// none. Pong fits into how a Harness already asks its human; it does not put a
// human into a run that never had one. A headless run (`claude -p`, `codex
// exec`) asks nobody, so Pong's hooks and tools stay out of it: no Session, no
// Pings, no tools offered to the agent. Pong starts no such run itself (ADR
// 0013).
package attended

import (
	"path/filepath"
	"slices"
	"strings"
)

// Harness returns the command line of the Harness process that ran this one:
// the nearest ancestor that is not a shell, its arguments exactly as the
// system holds them. Tests replace it.
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
	pid := parentOf(0)
	for range 4 {
		if pid <= 0 {
			return 0, nil
		}
		args := argsOf(pid)
		if len(args) == 0 {
			return 0, nil
		}
		switch strings.TrimPrefix(filepath.Base(args[0]), "-") {
		case "sh", "bash", "zsh", "dash", "fish":
			pid = parentOf(pid)
		default:
			return pid, args
		}
	}
	return 0, nil
}

// Unattended reports whether this process serves a headless run. When the
// Harness process cannot be read, the run counts as attended.
func Unattended() bool { return unattended(Harness()) }

// unattended reads a Harness's arguments as the system holds them, so a
// prompt is one argument however many words it has. Only flags count: the
// scan stops at "--", after which Pong and the human put the prompt.
func unattended(args []string) bool {
	if i := slices.Index(args, "--"); i >= 0 {
		args = args[:i]
	}
	head := args[:min(len(args), 3)]
	if slices.ContainsFunc(head, func(a string) bool { return strings.Contains(filepath.Base(a), "codex") }) {
		// The Codex app runs `codex app-server`; only `codex exec` is headless.
		return slices.Contains(head, "exec")
	}
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "-p" || a == "--print" || strings.HasPrefix(a, "--print=")
	})
}
