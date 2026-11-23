package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// ComputerState is what a computer reports about itself, with whether it can be reached now, which is observed here.
type ComputerState struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Platform  string          `json:"platform"`
	This      bool            `json:"this"`
	Reachable bool            `json:"reachable"`
	Harnesses []HarnessStatus `json:"harnesses"`
	// Projects are the keys of the Projects it takes Sessions in from other Devices.
	Projects []string `json:"projects"`
	Running  int      `json:"running"`
}

// Candidate is one computer weighed for a new Session.
type Candidate struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons,omitempty"` // why it cannot take the Session
	Warning  string   `json:"warning,omitempty"` // e.g. an untested Harness version; does not exclude
	Running  int      `json:"running"`
}

// Placement is where a new Session would go, and why each computer can or
// cannot take it.
type Placement struct {
	Choice     string      `json:"choice,omitempty"` // the computer Pong chooses; empty when none can
	Candidates []Candidate `json:"computers"`
}

// noComputer is a start no computer can take, with each one's reasons.
type noComputer struct{ p Placement }

func (e noComputer) Error() string {
	var why []string
	for _, c := range e.p.Candidates {
		why = append(why, c.Name+": "+strings.Join(c.Reasons, "; "))
	}
	return "no computer can take this Session. " + strings.Join(why, ". ")
}

// computers keeps every computer's state.
type computers struct {
	a       *across
	st      *starter
	h       Harnesses
	runners []Runner
	skills  *skillNames // set once made; may be nil

	rmu     sync.Mutex
	mu      sync.Mutex
	remote  map[string]ComputerState
	mine    []HarnessStatus
	checked time.Time
	sent    string // the last report sent, to send only changes
	results map[string]chan wire.StartResult
}

// stateEvery is how often a computer checks its own state and reports any change.
const stateEvery = time.Minute

func newComputers(a *across, st *starter, h Harnesses, runners []Runner) *computers {
	c := &computers{a: a, st: st, h: h, runners: runners, remote: map[string]ComputerState{}, results: map[string]chan wire.StartResult{}}
	a.mu.Lock()
	a.stateChanged = func() { c.report("") }
	a.mu.Unlock()
	a.node.Handle("state", c.onState)
	a.node.Handle("start", c.onStart)
	a.node.Handle("start_result", c.onStartResult)
	a.node.Handle("follow_up", c.onFollowUp)
	a.node.Handle("stop", c.onStop)
	a.node.OnPeer(func(id string, up bool) {
		if up {
			go c.report(id)
		}
	})
	go func() {
		t := time.NewTicker(stateEvery)
		defer t.Stop()
		for {
			select {
			case <-a.stop:
				return
			case <-t.C:
				c.report("")
			}
		}
	}()
	return c
}

// harnesses checks this computer's Harnesses, at most once a minute.
func (c *computers) harnesses(fresh bool) []HarnessStatus {
	c.mu.Lock()
	if !fresh && time.Since(c.checked) < stateEvery && c.mine != nil {
		h := c.mine
		c.mu.Unlock()
		return h
	}
	c.mu.Unlock()
	var list []HarnessStatus
	if c.h != nil {
		for _, s := range c.h.List() {
			for _, r := range c.runners {
				s.CanStart = s.CanStart || r.Harness() == s.ID
			}
			list = append(list, s)
		}
	}
	c.mu.Lock()
	c.mine, c.checked = list, time.Now()
	c.mu.Unlock()
	return list
}

// self is this computer's own state.
func (c *computers) self(fresh bool) ComputerState {
	me, _, _ := c.a.node.Member(c.a.node.ID())
	s := ComputerState{ID: me.ID, Name: me.Name, Platform: me.Platform, This: true, Reachable: true, Harnesses: c.harnesses(fresh), Projects: []string{}}
	if c.skills != nil {
		hs := make([]HarnessStatus, len(s.Harnesses))
		for i, h := range s.Harnesses {
			h.Skills, h.ProjectSkills = c.skills.of(h.ID)
			hs[i] = h
		}
		s.Harnesses = hs
	}
	if all, err := c.a.pr.list(); err == nil {
		for _, p := range all {
			if p.Enabled {
				s.Projects = append(s.Projects, p.Key)
			}
		}
	}
	// Pong runs no Session itself: a computer's load is the
	// Sessions working on it now.
	if all, err := c.st.pr.sessions(); err == nil {
		for _, x := range all {
			if x.State == SessionWorking {
				s.Running++
			}
		}
	}
	return s
}

