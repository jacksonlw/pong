package engine

import (
	"encoding/json"
	"time"
)

// Directory is GET /directory: the current state the event log would fold to,
// as events, and the seq it is at least as new as. A client applies Events
// as it would the same events from /events, then follows /events from Seq.
// The log keeps events about Sessions, Projects and settings for a week
// only, so a client reads this, not the log from its start.
type Directory struct {
	Seq    int64   `json:"seq"`
	Events []Event `json:"events"`
}

func directory(ps *pings, pr *projects, st *settings) (Directory, error) {
	seq, _ := ps.lastSeq() // read first: anything newer comes again from /events
	d := Directory{Seq: seq, Events: []Event{}}
	add := func(kind string, at time.Time, v any) {
		b, _ := json.Marshal(v)
		d.Events = append(d.Events, Event{Seq: seq, Kind: kind, At: at, Data: b})
	}
	cur, err := st.get()
	if err != nil {
		return d, err
	}
	add(EventSettingsChanged, time.Now().UTC(), withPalette(cur))
	projects, err := pr.list()
	if err != nil {
		return d, err
	}
	for _, p := range projects {
		add(EventProjectChanged, p.UpdatedAt, p)
	}
	sessions, err := pr.sessions()
	if err != nil {
		return d, err
	}
	for _, s := range sessions {
		add(EventSessionChanged, s.UpdatedAt, s)
	}
	waiting, err := ps.list(StateWaiting)
	if err != nil {
		return d, err
	}
	for _, p := range waiting {
		add(EventPingState, p.UpdatedAt, p)
	}
	return d, nil
}
