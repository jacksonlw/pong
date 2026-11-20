package engine_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// journal reads every line the Engine wrote to its diagnostics folder.
func journal(t *testing.T, dir string) []engine.Event {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var evs []engine.Event
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			var e engine.Event
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			evs = append(evs, e)
		}
		fh.Close()
	}
	return evs
}

func TestDiagnosticsKeepEachPingAndPongPastRetention(t *testing.T) {
	diag := t.TempDir()
	c := enginetest.StartWith(t, engine.Config{Diagnostics: diag, Retention: 50 * time.Millisecond, SweepEvery: 10 * time.Millisecond})
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Ship it?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pong(p.ID, client.SendPong{Text: "yes, after lunch"}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := c.Ping(p.ID); isStatus(err, http.StatusNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("answered Ping was not removed")
		}
	}
	var asked, answered bool
	for _, e := range journal(t, diag) {
		var body engine.Ping
		json.Unmarshal(e.Data, &body)
		if body.ID != p.ID {
			continue
		}
		asked = asked || (e.Kind == "ping.created" && body.Question == "Ship it?")
		answered = answered || (body.Pong != nil && body.Pong.Text == "yes, after lunch")
	}
	if !asked || !answered {
		t.Fatalf("journal kept the Ping %v and its Pong %v, want both", asked, answered)
	}
}

func TestNoDiagnosticsUnlessAsked(t *testing.T) {
	c, dir := enginetest.Start(t)
	if _, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Ship it?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, engine.DiagnosticsFolder)); !os.IsNotExist(err) {
		t.Fatalf("diagnostics folder exists without being asked for: %v", err)
	}
}

func TestDefaultDirInTestsIsNeverTheRealOne(t *testing.T) {
	t.Setenv("PONG_HOME", "")
	home, _ := os.UserHomeDir()
	first, err := engine.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	second, _ := engine.DefaultDir()
	if first == filepath.Join(home, "Library", "Application Support", "Pong") || first == filepath.Join(home, ".local", "share", "pong") {
		t.Fatalf("a test got the real data folder %s", first)
	}
	if first != second {
		t.Fatalf("DefaultDir changed within one test binary: %s then %s", first, second)
	}
}
