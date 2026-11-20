package engine

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// sessionFacts is what a computer tells the other Devices about one of its
// Sessions: what a card's Project line, its context ring and a list of
// Threads show. Its fields are named as the local API names a Session's, so
// an interface reads it as it reads session.changed. The folder and the
// commands that open the Session stay on its computer.
type sessionFacts struct {
	Harness   string   `json:"harness"`
	ID        string   `json:"id"`
	Project   string   `json:"project,omitempty"` // this computer's id for the Project
	Title     string   `json:"title,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	StartedBy string   `json:"started_by"`
	State     string   `json:"state"`
	Context   *Context `json:"context,omitempty"`
	Stop      *Stop    `json:"stop,omitempty"` // the stop asked for its turn (stop.go)
	// CreatedAt and TurnStartedAt say how long it and its turn have run.
	CreatedAt     time.Time  `json:"created_at"`
	TurnStartedAt *time.Time `json:"turn_started_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func factsOf(s Session) sessionFacts {
	return sessionFacts{Harness: s.Harness, ID: s.ID, Project: s.Project, Title: s.Title, Summary: s.Summary, StartedBy: s.StartedBy,
		State: s.State, Context: s.Context, Stop: s.Stop, CreatedAt: s.CreatedAt, TurnStartedAt: s.TurnStartedAt, UpdatedAt: s.UpdatedAt}
}

// sessionsSent is how many Sessions a snapshot carries at most, beyond those
// with a waiting Ping: the latest, as a list of Threads shows them.
const sessionsSent = 50

// sessionEvery is how often an unchanged Session's updated time is sent again.
const sessionEvery = time.Minute

// snapshotSessions is the facts of this computer's Sessions, all, for a
// Device that just connected: every Session with a waiting Ping, then the
// latest.
func snapshotSessions(all []Session, waiting []Ping) []json.RawMessage {
	asks := map[string]bool{}
	for _, p := range waiting {
		asks[p.Session] = true
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	var out []json.RawMessage
	for i, s := range all {
		if i < sessionsSent || asks[s.ID] {
			b, _ := json.Marshal(factsOf(s))
			out = append(out, b)
		}
	}
	return out
}

// watchSessions follows this Engine's events and sends every Device the
// facts of a Session when they change. A Harness's hooks register a Session
// on every tool call, so facts already sent go again only once a minute, for
// the updated time.
func (a *across) watchSessions() {
	seq, _ := a.s.lastSeq()
	type was struct {
		facts string
		at    time.Time
	}
	sent := map[SessionRef]was{}
	for {
		evs, next, err := a.s.eventsAfter(seq)
		if err != nil {
			return // the Engine is closing
		}
		for _, e := range evs {
			seq = e.Seq
			var s Session
			if e.Kind != EventSessionChanged || json.Unmarshal(e.Data, &s) != nil {
				continue
			}
			f := factsOf(s)
			f.UpdatedAt = time.Time{}
			same, _ := json.Marshal(f)
			ref := SessionRef{s.Harness, s.ID}
			if w, ok := sent[ref]; ok && w.facts == string(same) && s.UpdatedAt.Sub(w.at) < sessionEvery {
				continue
			}
			sent[ref] = was{string(same), s.UpdatedAt}
			b, _ := json.Marshal(factsOf(s))
			a.node.Broadcast(&wire.Session{Computer: a.node.ID(), Session: b}, "")
		}
		select {
		case <-a.stop:
			return
		case <-next:
		}
	}
}
