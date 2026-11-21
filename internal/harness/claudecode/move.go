package claudecode

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// A Session Pong starts for a human who works in the Claude desktop app
// begins with no terminal window, because only outside the app does it start
// on the prompt, and is then moved into the app with Claude Code's own
// /desktop command, which opens the Session in the app under the same id and
// ends the first copy. Nothing outside the Session can run /desktop, so
// Claude Code runs under a small expect script, in a terminal of its own
// that nobody sees, which types it (MoveNew, MoveResumed) once the Stop hook
// has marked the first turn as ended (MoveMarkEnv). Until then the human
// follows the Session through Pong: its Pings, its state, its Thread.
//
// /desktop cannot come sooner (tried on Claude Code 2.1.329, 2026-11-19):
// before the first prompt there is no transcript, so it moves nothing, and
// typed during a turn it is queued until that turn ends.
//
// The moved Session is a resumed one in the app. The Stop hook that waited
// on its finished Ping stayed behind with the first copy, so a listener
// starts with every resumed Session (Listen; hooks.json registers it on
// SessionStart for the source "resume") and carries the Pong or the text.
//
// Checked on Claude Code 2.1.329, 2026-11-19: /desktop typed into an idle
// Session put it in the Claude app's list under the same id and the first
// copy exited, from a terminal and from a hidden one alike; the app's copy
// fired SessionStart with source "resume"; an asyncRewake SessionStart hook
// that later exited 2 woke that Session in the app, and one woke a Session
// that had had no turn at all.
//
// The app does not always start its copy at once: in two of three runs it
// listed the Session and ran nothing (the human was at work in another
// Session of the app). Until the app runs it the Session is ended as far as
// Pong can tell, and a Pong or text for it opens it again, as for any ended
// Session, and moves it again.

// MoveMarkEnv names the file the wrapper watches. The Stop hook writes it
// when a turn has ended; the wrapper then types /desktop.
const MoveMarkEnv = "PONG_MOVE_MARK"

// The wrapper scripts, run with `expect -c`. They take the Session's id and
// the prompt from the environment, so nothing in them needs quoting, and
// hold no single quote, so the shell passes them as one word. With no
// terminal around it, the wrapper reads what Claude Code draws and throws it
// away, checking for the mark every second. It names the Session with
// /rename, from the title in the mark, then types /desktop, and waits
// a minute for the first copy to end, and ends it if it has not.
const (
	MoveNew     = moveSize + `spawn -noecho claude --session-id $env(PONG_MOVE_SESSION) -- $env(PONG_MOVE_PROMPT); ` + moveWhenMarked
	MoveResumed = moveSize + `spawn -noecho claude --resume $env(PONG_MOVE_SESSION) -- $env(PONG_MOVE_PROMPT); ` + moveWhenMarked

	moveSize       = `set stty_init "rows 50 cols 160"; set env(TERM) xterm-256color; `
	moveWhenMarked = `set mark $env(` + MoveMarkEnv + `); file delete $mark; ` +
		`proc move {} { global mark spawn_id; if {[file exists $mark]} { set f [open $mark]; set title [read $f]; close $f; file delete $mark; ` +
		`if {$title ne ""} { send "/rename $title\r"; expect -timeout 3 -re {zzzz} {} timeout {} }; send "/desktop\r"; ` +
		`expect -timeout 60 eof {} timeout { exec kill [exp_pid] }; exit } }; ` +
		`expect -timeout 1 -re {.+} { move; exp_continue } timeout { move; exp_continue } eof {}`
)

// moved is the hidden command that runs script for Session id on prompt.
func moved(script, id, prompt string) string {
	return MoveMarkEnv + `="$(mktemp -t pong-move)" PONG_MOVE_SESSION=` + shellQuote(id) +
		" PONG_MOVE_PROMPT=" + engine.ShellQuote(prompt) + " expect -c '" + script + "'"
}

// markTurnEnded tells a wrapper, if this Session runs under one, that its
// turn has ended. The mark holds the Session's title as Claude Code named
// it, for the wrapper to give the app: the app does not read a moved
// Session's title from its transcript, and names it after the folder.
//
// Before the mark, the Session is given the command that opens it in the
// app, which it had none of while it ran hidden: so a click in Pong reaches
// it there even when the app has not started its copy yet. Once the app
// does, its hooks give the app's own link, which replaces this one.
func markTurnEnded(c *client.Client, e Event) {
	mark := os.Getenv(MoveMarkEnv)
	if mark == "" {
		return
	}
	c.RegisterSession(engine.RegisterSession{Harness: HarnessName, ID: e.SessionID,
		AppCommand: DesktopResumeCommand(e.Cwd, e.SessionID), AppName: DesktopAppName})
	_, title := readTranscript(e.TranscriptPath)
	title = strings.Join(strings.Fields(title), " ")
	os.WriteFile(mark, []byte(title), 0o600)
}

// Listen sits beside a resumed Session for as long as it is idle, and wakes
// it with the Pong on its finished Ping or with text written in its Thread.
// It returns a Wake as its error, as the Stop hook does; nil wakes nothing.
func Listen(dir string, in io.Reader) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var e Event
	var s struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("reading hook event: %w", err)
	}
	json.Unmarshal(raw, &s)
	if e.HookEventName != "SessionStart" || s.Source != "resume" {
		return nil
	}
	c, err := client.Load(dir)
	if err != nil {
		return nil // the Engine is not running: nothing to listen to
	}
	// The Session's own SessionStart hook records it idle; this one may run first.
	var sess engine.Session
	for i := 0; i < 50; i++ {
		if sess, err = c.Session(HarnessName, e.SessionID); err == nil && sess.State != engine.SessionEnded {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || sess.State != engine.SessionIdle {
		return nil
	}
	if waits := waitingPings(c, e.SessionID); len(waits) > 0 {
		as, err := turn.WaitAll(c, waits)
		if err != nil {
			return err
		}
		if len(as) > 0 {
			_, err := wakeWith(c, e, turn.WakeText(as, answerText))
			return err
		}
	}
	texts, err := c.ListenText(HarnessName, e.SessionID)
	if err != nil || len(texts) == 0 {
		return err
	}
	_, err = wakeWith(c, e, handOver(texts))
	return err
}

// waitingPings lists, oldest first, the Session's waiting Pings: the questions
// its last turn ended on and its latest finished Ping.
func waitingPings(c *client.Client, session string) []engine.Ping {
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		return nil
	}
	var out []engine.Ping
	var finished *engine.Ping
	for i, p := range ps {
		if p.Session != session || (p.Harness != "" && p.Harness != HarnessName) || p.IsNote || p.Unasked {
			continue
		}
		if p.BlocksAgent {
			out = append(out, p)
		} else if finished == nil || p.CreatedAt.After(finished.CreatedAt) {
			finished = &ps[i]
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if finished != nil {
		out = append(out, *finished)
	}
	return out
}
