package engine

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ping states.
const (
	StateWaiting           = "waiting"             // awaiting a Pong; for a Note, not yet dismissed
	StateAnswered          = "answered"            // a Pong arrived
	StateHandedBack        = "handed_back"         // the human handed it back to its Harness
	StateAnsweredInHarness = "answered_in_harness" // the human answered in the Harness instead
	StateWithdrawn         = "withdrawn"           // the agent withdrew it
	StateDismissed         = "dismissed"           // a Note the human dismissed
)

// What a Ping approves, when it approves rather than chooses.
const (
	ApprovesPlan   = "plan"
	ApprovesAction = "action"
)

// Answer is one answer a Ping offers.
type Answer struct {
	Label      string `json:"label"`
	LeadsTo    string `json:"leads_to,omitempty"`
	HardToUndo bool   `json:"hard_to_undo,omitempty"`
}

// Pong is the human's reply to a Ping: any mix of one chosen answer, several
// ticked answers and typed text, plus whether it should start a new Session.
type Pong struct {
	Answer     string    `json:"answer,omitempty"`
	Answers    []string  `json:"answers,omitempty"`
	Text       string    `json:"text,omitempty"`
	NewSession bool      `json:"new_session,omitempty"`
	SentAt     time.Time `json:"sent_at"`
	// Delivery is where the Pong stands on its way to the Harness: one of
	// the Delivery constants.
	Delivery    string     `json:"delivery,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	// NotDelivered says why an undelivered or unconfirmed Pong is back with
	// the human.
	NotDelivered string `json:"not_delivered,omitempty"`
	// Again marks a Pong the human sent again into its own Session that has
	// not ended: it is kept for the Session's hooks, as text written in its
	// Thread is (thread_text.go).
	Again bool `json:"again,omitempty"`
}

// Ping is one thing a Session sends the human. Its answers come with the
// recommended one first; nothing else marks it.
type Ping struct {
	ID           string   `json:"id"`
	Session      string   `json:"session"`           // the Harness's own identifier for the Session
	Harness      string   `json:"harness,omitempty"` // with Session, identifies the Session
	Project      string   `json:"project,omitempty"` // the Project's id
	Folder       string   `json:"folder"`
	Question     string   `json:"question"`
	Answers      []Answer `json:"answers"`
	AllowSeveral bool     `json:"allow_several,omitempty"`
	Approves     string   `json:"approves,omitempty"`
	// Call is the tool call an Allow to this Ping allows (allowed.go).
	Call    *Call  `json:"call,omitempty"`
	Context string `json:"context,omitempty"`
	Why     string `json:"why,omitempty"`
	// NotVerified is what the agent says it did not check in the finished
	// work it hands over.
	NotVerified string `json:"not_verified,omitempty"`
	// Summary is the agent's line on where its Session stands as it sends
	// this Ping; it becomes the Session's summary.
	Summary     string `json:"summary,omitempty"`
	MoreComing  bool   `json:"more_coming,omitempty"`
	BlocksAgent bool   `json:"blocks_agent,omitempty"`
	// Finished is set when the agent said the Ping hands over finished work:
	// the apps draw the Finished mark on it.
	Finished bool `json:"finished,omitempty"`
	IsNote   bool `json:"is_note,omitempty"`
	// Unasked is set on the record of text the human wrote in the Thread
	// while no Ping waited: it has no question, only its Pong (thread_text.go).
	Unasked bool `json:"unasked,omitempty"`
	// Line marks one of Pong's own lines in the Thread, such as the one that
	// says the human stopped a turn (stop.go): the words are Question, it
	// carries no answers and never waits.
	Line bool `json:"line,omitempty"`
	// Thought marks one sentence the agent wrote with send_thought: something
	// it found, decided or changed. Like a Line its words are
	// Question, it carries no answers and is dismissed from the start; unlike
	// a Line, the agent wrote it.
	Thought bool `json:"thought,omitempty"`
	// First is set on a Ping that is its Session's first: see firstPing.
	First     bool       `json:"first,omitempty"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Pong      *Pong      `json:"pong,omitempty"`
	Resources []Resource `json:"resources,omitempty"`
	Tags      []Tag      `json:"tags,omitempty"`

	// Shown by the local API only, never stored:
	// Computer is the computer holding the Ping, set once the set has more
	// than one computer.
	Computer *Computer `json:"computer,omitempty"`
	// Remote is set on another computer's Ping: hand back, answering in
	// the Harness and opening it in the Harness happen only there.
	Remote bool `json:"remote,omitempty"`
	// Draft is the Pong being written on any Device, as that interface sent it.
	Draft json.RawMessage `json:"draft,omitempty"`
	// Lost is a Pong sent from this Device that arrived after another one
	// won, with what the computer holding the Ping said.
	Lost *Lost `json:"lost,omitempty"`
}

