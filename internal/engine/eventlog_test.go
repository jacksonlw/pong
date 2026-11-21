package engine_test

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// allEvents reads the log from its start until it has been quiet a moment.
func allEvents(t *testing.T, c *client.Client) []engine.Event {
	t.Helper()
	ch, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var out []engine.Event
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-time.After(300 * time.Millisecond):
			return out
		}
	}
}

func count(es []engine.Event, kind string) int {
	n := 0
	for _, e := range es {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// A Session registers at every tool call: one that did not change is told once.
func TestAnUnchangedSessionIsWrittenOnce(t *testing.T) {
	c, _ := enginetest.Start(t)
	reg := engine.RegisterSession{Harness: "claude-code", ID: "s1", Folder: t.TempDir(), State: engine.SessionWorking}
	for range 200 {
		if _, err := c.RegisterSession(reg); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(allEvents(t, c), engine.EventSessionChanged); n != 1 {
		t.Fatalf("%d session events for one unchanged Session, want 1", n)
	}
	reg.Name = "Renamed"
	if _, err := c.RegisterSession(reg); err != nil {
		t.Fatal(err)
	}
	if n := count(allEvents(t, c), engine.EventSessionChanged); n != 2 {
		t.Fatalf("%d session events after a real change, want 2", n)
	}
}

// fold is what a client keeps from a list of events: the last word on each
// Session, Project and the settings, and which Pings wait.
type folded struct {
	sessions, projects map[string]string
	settings           string
	waiting            map[string]bool
}

func fold(t *testing.T, es []engine.Event) folded {
	t.Helper()
	f := folded{sessions: map[string]string{}, projects: map[string]string{}, waiting: map[string]bool{}}
	for _, e := range es {
		var head struct {
			ID, Harness, State string
			UpdatedAt          time.Time `json:"updated_at"`
		}
		json.Unmarshal(e.Data, &head)
		switch {
		case e.Kind == engine.EventSessionChanged:
			f.sessions[head.Harness+"/"+head.ID] = string(e.Data)
		case e.Kind == engine.EventProjectChanged:
			f.projects[head.ID] = string(e.Data)
		case e.Kind == engine.EventSettingsChanged:
			f.settings = string(e.Data)
		case strings.HasPrefix(e.Kind, "ping."):
			if head.State == engine.StateWaiting {
				f.waiting[head.ID] = true
			} else {
				delete(f.waiting, head.ID)
			}
		}
	}
	return f
}

// The snapshot a client starts from folds to the same state as the whole log.
func TestTheDirectoryIsWhatTheLogFoldsTo(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	for _, id := range []string{"s1", "s2"} {
		if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: id, Folder: folder, State: engine.SessionWorking}); err != nil {
			t.Fatal(err)
		}
	}
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", Name: "Later name", State: engine.SessionIdle})
	answered, err := c.CreatePing(client.NewPing{Harness: "claude-code", Session: "s1", Question: "Ship it?", BlocksAgent: true, Answers: []engine.Answer{{Label: "Yes"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreatePing(client.NewPing{Harness: "claude-code", Session: "s2", Question: "And this?", BlocksAgent: true, Answers: []engine.Answer{{Label: "Yes"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pong(answered.ID, client.SendPong{Answer: "Yes"}); err != nil {
		t.Fatal(err)
	}
	var d engine.Directory
	if err := c.Do("GET", "/directory", nil, &d); err != nil {
		t.Fatal(err)
	}
	log := allEvents(t, c)
	got, want := fold(t, d.Events), fold(t, log)
	// A settings event is in the snapshot even when the log has none yet.
	if want.settings == "" {
		got.settings = ""
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the snapshot folds to\n%+v\nthe log to\n%+v", got, want)
	}
	if d.Seq != log[len(log)-1].Seq {
		t.Fatalf("snapshot at seq %d, the log ends at %d", d.Seq, log[len(log)-1].Seq)
	}
}

// A swept Ping leaves none of its events behind.
func TestASweptPingLeavesNoEvents(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Retention: 50 * time.Millisecond, SweepEvery: 10 * time.Millisecond})
	old, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Ship it?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Pong(old.ID, client.SendPong{Text: "yes"}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := c.Ping(old.ID); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("answered Ping was not removed")
		}
	}
	for _, e := range allEvents(t, c) {
		var p struct{ ID string }
		json.Unmarshal(e.Data, &p)
		if strings.HasPrefix(e.Kind, "ping.") && p.ID == old.ID {
			t.Fatalf("the swept Ping's event %d (%s) is still in the log", e.Seq, e.Kind)
		}
	}
}

// A store from before Ping events had a column of their own gets it filled
// from each event, so its old Pings' events are swept with them.
func TestAnOlderStoreGetsItsPingEventsIndexed(t *testing.T) {
	c, dir := enginetest.StartWithDir(t, engine.Config{})
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Old?"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, engine.StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	// As the store was: no ping_id column, no indexes.
	if _, err := db.Exec(`DROP INDEX IF EXISTS events_ping; DROP INDEX IF EXISTS events_kind_at; ALTER TABLE events DROP COLUMN ping_id`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	enginetest.Restart(t, dir) // the new Engine opens the store as it was
	db, _ = sql.Open("sqlite", filepath.Join(dir, engine.StoreFile))
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE ping_id = ?`, p.ID).Scan(&n); err != nil || n == 0 {
		t.Fatalf("the old Ping's events were not given its id: %d, %v", n, err)
	}
}
