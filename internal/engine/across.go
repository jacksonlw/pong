package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/wire"
)

// Events about the set of Devices.
const (
	// EventPingDraft carries {"id", "draft"} when a draft Pong changes on
	// any Device.
	EventPingDraft = "ping.draft"
	// EventPingRemoved carries {"id"} when another computer's Ping is no
	// longer shown here, because that computer no longer reports it.
	EventPingRemoved = "ping.removed"
)

// startError is a Pong refused because the Session it would start or resume
// could not be started.
type startError struct{ err error }

func (e startError) Error() string { return e.err.Error() }

// errLost is a Pong that arrived after another one was already applied.
type errLost struct {
	winner *Pong
	msg    string
}

func (e errLost) Error() string { return e.msg }

// errUnreachable is an action on another computer's Ping while that
// computer cannot be reached.
type errUnreachable struct{ name string }

func (e errUnreachable) Error() string {
	return e.name + " cannot be reached right now, so this was not sent"
}

// applyPong records a Pong on one of this Engine's Pings: the one
// authority on whether it still applies.
func (s *pings) applyPong(id string, in SendPong) (Ping, error) {
	if len(in.Files) > 0 {
		cur, err := s.get(id)
		if err != nil {
			return cur, err
		}
		if cur.State == StateWaiting {
			paths, err := placeFiles(s.dir, cur.Session, in.Files)
			if err != nil {
				return cur, err
			}
			in.Text = withFiles(in.Text, paths)
		}
		in.Files = nil
	}
	pong := &Pong{Answer: in.Answer, Answers: in.Answers, Text: in.Text, NewSession: in.NewSession}
	var undo func()
	var after func(Ping) Ping
	if cur, err := s.get(id); err == nil && s.onPong != nil && cur.State == StateWaiting && in.check(cur) == nil {
		if undo, after, err = s.onPong(cur, in); err != nil {
			return cur, startError{err}
		}
	}
	p, err := s.end(id, StateAnswered, in.check, pong)
	if err != nil {
		if undo != nil {
			undo()
		}
		if p.Pong != nil && p.State == StateAnswered {
			return p, errLost{winner: p.Pong, msg: "an earlier Pong was already sent to this Ping, so yours was not: " + describePong(p.Pong)}
		}
		return p, err
	}
	if after != nil {
		p = after(p)
	} else {
		s.sent(p)
	}
	return p, nil
}

func describePong(p *Pong) string {
	switch {
	case p.Answer != "" && p.Text != "":
		return "“" + p.Answer + "” with “" + p.Text + "”"
	case p.Answer != "":
		return "“" + p.Answer + "”"
	case len(p.Answers) > 0:
		return "“" + strings.Join(p.Answers, "”, “") + "”"
	}
	return "“" + p.Text + "”"
}

// getAny returns one of this Engine's Pings or another computer's.
func (s *pings) getAny(id string) (Ping, error) {
	p, err := s.get(id)
	if errors.Is(err, errNotFound) && s.set != nil {
		return s.set.ping(id)
	}
	return p, err
}

// decorate marks p with what the local API shows but never stores.
func (s *pings) decorate(p *Ping) {
	s.mu.Lock()
	p.Draft = s.drafts[p.ID]
	set := s.set
	s.mu.Unlock()
	if set != nil {
		set.decorate(p)
	}
}

// decorateEvent decorates the Ping a ping.* event carries.
func (s *pings) decorateEvent(e Event) Event {
	if s.set == nil {
		return e
	}
	switch e.Kind {
	case EventPingCreated, EventPingPonged, EventPingState, EventPongDelivered, EventPongUndelivered:
	default:
		return e
	}
	var p Ping
	if json.Unmarshal(e.Data, &p) != nil {
		return e
	}
	s.decorate(&p)
	e.Data, _ = json.Marshal(p)
	return e
}

// setDraft keeps the draft for Ping id in memory, shows it here and, when
// it was written here, sends it to every other Device.
func (s *pings) setDraft(id string, draft json.RawMessage, fromHere bool) {
	s.mu.Lock()
	if s.drafts == nil {
		s.drafts = map[string]json.RawMessage{}
	}
	s.drafts[id] = draft
	set := s.set
	s.mu.Unlock()
	if fromHere {
		// The interface that wrote it already shows it.
		if set != nil {
			set.node.Broadcast(&wire.Draft{PingID: id, Draft: draft}, "")
		}
		return
	}
	s.appendMem(EventPingDraft, s.clock(), map[string]any{"id": id, "draft": draft})
}

