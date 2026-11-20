package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// A stop crosses Devices as a Pong does: sealed, to the computer that runs
// the Session, whose Engine alone decides whether it takes. Unlike a Pong it is never held: a stop that arrived later would end
// a different turn, so a computer that cannot be reached now is said so at
// once and nothing is kept.

// onStop takes a stop another Device asked for one of this computer's
// Sessions, by the rules of the local API's stop (askStop), and answers as a
// start is answered, with the Session and its stop. Any Device in the set may
// send it, the phone too.
func (c *computers) onStop(from string, m wire.Message) {
	req := m.(*wire.Stop)
	reply := wire.StartResult{RequestID: req.RequestID}
	d, _, _ := c.a.node.Member(from)
	ref := SessionRef{req.Harness, req.SessionID}
	if _, err := c.st.askStop(ref, d.Name); errors.Is(err, errNotFound) {
		reply.Reasons = []string{"no such Session on this computer"}
	} else if err != nil {
		reply.Reasons = []string{err.Error()}
	} else if sess, err := c.st.pr.session(ref.Harness, ref.ID); err != nil {
		reply.Reasons = []string{err.Error()}
	} else {
		reply.OK = true
		reply.Session, _ = json.Marshal(sess)
	}
	c.a.node.Send(from, &reply)
}

// stopWait is how long a stop sent to another computer waits for its answer.
const stopWait = 10 * time.Second

// stopOn asks the computer that runs a Session this computer knows through
// its Pings to stop the turn it is in, and waits for its answer.
func (c *computers) stopOn(ref SessionRef) (Stop, error) {
	owner := c.a.sessionOwner(ref)
	if owner == "" {
		return Stop{}, errNotFound
	}
	d, _, _ := c.a.node.Member(owner)
	req := wire.Stop{RequestID: randomID(), Harness: ref.Harness, SessionID: ref.ID}
	ch := make(chan wire.StartResult, 1)
	c.mu.Lock()
	c.results[req.RequestID] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.results, req.RequestID)
		c.mu.Unlock()
	}()
	if !c.a.node.Send(owner, &req) {
		return Stop{}, errUnreachable{d.Name}
	}
	select {
	case r := <-ch:
		var sess Session
		if !r.OK {
			return Stop{}, errState{strings.Join(r.Reasons, "; ")}
		}
		if json.Unmarshal(r.Session, &sess) != nil || sess.Stop == nil {
			return Stop{}, errors.New("the stop's answer arrived unreadable")
		}
		return *sess.Stop, nil
	case <-time.After(stopWait):
		return Stop{}, errUnreachable{d.Name}
	}
}

// sessionOwner is the computer that runs a Session this computer knows
// through its Pings; "" when it knows none.
func (a *across) sessionOwner(ref SessionRef) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	owner := ""
	for pid, p := range a.remote {
		if p.Session == ref.ID && p.Harness == ref.Harness {
			owner = a.owner[pid]
		}
	}
	return owner
}
