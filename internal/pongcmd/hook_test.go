package pongcmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
)

func TestHookWithNoEngineLetsClaudeCodeProceed(t *testing.T) {
	in, err := os.ReadFile("../../internal/harness/claudecode/testdata/question/single.json")
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := hook([]string{"claude-code", "-dir", t.TempDir()}, bytes.NewReader(in), &out, &errs); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, wrote %q; want exit 0 and nothing", code, out.String())
	}
}

// When Claude Code stops the hook (Esc, or its timeout), the Ping it was
// waiting on is withdrawn instead of staying waiting.
func TestStoppedHookWithdrawsItsPing(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "pong")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/pong").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, s := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(s.String(), func(t *testing.T) {
			c, dir := enginetest.Start(t)
			cmd := exec.Command(bin, "hook", "claude-code", "-dir", dir)
			in, _ := os.Open("../../internal/harness/claudecode/testdata/permission/bash.json")
			defer in.Close()
			cmd.Stdin = in
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var id string
			for deadline := time.Now().Add(5 * time.Second); id == "" && time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
				if ps, _ := c.Pings(engine.StateWaiting); len(ps) == 1 {
					id = ps[0].ID
				}
			}
			if id == "" {
				cmd.Process.Kill()
				t.Fatal("no Ping arrived")
			}
			cmd.Process.Signal(s)
			if err := cmd.Wait(); err != nil {
				t.Fatalf("hook exited with %v", err)
			}
			if p, err := c.Ping(id); err != nil || p.State != engine.StateWithdrawn {
				t.Fatalf("Ping after stop = %+v, %v", p, err)
			}
		})
	}
}

// A headless run the Engine did not start has no human in the loop, and Pong
// puts none in: its hook sends nothing and its tools are not offered.
func TestUnattendedRunGetsNoPingsAndNoTools(t *testing.T) {
	was := attended.Harness
	attended.Harness = func() []string { return []string{"claude", "-p", "--model", "haiku"} }
	defer func() { attended.Harness = was }()

	c, dir := enginetest.Start(t)
	in, err := os.ReadFile("../../internal/harness/claudecode/testdata/question/single.json")
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := hook([]string{"claude-code", "-dir", dir}, bytes.NewReader(in), &out, &errs); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, wrote %q; want exit 0 and nothing", code, out.String())
	}
	if ps, _ := c.Pings(engine.StateWaiting); len(ps) != 0 {
		t.Fatalf("%d Pings from a run with no human", len(ps))
	}

	msgs := request("initialize", nil)
	msgs = append(append(msgs, '\n'), request("tools/list", nil)...)
	out.Reset()
	if code := serveMCP([]string{"-dir", dir}, bytes.NewReader(append(msgs, '\n')), &out, &errs); code != 0 {
		t.Fatalf("mcp exit %d: %s", code, errs.String())
	}
	if s := out.String(); strings.Contains(s, "send_ping") || strings.Contains(s, "instructions") {
		t.Fatalf("an unattended agent was offered Pong: %s", s)
	}
}