// across is this Engine's part in the set of Devices: it sends its own
// Pings to every Device and keeps other computers' Pings in memory. Each
// Engine is the only authority for its own Pings.
type across struct {
	node *devices.Node
	s    *pings

	mu      sync.Mutex
	remote  map[string]Ping               // other computers' Pings, by id
	owner   map[string]string             // Ping id -> the computer holding it
	results map[string]chan wire.Delivery // forwarded Pong id -> its answer
	// pending are Pongs sent from here for another computer's Ping that it
	// has not answered yet, by Ping id; lost are those that arrived second.
	pending map[string]wire.Pong
	lost    map[string]Lost
	// applied are Pongs another Device sent for this Engine's Pings, by Ping
	// id: only that Device may withdraw or resend them, and the same Pong
	// arriving twice is not a second answer. Kept in the store too
	// (across_applied.go).
	applied map[string]appliedPong
	// sentIDs are the Pongs sent from here for other computers' Pings, by
	// Ping id, to withdraw them. Kept in the store too (across_applied.go).
	sentIDs map[string]sentPong
	// parts and threads wait for Resources and Threads asked of another computer.
	parts   map[string][]chan wire.Resource
	threads map[string][]chan wire.Thread
	// shared is other computers' Project metadata, by computer then key.
	shared map[string]map[string]sharedProject
	pr     *projects
	// stateChanged reports this computer's state again; set by computers.
	stateChanged func()
	// thread reads one of this Engine's Threads (projects.thread). May be nil.
	thread func(harness, id string) (Thread, error)

	qmu   sync.Mutex
	queue []wire.Message
	wake  chan struct{}
	stop  chan struct{}
}

type appliedPong struct{ from, pongID string }

// pongWait is how long a forwarded Pong waits for the computer holding the
// Ping to answer.
const pongWait = 10 * time.Second

func newAcross(node *devices.Node, s *pings) *across {
	a := &across{node: node, s: s, remote: map[string]Ping{}, owner: map[string]string{},
		results: map[string]chan wire.Delivery{}, pending: map[string]wire.Pong{}, lost: map[string]Lost{}, applied: map[string]appliedPong{}, sentIDs: map[string]sentPong{}, shared: map[string]map[string]sharedProject{},
		parts: map[string][]chan wire.Resource{}, threads: map[string][]chan wire.Thread{}, wake: make(chan struct{}, 1), stop: make(chan struct{})}
	if err := a.loadSent(); err != nil {
		fmt.Fprintln(os.Stderr, "pong-engine: Pongs sent to other computers could not be read back:", err)
	}
	if err := a.loadApplied(); err != nil {
		fmt.Fprintln(os.Stderr, "pong-engine: Pongs sent from other Devices could not be read back:", err)
	}
	node.Handle("ping", a.onPing)
	node.Handle("snapshot", a.onSnapshot)
	node.Handle("pong", a.onPong)
	node.Handle("delivery", a.onDelivery)
	node.Handle("draft", a.onDraft)
	node.Handle("dismiss", a.onDismiss)
	node.Handle("withdraw", a.onWithdraw)
	node.Handle("resource_get", a.onResourceGet)
	node.Handle("resource", a.onResource)
	node.Handle("thread_get", a.onThreadGet)
	node.Handle("thread", a.onThread)
	node.Handle("project", a.onProject)
	// Pongs still in the outbox from before a restart are still on their way.
	for _, q := range node.Outbox() {
		if q.Kind == "pong" {
			if m, err := wire.Decode(q.Body); err == nil {
				pm := m.(*wire.Pong)
				if pm.Again == "" {
					a.pending[pm.PingID] = *pm
				}
			}
		}
	}
	node.OnPeer(a.onPeer)
	node.OnChange(a.prune)
	go a.sendLoop()
	go a.watchSessions()
	return a
}

func (a *across) close() { close(a.stop) }

