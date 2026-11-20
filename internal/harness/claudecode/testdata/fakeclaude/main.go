// Command fakeclaude stands in for Claude Code's `claude` in tests of the
// Sessions the Engine starts. It speaks the part of `claude` the Runner uses
// (`auth status --json`, and `-p` with --session-id or --resume and the
// prompt on standard input) and, during a -p run, plays Claude Code's side of
// Pong's hooks by running `pong hook claude-code` on recorded payloads.
//
// It is driven by its environment:
//
//	FAKE_CLAUDE_LOG       file it appends one JSON line to per run and per event
//	FAKE_CLAUDE_FIXTURES  the adapter's testdata folder (recorded payloads)
//	FAKE_CLAUDE_AUTH      "signed-out" to report not signed in
//	FAKE_CLAUDE_PLUGINS   what `plugin list --json` prints; unset, that command
//	                      fails like every other plugin command
//	FAKE_CLAUDE_STEPS    what a -p run does, comma-separated, in order:
//	                      permission  a PermissionRequest for Bash (permission/bash.json)
//	                      stop        end the turn: the Stop hook, carrying on with
//	                                  each wake (exit 2) as the next turn, as
//	                                  Claude Code 2.1.322 does in -p mode (which
//	                                  fires no UserPromptSubmit for that turn)
//	                      held        wait until the file FAKE_CLAUDE_RELEASE exists
//	                      hang        wait until stopped
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 2 && args[0] == "auth" && args[1] == "status" {
		name := "signed-in.json"
		if os.Getenv("FAKE_CLAUDE_AUTH") == "signed-out" {
			name = "signed-out.json"
		}
		b, _ := os.ReadFile(filepath.Join(os.Getenv("FAKE_CLAUDE_FIXTURES"), "auth", name))
		os.Stdout.Write(b)
		if name == "signed-out.json" {
			os.Exit(1)
		}
		return
	}
	if list, ok := os.LookupEnv("FAKE_CLAUDE_PLUGINS"); ok && len(args) >= 2 && args[0] == "plugin" && args[1] == "list" {
		fmt.Println(list)
		return
	}
	if len(args) == 0 || args[0] != "-p" {
		fmt.Fprintln(os.Stderr, "fakeclaude: only auth status, plugin list and -p are faked")
		os.Exit(2)
	}
	var id string
	for i, a := range args {
		if (a == "--session-id" || a == "--resume") && i+1 < len(args) {
			id = args[i+1]
		}
	}
	prompt, _ := io.ReadAll(os.Stdin)
	cwd, _ := os.Getwd()
	logLine(map[string]any{"args": args, "cwd": cwd, "prompt": string(prompt), "pong_session": os.Getenv("PONG_SESSION")})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		logLine(map[string]any{"event": "terminated", "session": id})
		// Claude Code stops the hooks still running when it is stopped.
		if c := running; c != nil && c.Process != nil {
			c.Process.Signal(syscall.SIGTERM)
		}
		os.Exit(143)
	}()
	for _, step := range strings.Split(os.Getenv("FAKE_CLAUDE_STEPS"), ",") {
		switch step {
		case "permission":
			out, _, _ := hook("permission/bash.json", map[string]any{"session_id": id, "cwd": cwd})
			logLine(map[string]any{"event": "permission", "hook_output": out})
		case "stop":
			active := false
			last := "Done: " + strings.TrimSpace(string(prompt))
			for {
				_, wake, code := hook("stop/first.json", map[string]any{"session_id": id, "cwd": cwd,
					"stop_hook_active": active, "last_assistant_message": last})
				if code != 2 {
					break
				}
				logLine(map[string]any{"event": "woken", "session": id, "prompt": wake})
				active, last = true, "Done: "+wake
			}
		case "held":
			for {
				if _, err := os.Stat(os.Getenv("FAKE_CLAUDE_RELEASE")); err == nil {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		case "hang":
			select {}
		}
	}
	fmt.Println("Done")
}

// hook runs `pong hook claude-code` on a recorded payload with fields
// replaced, and returns its standard output, standard error and exit code.
func hook(fixture string, set map[string]any) (stdout, stderr string, code int) {
	b, err := os.ReadFile(filepath.Join(os.Getenv("FAKE_CLAUDE_FIXTURES"), fixture))
	if err != nil {
		panic(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		panic(err)
	}
	payload["transcript_path"] = ""
	for k, v := range set {
		payload[k] = v
	}
	in, _ := json.Marshal(payload)
	cmd := exec.Command("pong", "hook", "claude-code")
	cmd.Stdin = bytes.NewReader(in)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	running = cmd
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		panic(err)
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(errb.String()), code
}

// running is the hook being run, if any.
var running *exec.Cmd

func logLine(v map[string]any) {
	f, err := os.OpenFile(os.Getenv("FAKE_CLAUDE_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(v)
	f.Write(append(b, '\n'))
}