// Lost is a Pong that did not apply because another came first.
type Lost struct {
	Pong   Pong   `json:"pong"`
	Reason string `json:"reason"`
}

// Computer is the computer a Ping is on, as the local API shows it.
type Computer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Reachable bool   `json:"reachable"`
}

// Event kinds.
const (
	EventPingCreated = "ping.created" // a new Ping or Note
	EventPingPonged  = "ping.ponged"  // a Pong arrived
	EventPingState   = "ping.state"   // any other change of state
)

// Event is one entry in the event stream. Data holds the full Ping for the
// ping.* kinds.
type Event struct {
	Seq  int64           `json:"seq"`
	Kind string          `json:"kind"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

var errNotFound = errors.New("not found")

// errState is a change the Ping's current state or kind does not allow.
type errState struct{ msg string }

func (e errState) Error() string { return e.msg }

// pings stores Pings and the event log, and wakes those waiting on either.
type pings struct {
	db      *sql.DB
	diag    string // diagnostics folder; "" keeps no journal
	mu      sync.Mutex
	waiters map[string]chan struct{} // closed when the Ping leaves waiting
	events  chan struct{}            // closed and replaced on every new event
	closing chan struct{}            // closed when the Engine stops, ending waits
	place   func(*Ping) error        // sets the Ping's Project, registering its Session; may be nil
	dir     string                   // data directory; Resources are kept under it
	// onPong runs before a Pong is recorded; an error refuses the Pong and
	// undo reverts what it did when the Pong is then refused. after, when
	// set, takes over delivering the recorded Pong and returns the Ping as it
	// then stands. May be nil.
	onPong func(Ping, SendPong) (undo func(), after func(Ping) Ping, err error)
	now    func() time.Time // the clock; tests may set it
	// holders counts the open waits holding each Ping's prompt; wasHeld
	// marks the Pings one ever held. See held.
	holders map[string]int
	wasHeld map[string]bool
	grace   time.Duration // see DefaultDeliveryGrace
	// onUnheld decides what becomes of a sent Pong once grace passed with
	// nothing holding it. May be nil: it is then undelivered.
	onUnheld func(Ping)

	// seq is the last event's seq, across the store and mem; s.mu guards it.
	seq int64
	// mem holds events about other computers' Pings: never written to the
	// store, gone when the Engine stops. Their seqs interleave with
	// the store's.
	mem []Event
	// set, when this Engine is in a set of Devices, merges other
	// computers' Pings in and marks every Ping with its computer. May be nil.
	set *across
	// drafts are draft Pongs by Ping id, from any Device: in memory only.
	drafts map[string]json.RawMessage
	// reserved is the highest seq saved in seqFile.
	reserved int64
	// again sends an undelivered Pong again (the starter's sendAgain). May be nil.
	again func(id string, in SendAgain) (Ping, error)
}

// maxMem bounds the in-memory events kept for replay.
const maxMem = 10000

// FollowUpWindow is how soon after its Session's last Ping was answered a
// new Ping counts as a follow-up rather than a first Ping.
const FollowUpWindow = 5 * time.Minute

// firstPing reports whether a Ping arriving at at is its Session's first,
// given the Session's earlier Pings: none of them awaits a Pong, and either
// there is none or the last one left waiting more than FollowUpWindow ago.
// Notes do not count.
func firstPing(earlier []Ping, at time.Time) bool {
	var last *Ping
	for i, e := range earlier {
		if e.IsNote {
			continue
		}
		if e.State == StateWaiting {
			return false
		}
		if last == nil || e.CreatedAt.After(last.CreatedAt) {
			last = &earlier[i]
		}
	}
	return last == nil || at.Sub(last.UpdatedAt) > FollowUpWindow
}

// session returns the Pings of one Session.
func (s *pings) session(harness, session string) ([]Ping, error) {
	rows, err := s.db.Query(`SELECT body FROM pings WHERE json_extract(body, '$.session') = ? AND coalesce(json_extract(body, '$.harness'), '') = ?`, session, harness)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ping
	for rows.Next() {
		p, err := scanPing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *pings) clock() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func newPings(db *sql.DB) (*pings, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS pings (
		id TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		blocks_agent INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		body TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS events (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		at TEXT NOT NULL,
		data TEXT NOT NULL
	)`)
	if err != nil {
		return nil, err
	}
	if err := indexPingEvents(db); err != nil {
		return nil, err
	}
	if err := newAllowed(db); err != nil {
		return nil, err
	}
	ps := &pings{db: db, waiters: map[string]chan struct{}{}, events: make(chan struct{}), closing: make(chan struct{}),
		holders: map[string]int{}, wasHeld: map[string]bool{}, grace: DefaultDeliveryGrace}
	var n sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n); err != nil {
		return nil, err
	}
	ps.seq = n.Int64
	return ps, nil
}