// local is told of every event on this Engine's own Pings, in order, with
// s.mu held: it queues the Ping for every Device.
func (a *across) local(kind string, p Ping) {
	if p.State != StateWaiting {
		delete(a.s.drafts, p.ID) // s.mu is held
	}
	b, _ := json.Marshal(p)
	a.qmu.Lock()
	a.queue = append(a.queue, &wire.Ping{Computer: a.node.ID(), Event: kind, Ping: b})
	if p.Pong != nil && (kind == EventPongDelivered || kind == EventPongUndelivered) {
		state := wire.Delivered
		if kind == EventPongUndelivered {
			state = wire.Undelivered
		}
		a.mu.Lock()
		ap := a.applied[p.ID]
		a.mu.Unlock()
		a.queue = append(a.queue, &wire.Delivery{PingID: p.ID, PongID: ap.pongID, State: state, Reason: p.Pong.NotDelivered})
	}
	if p.Pong != nil && (p.Pong.Delivery == DeliveryDelivered || p.Pong.Delivery == DeliveryLeft) {
		a.dropApplied(p.ID) // nothing more can be done with it from elsewhere
	}
	a.qmu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *across) sendLoop() {
	for {
		select {
		case <-a.stop:
			return
		case <-a.wake:
		}
		a.qmu.Lock()
		q := a.queue
		a.queue = nil
		a.qmu.Unlock()
		for _, m := range q {
			a.node.Broadcast(m, "")
		}
	}
}

// onPeer sends a Device that just connected every waiting Ping here, and
// re-shows the Pings of a computer whose reachability changed.
func (a *across) onPeer(id string, up bool) {
	if up {
		a.sendProjects(id)
		// A Pong not yet delivered, or back with the human, can still be
		// withdrawn, sent again or left from the Device that sent it: its
		// Ping goes too, so that Device has it after a restart.
		answered, _ := a.s.list(StateAnswered)
		var sessions []Session
		if a.pr != nil {
			sessions, _ = a.pr.sessions()
		}
		// The waiting Pings are read last and sent at once: a Ping created
		// after they are read goes by itself, and must not get there first,
		// or this snapshot would say it no longer waits.
		ps, err := a.s.listOldest(StateWaiting)
		if err == nil {
			snap := wire.Snapshot{Computer: a.node.ID(), Pings: []wire.Ping{}, Sessions: snapshotSessions(sessions, ps)}
			for _, p := range answered {
				if p.Pong != nil && !p.Unasked && p.Pong.Delivery != "" && p.Pong.Delivery != DeliveryDelivered && p.Pong.Delivery != DeliveryLeft {
					ps = append(ps, p)
				}
			}
			for _, p := range ps {
				b, _ := json.Marshal(p)
				snap.Pings = append(snap.Pings, wire.Ping{Computer: a.node.ID(), Event: EventPingCreated, Ping: b})
			}
			a.node.Send(id, &snap)
		}
		a.resendPending(id)
	}
	a.mu.Lock()
	var shown []Ping
	for pid, o := range a.owner {
		if o == id {
			if p := a.remote[pid]; p.State == StateWaiting {
				shown = append(shown, p)
			}
		}
	}
	a.mu.Unlock()
	for _, p := range shown {
		a.s.appendMem(EventPingState, a.s.clock(), p)
	}
}

// resendPending sends id again each Pong sent to it that has no answer yet. A
// newer connection replaces an older one, as when two computers that just paired
// dial each other, and the older one can take the answer down with it. id
// answers a Pong it already took as sent again (see onPong). One still in the
// outbox goes when the outbox is flushed.
func (a *across) resendPending(id string) {
	queued := map[string]bool{}
	for _, q := range a.node.Outbox() {
		queued[q.Key] = true
	}
	a.mu.Lock()
	var again []wire.Pong
	for pid, pm := range a.pending {
		if a.owner[pid] == id && !queued["pong:"+pid] {
			again = append(again, pm)
		}
	}
	a.mu.Unlock()
	for i := range again {
		a.node.Send(id, &again[i])
	}
}

