package pongcmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestStatusRunning(t *testing.T) {
	_, dir := enginetest.Start(t)
	var out bytes.Buffer
	if code := run([]string{"status", "-dir", dir, "-home", t.TempDir()}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "running, version "+engine.Version) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestStatusNotRunning(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"status", "-dir", t.TempDir(), "-home", t.TempDir()}, &out, &out); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); first != "Engine: not running" {
		t.Fatalf("output = %q", out.String())
	}
}

func TestStatusAfterEngineStopped(t *testing.T) {
	dir := t.TempDir()
	e, err := engine.Start(engine.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	var out bytes.Buffer
	if code := run([]string{"status", "-dir", dir, "-home", t.TempDir()}, &out, &out); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, out.String())
	}
}
