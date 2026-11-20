package pongcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jacksonlw/pong/internal/harness/attended"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
	"github.com/jacksonlw/pong/internal/harness/codex"
)

// hook handles one hook call from a Harness: `pong hook claude-code` reads the
// event on standard input and writes the Harness's answer on standard output.
// It exits 0 whatever happens (except 2 to wake a Session), so an unreachable Engine lets the Harness
// proceed on its own; what went wrong goes to standard error.
func hook(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir := flags("hook", stderr)
	listen := fs.Bool("listen", false, "sit beside a resumed Claude Code Session and wake it with a Pong or text")
	afterStop := fs.Bool("after-stop", false, "run in the background beside a Claude Code tool call and, if the turn is stopped there, wake the Session with text")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(stderr, "usage: pong hook claude-code|codex [-dir DIR] [-listen|-after-stop] < event.json")
		return 2
	}
	args = pos
	var run func(string, io.Reader, io.Writer) error
	var withdraw func()
	switch args[0] {
	case "claude-code":
		run, withdraw = claudecode.Hook, claudecode.WithdrawWaiting
		if *listen {
			run = func(dir string, in io.Reader, _ io.Writer) error { return claudecode.Listen(dir, in) }
		}
		if *afterStop {
			run = func(dir string, in io.Reader, _ io.Writer) error { return claudecode.ListenAfterStop(dir, in) }
		}
	case "codex":
		run, withdraw = codex.Hook, codex.WithdrawWaiting
	default:
		fmt.Fprintf(stderr, "pong hook: unknown Harness %q\n", args[0])
		return 2
	}
	// A headless run nobody attends keeps running as it would without Pong.
	if attended.Unattended() {
		return 0
	}
	// The Harness stops a hook it no longer waits for; withdraw its Pings so
	// they do not stay waiting.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		if _, ok := <-sig; ok {
			withdraw()
			os.Exit(0)
		}
	}()
	err = run(*dir, stdin, stdout)
	var wake claudecode.Wake
	if errors.As(err, &wake) {
		// Claude Code wakes the Session with standard error on exit 2.
		fmt.Fprintln(stderr, wake.Text)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "pong hook:", err)
	}
	return 0
}