// upsert keeps another computer's Ping and shows the change here.
func (a *across) upsert(from string, kind string, raw json.RawMessage) {
	var p Ping
	if json.Unmarshal(raw, &p) != nil || p.ID == "" {
		return
	}
	p.Computer, p.Remote, p.Draft = nil, false, nil
	a.mu.Lock()
	if o, ok := a.owner[p.ID]; ok && o != from {
		a.mu.Unlock()
		return // only the computer holding a Ping speaks for it
	}
	if _, mine := a.s.getLocal(p.ID); mine {
		a.mu.Unlock()
		return
	}
	_, known := a.remote[p.ID]
	a.remote[p.ID] = p
	a.owner[p.ID] = from
	// A Pong sent from here that its computer reports delivered, left or
	// taken back can no longer be withdrawn; one still on its way can.
	_, onItsWay := a.pending[p.ID]
	finished := !onItsWay && (p.State != StateAnswered || p.Pong == nil || p.Pong.Delivery == DeliveryDelivered || p.Pong.Delivery == DeliveryLeft)
	// Forget other computers' Pings that ended a day ago.
	for id, q := range a.remote {
		if q.State != StateWaiting && a.s.clock().Sub(q.UpdatedAt) > 24*time.Hour {
			delete(a.remote, id)
			delete(a.owner, id)
		}
	}
	a.mu.Unlock()
	if finished {
		a.dropSent(p.ID)
	}
	if p.State != StateWaiting {
		a.s.mu.Lock()
		delete(a.s.drafts, p.ID)
		a.s.mu.Unlock()
	}
	if !known {
		kind = EventPingCreated
	}
	if kind == "" {
		kind = EventPingState
	}
	a.s.appendMem(kind, p.UpdatedAt, p)
}

// fromComputer reports whether id is a computer in the set: only a computer
// holds Pings, so a phone cannot report one.
func (a *across) fromComputer(id string) bool {
	d, _, ok := a.node.Member(id)
	return ok && d.Engine
}

func (a *across) onPing(from string, m wire.Message) {
	pm := m.(*wire.Ping)
	if pm.Computer != from || !a.fromComputer(from) {
		return
	}
	a.upsert(from, pm.Event, pm.Ping)
}

func (a *across) onSnapshot(from string, m wire.Message) {
	sn := m.(*wire.Snapshot)
	if sn.Computer != from || !a.fromComputer(from) {
		return
	}
	in := map[string]bool{}
	for _, pm := range sn.Pings {
		var head struct {
			ID string `json:"id"`
		}
		json.Unmarshal(pm.Ping, &head)
		in[head.ID] = true
		a.mu.Lock()
		old, known := a.remote[head.ID]
		a.mu.Unlock()
		var p Ping
		json.Unmarshal(pm.Ping, &p)
		if known && (old.UpdatedAt.Equal(p.UpdatedAt) && old.State == p.State || old.UpdatedAt.After(p.UpdatedAt)) {
			continue // as known, or a later change already got here
		}
		a.upsert(from, EventPingState, pm.Ping)
	}
	// A waiting Ping the computer no longer reports is no longer waiting
	// there; it is not shown here any more.
	a.mu.Lock()
	var unsent []string
	for id, sp := range a.sentIDs {
		// Not reported any more: delivered or left while out of reach, or no longer kept.
		if _, onItsWay := a.pending[id]; sp.computer == from && !in[id] && !onItsWay {
			unsent = append(unsent, id)
		}
	}
	a.mu.Unlock()
	for _, id := range unsent {
		a.dropSent(id)
	}
	a.mu.Lock()
	var gone []string
	for id, o := range a.owner {
		if o == from && !in[id] && a.remote[id].State == StateWaiting {
			gone = append(gone, id)
			delete(a.remote, id)
			delete(a.owner, id)
		}
	}
	a.mu.Unlock()
	for _, id := range gone {
		a.s.appendMem(EventPingRemoved, a.s.clock(), map[string]string{"id": id})
	}
}

// prune stops showing the Pings of a computer that left the set, such as a
// revoked one, and forgets its drafts.
func (a *across) prune() {
	a.mu.Lock()
	var gone []string
	for id, o := range a.owner {
		if _, _, member := a.node.Member(o); !member {
			gone = append(gone, id)
			delete(a.remote, id)
			delete(a.owner, id)
			delete(a.pending, id)
			delete(a.lost, id)
		}
	}
	var unsent []string
	for id, sp := range a.sentIDs {
		if _, _, member := a.node.Member(sp.computer); !member {
			unsent = append(unsent, id)
		}
	}
	var left []string
	for id, ap := range a.applied {
		if _, _, member := a.node.Member(ap.from); !member {
			left = append(left, id)
		}
	}
	a.mu.Unlock()
	for _, id := range left {
		a.dropApplied(id)
	}
	for _, id := range unsent {
		a.dropSent(id)
	}
	a.s.mu.Lock()
	for _, id := range gone {
		delete(a.s.drafts, id)
	}
	a.s.mu.Unlock()
	for _, id := range gone {
		a.s.appendMem(EventPingRemoved, a.s.clock(), map[string]string{"id": id})
	}
}