// report sends this computer's state to one Device, or, with "", to every
// Device when it changed.
func (c *computers) report(to string) {
	c.rmu.Lock() // one at a time, so an older state is never sent after a newer one
	defer c.rmu.Unlock()
	s := c.self(to == "")
	b, _ := json.Marshal(s)
	if to != "" {
		c.a.node.Send(to, &wire.State{State: b})
		return
	}
	c.mu.Lock()
	changed := string(b) != c.sent
	c.sent = string(b)
	c.mu.Unlock()
	if changed {
		c.a.node.Broadcast(&wire.State{State: b}, "")
	}
}

func (c *computers) onState(from string, m wire.Message) {
	var s ComputerState
	if json.Unmarshal(m.(*wire.State).State, &s) != nil || !c.a.fromComputer(from) {
		return
	}
	s.ID, s.This = from, false
	c.mu.Lock()
	c.remote[from] = s
	c.mu.Unlock()
}

// all is every computer in the set with its state; reachability is observed.
func (c *computers) all() []ComputerState {
	out := []ComputerState{c.self(false)}
	c.mu.Lock()
	remote := make([]ComputerState, 0, len(c.remote))
	for _, s := range c.remote {
		remote = append(remote, s)
	}
	c.mu.Unlock()
	for _, s := range remote {
		d, reachable, member := c.a.node.Member(s.ID)
		if !member || !d.Engine {
			continue
		}
		s.Reachable, s.Name = reachable, d.Name
		out = append(out, s)
	}
	sort.SliceStable(out[1:], func(i, j int) bool { return out[1+i].Name < out[1+j].Name })
	return out
}

// place weighs every computer for a new Session in the Project with key, in
// harness: a computer is eligible when it is reachable, has the Project
// enabled (this computer needs only to have it) and has the Harness
// installed and connected; among them the fewest running Sessions wins.
func (c *computers) place(key, harness string) Placement {
	var p Placement
	for _, s := range c.all() {
		cand := Candidate{ID: s.ID, Name: s.Name, Running: s.Running}
		if !s.Reachable {
			cand.Reasons = append(cand.Reasons, "it cannot be reached")
		}
		if s.This {
			if _, err := c.a.pr.byKey(key); err != nil {
				cand.Reasons = append(cand.Reasons, "the Project is not on it")
			}
		} else if !contains(s.Projects, key) {
			cand.Reasons = append(cand.Reasons, "the Project is not enabled on it")
		}
		var h *HarnessStatus
		for i := range s.Harnesses {
			if s.Harnesses[i].ID == harness {
				h = &s.Harnesses[i]
			}
		}
		switch {
		case s.This && h != nil && h.CanStart:
			// This computer's own start checks its Harness as it starts.
		case h == nil || !h.Installed:
			cand.Reasons = append(cand.Reasons, "the Harness is not installed on it")
		case !h.Connected:
			cand.Reasons = append(cand.Reasons, "Pong is not connected to the Harness on it")
		case !h.CanStart:
			cand.Reasons = append(cand.Reasons, "Pong cannot start Sessions in that Harness")
		default:
			cand.Warning = h.Warning
		}
		cand.Eligible = len(cand.Reasons) == 0
		p.Candidates = append(p.Candidates, cand)
	}
	best := -1
	for i, cand := range p.Candidates {
		if cand.Eligible && (best < 0 || cand.Running < p.Candidates[best].Running) {
			best = i
		}
	}
	if best >= 0 {
		p.Choice = p.Candidates[best].ID
	}
	return p
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// startWait is how long a start sent to another computer waits for its answer.
const startWait = 30 * time.Second

// remoteStart is the start body one computer sends another: the Project by
// its key, since each computer has its own id and folder for it.
type remoteStart struct {
	Harness    string     `json:"harness"`
	ProjectKey string     `json:"project_key"`
	Prompt     string     `json:"prompt"`
	Files      []PongFile `json:"files,omitempty"`
}

// startOn starts a Session on another computer and waits for its answer.
func (c *computers) startOn(computer string, in remoteStart) (json.RawMessage, error) {
	d, _, _ := c.a.node.Member(computer)
	body, _ := json.Marshal(in)
	req := wire.Start{RequestID: randomID(), Start: body}
	ch := make(chan wire.StartResult, 1)
	c.mu.Lock()
	c.results[req.RequestID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.results, req.RequestID)
		c.mu.Unlock()
	}()
	if !c.a.node.Send(computer, &req) {
		return nil, errUnreachable{d.Name}
	}
	select {
	case r := <-ch:
		if !r.OK {
			return nil, CannotStart(d.Name + " did not start it: " + strings.Join(r.Reasons, "; "))
		}
		return r.Session, nil
	case <-time.After(startWait):
		return nil, errState{d.Name + " did not answer in time; the Session may have started there, so check before you start it again"}
	}
}