// indexPingEvents gives each event about a Ping that Ping's id in a column of
// its own, with an index, so a Ping's events are found without reading every
// event. Stores from before the column get it filled from each event once.
func indexPingEvents(db *sql.DB) error {
	if _, err := db.Exec(`SELECT ping_id FROM events LIMIT 0`); err == nil {
		return nil
	}
	_, err := db.Exec(`ALTER TABLE events ADD COLUMN ping_id TEXT;
		UPDATE events SET ping_id = json_extract(data, '$.id') WHERE kind LIKE 'ping.%';
		CREATE INDEX IF NOT EXISTS events_ping ON events(ping_id) WHERE ping_id IS NOT NULL;
		CREATE INDEX IF NOT EXISTS events_kind_at ON events(kind, at)`)
	return err
}

// save writes p and appends an event of kind carrying it, in one
// transaction. The caller holds s.mu.
func (s *pings) save(p Ping, kind string, insert bool) error {
	body, _ := json.Marshal(p)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if insert {
		_, err = tx.Exec(`INSERT INTO pings (id, state, blocks_agent, created_at, body) VALUES (?,?,?,?,?)`,
			p.ID, p.State, p.BlocksAgent, p.CreatedAt.Format(time.RFC3339Nano), string(body))
	} else {
		_, err = tx.Exec(`UPDATE pings SET state = ?, body = ? WHERE id = ?`, p.State, string(body), p.ID)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO events (seq, kind, at, data, ping_id) VALUES (?,?,?,?,?)`, s.seq+1, kind, p.UpdatedAt.Format(time.RFC3339Nano), string(body), p.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.seq++
	s.diagnose(s.seq, kind, p.UpdatedAt, body)
	close(s.events)
	s.events = make(chan struct{})
	if s.set != nil {
		s.set.local(kind, p)
	}
	return nil
}

func (s *pings) create(p Ping) (Ping, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return p, err
	}
	p.ID = hex.EncodeToString(buf)
	p.State = StateWaiting
	if p.Thought {
		p.State = StateDismissed // never in the Queue
	}
	if p.Answers == nil {
		p.Answers = []Answer{}
	}
	if s.place != nil {
		if err := s.place(&p); err != nil {
			return p, err
		}
	}
	// Created after place, so a Session this Ping registers starts its turn
	// before the Ping and counts it as sent in that turn.
	p.CreatedAt = s.clock()
	p.UpdatedAt = p.CreatedAt
	s.mu.Lock()
	defer s.mu.Unlock()
	if !p.IsNote {
		earlier, err := s.session(p.Harness, p.Session)
		if err != nil {
			return p, err
		}
		p.First = firstPing(earlier, p.CreatedAt)
	}
	return p, s.save(p, EventPingCreated, true)
}

func scanPing(row interface{ Scan(...any) error }) (Ping, error) {
	var p Ping
	var body string
	if err := row.Scan(&body); err != nil {
		return p, err
	}
	return p, json.Unmarshal([]byte(body), &p)
}

// getLocal reports whether Ping id is this Engine's own.
func (s *pings) getLocal(id string) (Ping, bool) {
	p, err := s.get(id)
	return p, err == nil
}

func (s *pings) get(id string) (Ping, error) {
	p, err := scanPing(s.db.QueryRow(`SELECT body FROM pings WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	return p, err
}

// list returns Pings in the given state (all when empty) in the Queue's
// order: those that block an agent, then those from the Session of the last
// Pong the human sent, then those from that Session's Project, then the
// rest; oldest first within each. Nothing an agent sends sets the place.
func (s *pings) list(state string) ([]Ping, error) {
	ps, err := s.listOldest(state)
	if err != nil {
		return nil, err
	}
	last, ok, err := s.lastPonged()
	if err != nil {
		return nil, err
	}
	if s.set != nil {
		// Other computers' Pings join the Queue, and the last Pong on any
		// Device decides which Session and Project come first.
		ps = append(ps, s.set.list(state)...)
		sort.SliceStable(ps, func(i, j int) bool {
			if ps[i].BlocksAgent != ps[j].BlocksAgent {
				return ps[i].BlocksAgent
			}
			return ps[i].CreatedAt.Before(ps[j].CreatedAt)
		})
		if r, rok := s.set.lastPonged(); rok && (!ok || r.Pong.SentAt.After(last.Pong.SentAt)) {
			last, ok = r, true
		}
	}
	if !ok {
		return ps, nil
	}
	rank := func(p Ping) int {
		switch {
		case p.BlocksAgent:
			return 0
		case p.Session == last.Session && p.Harness == last.Harness:
			return 1
		case p.Project != "" && p.Project == last.Project:
			return 2
		}
		return 3
	}
	sort.SliceStable(ps, func(i, j int) bool { return rank(ps[i]) < rank(ps[j]) })
	return ps, nil
}

// lastPonged is the Ping that carries the last Pong the human sent.
func (s *pings) lastPonged() (Ping, bool, error) {
	rows, err := s.db.Query(`SELECT body FROM pings WHERE json_extract(body, '$.pong') IS NOT NULL`)
	if err != nil {
		return Ping{}, false, err
	}
	defer rows.Close()
	var out Ping
	ok := false
	for rows.Next() {
		p, err := scanPing(rows)
		if err != nil {
			return out, false, err
		}
		if !ok || p.Pong.SentAt.After(out.Pong.SentAt) {
			out, ok = p, true
		}
	}
	return out, ok, rows.Err()
}

// listOldest is list with blocking Pings first, then oldest first.
func (s *pings) listOldest(state string) ([]Ping, error) {
	rows, err := s.db.Query(`SELECT body FROM pings WHERE ? = '' OR state = ? ORDER BY blocks_agent DESC, created_at`, state, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ping{}
	for rows.Next() {
		p, err := scanPing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// end moves a waiting Ping to state, after check approves it, and wakes
// everyone waiting on it.
func (s *pings) end(id, state string, check func(Ping) error, pong *Pong) (Ping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(id)
	if err != nil {
		return p, err
	}
	if p.Thought {
		return p, errThought
	}
	if p.State != StateWaiting {
		return p, errState{"this Ping is already " + p.State + "; only a waiting Ping can change"}
	}
	if err := check(p); err != nil {
		return p, err
	}
	p.State = state
	p.UpdatedAt = s.clock()
	kind := EventPingState
	if pong != nil {
		pong.SentAt = p.UpdatedAt
		pong.Delivery = DeliverySent
		p.Pong = pong
		kind = EventPingPonged
	}
	if err := s.save(p, kind, false); err != nil {
		return p, err
	}
	if err := s.allowCall(p, pong); err != nil {
		return p, err
	}
	if ch, ok := s.waiters[id]; ok {
		close(ch)
		delete(s.waiters, id)
	}
	return p, nil
}

// noMoreComing takes more_coming back from the Pings of one Session, or of
// every Session when all is set: the Ping they promised will not come, so no
// interface goes on waiting for it. It runs when a Session's turn ends, when
// a Session ends, and when the Engine starts, which cannot know what a
// Session tried to send while it was not running.
func (s *pings) noMoreComing(harness, session string, all bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ps []Ping
	var err error
	if all {
		ps, err = s.listOldest("")
	} else {
		ps, err = s.session(harness, session)
	}
	if err != nil {
		return err
	}
	for _, p := range ps {
		if !p.MoreComing {
			continue
		}
		p.MoreComing = false
		p.UpdatedAt = s.clock()
		if err := s.save(p, EventPingState, false); err != nil {
			return err
		}
	}
	return nil
}

// waitOnce returns the Ping once it leaves waiting, or as it stands after
// hold. A Note returns at once: it never blocks. holds says the wait holds
// the Ping's prompt (see held). See wait.
func (s *pings) waitOnce(id string, hold time.Duration, done <-chan struct{}, holds bool) (Ping, error) {
	if holds {
		defer s.held(id)()
	}
	s.mu.Lock()
	p, err := s.get(id)
	if err != nil || p.State != StateWaiting || p.IsNote {
		s.mu.Unlock()
		return p, err
	}
	ch, ok := s.waiters[id]
	if !ok {
		ch = make(chan struct{})
		s.waiters[id] = ch
	}
	s.mu.Unlock()
	t := time.NewTimer(hold)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-done:
	case <-s.closing:
	}
	return s.get(id)
}

// eventsAfter returns events after seq, and a channel closed when another
// arrives.
func (s *pings) eventsAfter(seq int64) ([]Event, <-chan struct{}, error) {
	s.mu.Lock()
	next := s.events
	var mem []Event
	for _, e := range s.mem {
		if e.Seq > seq {
			mem = append(mem, e)
		}
	}
	upTo := s.seq // later events, in the store or mem, come on the next call
	s.mu.Unlock()
	rows, err := s.db.Query(`SELECT seq, kind, at, data FROM events WHERE seq > ? AND seq <= ? ORDER BY seq`, seq, upTo)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at, data string
		if err := rows.Scan(&e.Seq, &e.Kind, &at, &data); err != nil {
			return nil, nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	if len(mem) > 0 {
		out = append(out, mem...)
		sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	}
	return out, next, rows.Err()
}

func (s *pings) lastSeq() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq, nil
}

// seqFile keeps the highest seq given to an in-memory event, so that after a
// restart new events never reuse a seq a client already saw.
const seqFile = "events.seq"

// saveSeq writes s.seq; s.mu is held.
func (s *pings) saveSeq() {
	if s.dir == "" {
		return
	}
	select {
	case <-s.closing:
		return
	default:
	}
	p := filepath.Join(s.dir, seqFile)
	if os.WriteFile(p+".tmp", []byte(strconv.FormatInt(s.reserved, 10)), 0o600) == nil {
		os.Rename(p+".tmp", p)
	}
}

// loadSeq raises s.seq to the highest seq saved by saveSeq.
func (s *pings) loadSeq() {
	b, err := os.ReadFile(filepath.Join(s.dir, seqFile))
	if err != nil {
		return
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && n > s.seq {
		s.seq = n
	}
}

// appendMem appends an event about another computer's Ping, in memory only,
// and wakes the stream.
func (s *pings) appendMem(kind string, at time.Time, data any) {
	b, _ := json.Marshal(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.mem = append(s.mem, Event{Seq: s.seq, Kind: kind, At: at, Data: b})
	if s.seq >= s.reserved {
		// Reserve seqs a thousand at a time, so the file is written rarely
		// and a restart starts above every seq handed out.
		s.reserved = s.seq + 1000
		s.saveSeq()
	}
	if len(s.mem) > maxMem {
		s.mem = append([]Event{}, s.mem[len(s.mem)-maxMem:]...)
	}
	close(s.events)
	s.events = make(chan struct{})
}

// appendEvent appends an event of kind carrying data and wakes the stream.
func (s *pings) appendEvent(kind string, at time.Time, data any) error {
	b, _ := json.Marshal(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`INSERT INTO events (seq, kind, at, data) VALUES (?,?,?,?)`, s.seq+1, kind, at.Format(time.RFC3339Nano), string(b)); err != nil {
		return err
	}
	s.seq++
	s.diagnose(s.seq, kind, at, b)
	close(s.events)
	s.events = make(chan struct{})
	return nil
}

// unsend takes back a Pong that is sent but not yet delivered: the Ping
// waits again. Only a sent Pong can be taken back.
func (s *pings) unsend(id string) (Ping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(id)
	if err != nil {
		return p, err
	}
	if p.State != StateAnswered || p.Pong == nil || p.Pong.Delivery != DeliverySent {
		return p, errState{"only a Pong not yet delivered can be withdrawn"}
	}
	if p.Unasked {
		// No Ping to wait again: the text is kept, and no longer sent.
		p.Pong.Delivery = DeliveryLeft
	} else {
		p.State = StateWaiting
		p.Pong = nil
	}
	p.UpdatedAt = s.clock()
	if err := s.save(p, EventPingState, false); err != nil {
		return p, err
	}
	// A withdrawn Allow allows nothing.
	return p, s.unallow(p.ID)
}
