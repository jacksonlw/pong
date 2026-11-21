package engine_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// gonePID returns the id of a process that has already exited.
func gonePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// A Harness process that dies without its end reaching the Engine (killed,
// or ended while the Engine restarted) leaves no Session working forever.
func TestASessionWhoseHarnessProcessIsGoneEnds(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{SessionCheckEvery: 20 * time.Millisecond})
	folder := repo(t, "shop")
	gone, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "gone", Folder: folder, PID: gonePID(t)})
	if err != nil {
		t.Fatal(err)
	}
	if gone.PID == 0 || gone.State != engine.SessionWorking {
		t.Fatalf("registered %+v", gone)
	}
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "alive", Folder: folder, PID: os.Getpid()})
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "unknown", Folder: folder})
	eventually(t, "the Session whose process is gone to end", func() bool {
		s, err := c.Session("claude-code", "gone")
		return err == nil && s.State == engine.SessionEnded
	})
	for _, id := range []string{"alive", "unknown"} {
		if s, _ := c.Session("claude-code", id); s.State != engine.SessionWorking {
			t.Errorf("%s became %s", id, s.State)
		}
	}
}

// A Session with no Harness process of its own to watch (none given, or one
// shared with other Sessions, as Codex's app-server is) ends once it has been
// quiet for SessionQuietFor; one with its own live process never does.
func TestAQuietSessionWithNoProcessOfItsOwnEnds(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{SessionCheckEvery: 20 * time.Millisecond, SessionQuietFor: 100 * time.Millisecond})
	folder := repo(t, "shop")
	shared := os.Getppid()
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "no-pid", Folder: folder})
	c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "thread-1", Folder: folder, PID: shared, State: engine.SessionIdle})
	c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "thread-2", Folder: folder, PID: shared, State: engine.SessionIdle})
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "own", Folder: folder, PID: os.Getpid(), State: engine.SessionIdle})
	for _, s := range []struct{ h, id string }{{"claude-code", "no-pid"}, {"codex", "thread-1"}, {"codex", "thread-2"}} {
		eventually(t, s.id+" to end", func() bool {
			x, err := c.Session(s.h, s.id)
			return err == nil && x.State == engine.SessionEnded
		})
	}
	if s, _ := c.Session("claude-code", "own"); s.State != engine.SessionIdle {
		t.Errorf("a Session with its own live process became %s", s.State)
	}
	// Its next hook brings an ended one back.
	if s, _ := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "no-pid", State: engine.SessionWorking}); s.State != engine.SessionWorking {
		t.Errorf("a hook after the end left it %s", s.State)
	}
}

// The Engine also checks when it starts, so Sessions whose processes died
// while it was down end at once.
func TestTheEngineEndsGoneSessionsWhenItStarts(t *testing.T) {
	c, dir := enginetest.StartWithDir(t, engine.Config{SessionCheckEvery: time.Hour})
	c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "gone", Folder: repo(t, "api"), PID: gonePID(t), State: engine.SessionIdle})
	c = enginetest.RestartWith(t, dir, engine.Config{SessionCheckEvery: time.Hour})
	eventually(t, "the Session to end after a restart", func() bool {
		s, err := c.Session("codex", "gone")
		return err == nil && s.State == engine.SessionEnded
	})
}

// A Ping that names no Harness, sent while the Harness's hook could not reach
// the Engine, joins the one Session known by its id instead of making a copy.
func TestAPingWithNoHarnessJoinsTheSessionKnownByItsID(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := repo(t, "shop")
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", Folder: folder})
	p, err := c.CreatePing(client.NewPing{Session: "s1", Folder: folder, Question: "Reinstalled.", IsNote: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Harness != "claude-code" {
		t.Fatalf("Ping's Harness = %q", p.Harness)
	}
	all, err := c.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.ID == "s1" && s.Harness != "claude-code" {
			t.Fatalf("a second Session %+v", s)
		}
	}
}

// A Harness process that has exited but not been reaped by its parent (a
// zombie) is gone: it does no more work, and its Session ends.
func TestASessionWhoseHarnessProcessIsAZombieEnds(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Wait() })
	eventually(t, "the child to exit unreaped", func() bool { return isZombie(cmd.Process.Pid) })
	c := enginetest.StartWith(t, engine.Config{SessionCheckEvery: 20 * time.Millisecond})
	c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "zombie", Folder: repo(t, "api"), PID: cmd.Process.Pid})
	eventually(t, "the zombie's Session to end", func() bool {
		s, err := c.Session("codex", "zombie")
		return err == nil && s.State == engine.SessionEnded
	})
}

// A Session Pong learns of only from a Ping, with no hook to say when it
// ends, takes the Harness process the Ping names and ends once it is gone.
func TestASessionKnownOnlyFromPingsEndsWhenItsHarnessProcessIsGone(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{SessionCheckEvery: 20 * time.Millisecond})
	folder := repo(t, "shop")
	if _, err := c.CreatePing(client.NewPing{Session: "mcp-1", Folder: folder, Question: "Tests pass.", IsNote: true, PID: gonePID(t)}); err != nil {
		t.Fatal(err)
	}
	c.CreatePing(client.NewPing{Session: "mcp-2", Folder: folder, Question: "Still going.", IsNote: true, PID: os.Getpid()})
	eventually(t, "the Session whose process is gone to end", func() bool {
		s, err := c.Session("", "mcp-1")
		return err == nil && s.State == engine.SessionEnded
	})
	if s, _ := c.Session("", "mcp-2"); s.State != engine.SessionWorking {
		t.Errorf("mcp-2 became %s", s.State)
	}
}
