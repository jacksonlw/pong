package claudecode_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// call runs the hook on a recorded payload in the background, as Claude Code
// would, and returns a function that waits for what the hook wrote.
func call(t *testing.T, dir, fixture string) func() string {
	t.Helper()
	return callFile(t, dir, filepath.Join("testdata", fixture))
}

// callBytes runs the hook on payload and returns what it wrote.
func callBytes(t *testing.T, dir string, payload []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// callFile is call for a payload at any path.
func callFile(t *testing.T, dir, file string) func() string {
	t.Helper()
	in, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		claudecode.Hook(dir, bytes.NewReader(in), &out)
		done <- out.String()
	}()
	return func() string {
		select {
		case s := <-done:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("the hook did not return")
			return ""
		}
	}
}

// waiting plays the interface: it waits until n Pings are waiting and returns
// them oldest first.
func waiting(t *testing.T, c *client.Client, n int) []engine.Ping {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ps, err := c.Pings(engine.StateWaiting)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) >= n {
			return ps
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fewer than %d Pings arrived", n)
	return nil
}

func pong(t *testing.T, c *client.Client, id string, p engine.SendPong) engine.Ping {
	t.Helper()
	got, err := c.Pong(id, p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// golden compares the hook's output with the expected bytes in testdata.
func golden(t *testing.T, got, file string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("Claude Code would receive\n%s\nwant (%s)\n%s", got, file, want)
	}
}
