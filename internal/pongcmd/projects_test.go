package pongcmd

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
)

func TestEnablingAProjectFromTheCommandLine(t *testing.T) {
	book := &enginetest.Book{}
	box := enginetest.StartComputer(t, book, "Box")
	folder := t.TempDir()
	exec.Command("git", "-C", folder, "init", "-q").Run()
	var out bytes.Buffer
	if c := run([]string{"projects", "enable", "-dir", box.Dir, "-folder", folder}, &out, &out); c != 0 {
		t.Fatalf("exit %d: %s", c, out.String())
	}
	out.Reset()
	run([]string{"projects", "-dir", box.Dir}, &out, &out)
	if !strings.Contains(out.String(), "Sessions from other Devices run on Box") {
		t.Fatalf("list: %s", out.String())
	}
	out.Reset()
	run([]string{"projects", "disable", "-dir", box.Dir, "-folder", folder}, &out, &out)
	out.Reset()
	run([]string{"projects", "-dir", box.Dir}, &out, &out)
	if !strings.Contains(out.String(), "no computer takes Sessions") {
		t.Fatalf("list after disable: %s", out.String())
	}
}