func (a *across) has(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.remote[id]
	return ok
}

func (a *across) ping(id string) (Ping, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.remote[id]
	if !ok {
		return p, errNotFound
	}
	return p, nil
}

func (a *across) list(state string) []Ping {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Ping
	for _, p := range a.remote {
		if state == "" || p.State == state {
			out = append(out, p)
		}
	}
	return out
}

func (a *across) lastPonged() (Ping, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var last Ping
	ok := false
	for _, p := range a.remote {
		if p.Pong != nil && (!ok || p.Pong.SentAt.After(last.Pong.SentAt)) {
			last, ok = p, true
		}
	}
	return last, ok
}

// decorate marks p with its computer once the set has more than one, with
// whether it is another computer's, and with its draft.
func (a *across) decorate(p *Ping) {
	a.mu.Lock()
	owner, remote := a.owner[p.ID]
	pend, pending := a.pending[p.ID]
	lost, isLost := a.lost[p.ID]
	a.mu.Unlock()
	if !remote {
		owner = a.node.ID()
	}
	p.Remote = remote
	if remote {
		if local, ok := a.localProjectFor(p.Project); ok {
			p.Project = local // the same Project here
		}
	}
	if pending && p.State == StateWaiting {
		// Sent from here and on its way: the computer holding the Ping has
		// not taken it yet.
		var in SendPong
		json.Unmarshal(pend.Pong, &in)
		p.State = StateAnswered
		p.Pong = &Pong{Answer: in.Answer, Answers: in.Answers, Text: in.Text, NewSession: in.NewSession, Delivery: DeliverySent}
	}
	if isLost {
		p.Lost = &lost
	}
	if a.node.Computers() > 1 {
		d, reachable, _ := a.node.Member(owner)
		p.Computer = &Computer{ID: owner, Name: d.Name, Reachable: reachable}
	}
}

// forwardPong sends a Pong for another computer's Ping to that computer: it
// alone decides whether the Pong applies. When that computer cannot be
// reached the Pong waits in the outbox and goes when the two meet; the
// Ping shows it as sent meanwhile.
func (a *across) forwardPong(id string, in SendPong) (Ping, error) {
	a.mu.Lock()
	p, ok := a.remote[id]
	owner := a.owner[id]
	_, already := a.pending[id]
	a.mu.Unlock()
	if !ok {
		return p, errNotFound
	}
	if already {
		return p, errState{"a Pong from this Device is already on its way to this Ping: withdraw it first to change it"}
	}
	if p.State != StateWaiting {
		if p.Pong != nil {
			return p, errLost{winner: p.Pong, msg: "an earlier Pong was already sent to this Ping, so yours was not: " + describePong(p.Pong)}
		}
		return p, errState{"this Ping is already " + p.State + "; only a waiting Ping can change"}
	}
	if len(in.Files) > 0 {
		files, err := inlineFiles(in.Files)
		if err != nil {
			return p, err
		}
		in.Files = files
	}
	body, _ := json.Marshal(in)
	pm := wire.Pong{PingID: id, PongID: randomID(), Pong: body}
	ch := make(chan wire.Delivery, 1)
	a.mu.Lock()
	a.results[pm.PongID] = ch
	a.pending[id] = pm
	delete(a.lost, id)
	a.mu.Unlock()
	a.keepSent(id, sentPong{computer: owner, pongID: pm.PongID})
	defer func() {
		a.mu.Lock()
		delete(a.results, pm.PongID)
		a.mu.Unlock()
	}()
	now, err := a.node.SendOrQueue(owner, "pong:"+id, &pm)
	if err != nil {
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		a.dropSent(id)
		return p, err
	}
	a.s.appendMem(EventPingState, a.s.clock(), p)
	if now {
		select {
		case r := <-ch:
			var winner Pong
			if r.State != wire.Sent && len(r.Winner) > 0 && json.Unmarshal(r.Winner, &winner) == nil {
				return p, errLost{winner: &winner, msg: r.Reason}
			}
			if r.State != wire.Sent {
				return p, errState{r.Reason}
			}
		case <-time.After(pongWait):
			// It went; the answer comes later and is shown then.
		}
	}
	p, _ = a.ping(id)
	if p.State == StateWaiting {
		// Taken by its computer, whose report of the change may not be here yet.
		p.State = StateAnswered
		p.Pong = &Pong{Answer: in.Answer, Answers: in.Answers, Text: in.Text, NewSession: in.NewSession, SentAt: a.s.clock(), Delivery: DeliverySent}
	}
	return p, nil
}