// onStart starts a Session another Device asked for, if this computer takes
// Sessions in that Project from other Devices. Any Device in the set may
// ask, the phone too; the node drops envelopes from anyone else, revoked
// Devices included.
func (c *computers) onStart(from string, m wire.Message) {
	req := m.(*wire.Start)
	reply := wire.StartResult{RequestID: req.RequestID}
	var in remoteStart
	proj, err := Project{}, json.Unmarshal(req.Start, &in)
	if err == nil {
		proj, err = c.a.pr.byKey(in.ProjectKey)
	}
	switch {
	case err != nil:
		reply.Reasons = []string{"the Project is not on this computer"}
	case !proj.Enabled:
		reply.Reasons = []string{"the Project is not enabled on this computer"}
	default:
		prompt, err := c.st.promptWithFiles(in.Prompt, "start-"+randomID(), in.Files)
		var sess Session
		if err == nil {
			sess, err = c.st.start(in.Harness, proj.Root, prompt, nil)
		}
		if err != nil {
			reply.Reasons = []string{err.Error()}
		} else {
			reply.OK = true
			reply.Session, _ = json.Marshal(sess)
		}
	}
	c.a.node.Send(from, &reply)
	go c.report("")
}

func (c *computers) onStartResult(from string, m wire.Message) {
	r := m.(*wire.StartResult)
	c.mu.Lock()
	ch, ok := c.results[r.RequestID]
	c.mu.Unlock()
	if ok {
		select {
		case ch <- *r:
		default:
		}
	}
}

// routes serves the computers' states and placement on the local API.
func (c *computers) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /computers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, c.all())
	})
	mux.HandleFunc("GET /sessions/placement", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		key, err := c.keyOf(q.Get("project"))
		if err != nil {
			writeStart(w, nil, err)
			return
		}
		writeJSON(w, http.StatusOK, c.place(key, q.Get("harness")))
	})
}

// keyOf is the key of a Project here or on another computer, by its id.
func (c *computers) keyOf(id string) (string, error) {
	if p, err := c.a.pr.get(id); err == nil {
		return p.Key, nil
	}
	if sp, ok := c.a.sharedByID(id); ok {
		return sp.Key, nil
	}
	return "", errNotFound
}

// asNoComputer reports whether err is a start no computer could take.
func asNoComputer(err error) (Placement, bool) {
	var nc noComputer
	if errors.As(err, &nc) {
		return nc.p, true
	}
	return Placement{}, false
}

// startPlaced starts a Session in a Project on the computer the human chose,
// or the one Pong chooses. Pong never tries another computer by itself: when
// the one chosen refuses, the human is told why and keeps the prompt.
func (c *computers) startPlaced(in StartSession) (json.RawMessage, error) {
	key, err := c.keyOf(in.Project)
	if err != nil {
		return nil, err
	}
	p := c.place(key, in.Harness)
	target := p.Choice
	if in.Computer != "" {
		target = ""
		for _, cand := range p.Candidates {
			if cand.ID == in.Computer {
				if !cand.Eligible {
					return nil, noComputer{Placement{Candidates: []Candidate{cand}}}
				}
				target = cand.ID
			}
		}
		if target == "" {
			return nil, errState{"that computer is not in your set"}
		}
	}
	if target == "" {
		return nil, noComputer{p}
	}
	if target == c.a.node.ID() {
		proj, err := c.a.pr.byKey(key)
		if err != nil {
			return nil, err
		}
		prompt, err := c.st.promptWithFiles(in.Prompt, "start-"+randomID(), in.Files)
		if err != nil {
			return nil, err
		}
		sess, err := c.st.start(in.Harness, proj.Root, prompt, nil)
		if err != nil {
			return nil, err
		}
		go c.report("")
		return json.Marshal(sess)
	}
	files, err := sendable(in.Files, c.a.partsTo(target))
	if err != nil {
		return nil, err
	}
	return c.startOn(target, remoteStart{Harness: in.Harness, ProjectKey: key, Prompt: in.Prompt, Files: files})
}

// remoteSkills gives the Skill names another computer reported for a Harness
// in a Project, by the Project's id: the computer's chosen, else the first
// that knows the Project.
func (c *computers) remoteSkills(harness, project, computer string) ([]string, bool) {
	key, err := c.keyOf(project)
	if err != nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, 0, len(c.remote))
	for id := range c.remote {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if computer != "" && id != computer {
			continue
		}
		for _, h := range c.remote[id].Harnesses {
			if h.ID != harness {
				continue
			}
			if names, ok := h.ProjectSkills[key]; ok || computer != "" || slices.Contains(c.remote[id].Projects, key) {
				return sortedNames(append(slices.Clone(h.Skills), names...)), true
			}
		}
	}
	return nil, false
}
