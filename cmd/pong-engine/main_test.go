package main

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// With -exit-with-stdin the Engine exits cleanly once its standard input
// closes, as when the app that holds the pipe dies.
func TestEngineExitsWhenStdinCloses(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "pong-engine")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	cmd := exec.Command(bin, "-dir", t.TempDir(), "-port", "0", "-devices-port", "-1", "-exit-with-stdin")
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	line, _ := bufio.NewReader(out).ReadString('\n')
	if !strings.Contains(line, "listening") {
		t.Fatalf("Engine said %q", line)
	}
	in.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Engine exited with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Engine still running after its standard input closed")
	}
}