// onPong applies a Pong another Device sent for one of this Engine's Pings
// and tells it whether it applied, and which Pong won when it did not.
func (a *across) onPong(from string, m wire.Message) {
	pm := m.(*wire.Pong)
	a.mu.Lock()
	ap, had := a.applied[pm.PingID]
	a.mu.Unlock()
	if pm.Again != "" {
		// Its sender is told whether it was acted on, and why not.
		again := wire.Delivery{PingID: pm.PingID, PongID: pm.PongID, State: wire.Undelivered}
		switch {
		case !had || ap.from != from:
			again.Reason = "only the Device that sent a Pong can send it again or leave it"
		case pm.Again == "leave":
			if _, ok, err := a.s.deliver(pm.PingID, []string{DeliveryUndelivered, DeliveryUnconfirmed}, DeliveryLeft, ""); err != nil {
				again.Reason = err.Error()
			} else if !ok {
				again.Reason = "only an undelivered Pong, or one that may not have arrived, can be left"
			}
		case (pm.Again == "same_session" || pm.Again == "new_session") && a.s.again != nil:
			if _, err := a.s.again(pm.PingID, SendAgain{NewSession: pm.Again == "new_session"}); err != nil {
				again.Reason = err.Error()
			}
		default:
			again.Reason = "this computer cannot do that with a Pong"
		}
		if again.Reason == "" {
			again.State = wire.Sent
		}
		a.node.Send(from, &again)
		return
	}
	var in SendPong
	reply := wire.Delivery{PingID: pm.PingID, PongID: pm.PongID}
	if err := json.Unmarshal(pm.Pong, &in); err != nil {
		reply.State, reply.Reason = wire.Undelivered, "the Pong could not be read"
		a.node.Send(from, &reply)
		return
	}
	if had && ap.from == from && ap.pongID == pm.PongID {
		reply.State = wire.Sent // the same Pong again, after its sender restarted
		a.node.Send(from, &reply)
		return
	}
	_, err := a.s.applyPong(pm.PingID, in)
	var lost errLost
	switch {
	case err == nil:
		reply.State = wire.Sent
		a.mu.Lock()
		a.applied[pm.PingID] = appliedPong{from: from, pongID: pm.PongID}
		a.mu.Unlock()
		// Its sender is told first: writing it to the store takes a moment.
		defer a.storeApplied(pm.PingID)
	case errors.As(err, &lost):
		reply.State, reply.Reason = wire.Undelivered, lost.msg
		reply.Winner, _ = json.Marshal(lost.winner)
	default:
		reply.State, reply.Reason = wire.Undelivered, err.Error()
	}
	a.node.Send(from, &reply)
}

func (a *across) onDelivery(from string, m wire.Message) {
	d := m.(*wire.Delivery)
	a.mu.Lock()
	ch, waiting := a.results[d.PongID]
	owner := a.owner[d.PingID]
	pend, pending := a.pending[d.PingID]
	if owner != from || !pending || pend.PongID != d.PongID {
		a.mu.Unlock()
		return
	}
	delete(a.pending, d.PingID)
	if d.Reason == withdrawn {
		p := a.remote[d.PingID]
		a.mu.Unlock()
		a.dropSent(d.PingID)
		a.s.appendMem(EventPingState, a.s.clock(), p)
		return
	}
	if d.State != wire.Sent {
		defer a.dropSent(d.PingID) // it was not applied: there is nothing of ours to withdraw
	}
	if d.State != wire.Sent && !waiting {
		// Nobody waits for the answer any more: keep it for the card.
		var in SendPong
		json.Unmarshal(pend.Pong, &in)
		a.lost[d.PingID] = Lost{Pong: Pong{Answer: in.Answer, Answers: in.Answers, Text: in.Text, NewSession: in.NewSession}, Reason: d.Reason}
	}
	p := a.remote[d.PingID]
	a.mu.Unlock()
	if waiting {
		select {
		case ch <- *d:
		default:
		}
	}
	a.s.appendMem(EventPingState, a.s.clock(), p)
}

// withdrawPong takes back a Pong sent from here that is not yet delivered:
// from the outbox if it has not gone, else from the computer holding the
// Ping, which takes it back only if it is not yet delivered.
func (a *across) withdrawPong(id string) (Ping, error) {
	p, err := a.ping(id)
	if err != nil {
		return p, err
	}
	a.mu.Lock()
	owner := a.owner[id]
	pend, pending := a.pending[id]
	a.mu.Unlock()
	if pending && a.node.Unqueue("pong:"+id) { // still here: it never went
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		a.dropSent(id)
		a.s.appendMem(EventPingState, a.s.clock(), p)
		return p, nil
	}
	if p.Pong == nil && !pending {
		return p, errState{"this Ping has no Pong to withdraw"}
	}
	d, _, _ := a.node.Member(owner)
	pongID := pend.PongID
	if !pending {
		a.mu.Lock()
		pongID = a.sentIDs[id].pongID
		a.mu.Unlock()
	}
	if !a.node.Send(owner, &wire.Withdraw{PingID: id, PongID: pongID}) {
		return p, errUnreachable{d.Name}
	}
	return p, nil
}

// onWithdraw takes back a Pong another Device sent, if that Device sent it
// and it is not yet delivered, and tells it either way.
func (a *across) onWithdraw(from string, m wire.Message) {
	w := m.(*wire.Withdraw)
	a.mu.Lock()
	ap, ok := a.applied[w.PingID]
	a.mu.Unlock()
	reply := wire.Delivery{PingID: w.PingID, PongID: w.PongID, State: wire.Sent, Reason: withdrawn}
	switch {
	case !ok || ap.from != from || ap.pongID != w.PongID:
		reply.Reason = "only the Device that sent a Pong can withdraw it"
	default:
		if _, err := a.s.unsend(w.PingID); err != nil {
			reply.Reason = err.Error()
		} else {
			a.dropApplied(w.PingID)
		}
	}
	a.node.Send(from, &reply)
}

// withdrawn is the reason a delivery report gives when a Pong was taken back.
const withdrawn = "withdrawn"

// forwardAgain asks the computer holding the Ping to send its undelivered
// Pong again, or to keep it unsent.
func (a *across) forwardAgain(id, again string) (Ping, error) {
	p, err := a.ping(id)
	if err != nil {
		return p, err
	}
	a.mu.Lock()
	owner := a.owner[id]
	a.mu.Unlock()
	d, _, _ := a.node.Member(owner)
	if p.Pong == nil || !needsHuman(p.Pong.Delivery) {
		return p, errState{"only an undelivered Pong, or one that may not have arrived, can be sent again or left"}
	}
	if _, err := a.node.SendOrQueue(owner, "again:"+id, &wire.Pong{PingID: id, PongID: randomID(), Again: again}); err != nil {
		return p, errUnreachable{d.Name}
	}
	return p, nil
}

func (a *across) onDraft(from string, m wire.Message) {
	d := m.(*wire.Draft)
	if _, mine := a.s.getLocal(d.PingID); !mine && !a.has(d.PingID) {
		return
	}
	a.s.setDraft(d.PingID, d.Draft, false)
}

// forwardEnd handles hand-back, withdraw, dismiss and answered-in-harness
// on another computer's Ping: only dismissing a Note travels; the rest
// happen at the computer running the Session.
func (a *across) forwardEnd(id, action string) (Ping, error) {
	p, err := a.ping(id)
	if err != nil {
		return p, err
	}
	a.mu.Lock()
	owner := a.owner[id]
	a.mu.Unlock()
	d, _, _ := a.node.Member(owner)
	if p.Thought {
		return p, errThought
	}
	if action != "dismiss" {
		return p, errState{"this Ping is on " + d.Name + ": do this there, at the computer running the Session"}
	}
	if !p.IsNote {
		return p, errState{"only a Note can be dismissed: send a Pong, or hand the Ping back to its Harness"}
	}
	if !a.node.Send(owner, &wire.Dismiss{PingID: id}) {
		return p, errUnreachable{d.Name}
	}
	p.State = StateDismissed
	return p, nil
}

func (a *across) onDismiss(from string, m wire.Message) {
	d := m.(*wire.Dismiss)
	a.s.end(d.PingID, StateDismissed, func(p Ping) error {
		if !p.IsNote {
			return errState{"only a Note can be dismissed"}
		}
		return nil
	}, nil)
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
