package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// Session states.
const (
	SessionWorking = "working"
	SessionWaiting = "waiting" // waiting on a Pong
	SessionIdle    = "idle"
	SessionEnded   = "ended"
	SessionFailed  = "failed" // recorded by an earlier Pong whose own run of it ended with an error
)

// Who started a Session.
const (
	StartedByHuman  = "human"  // the human, in the Harness
	StartedByEngine = "engine" // recorded by an earlier Pong that ran Sessions itself; nothing sets it now
)

// EventSessionChanged carries the full Session when one is registered or changed.
const EventSessionChanged = "session.changed"

// SessionRef names a Session: its Harness and the Harness's own identifier.
type SessionRef struct {
	Harness string `json:"harness"`
	ID      string `json:"id"`
}

// Session is one conversation in a Harness, as Pong records it.
type Session struct {
	Harness string `json:"harness"`
	ID      string `json:"id"` // the Harness's own identifier
	Folder  string `json:"folder,omitempty"`
	Project string `json:"project,omitempty"` // the Project's id
	Title   string `json:"title,omitempty"`
	// Summary is the agent's latest line or two on where the Session stands,
	// from its last Ping or Note that gave one.
	Summary string `json:"summary,omitempty"`
	// FirstPrompt is the prompt the Session began on, from Pong or from the
	// human in the Harness: the first one recorded is kept.
	FirstPrompt string `json:"first_prompt,omitempty"`
	StartedBy   string `json:"started_by"`
	State       string `json:"state"`
	// AwaitsOwnWork is true while the Session is working only because its
	// last turn ended on a Note saying it waits on work it started, such as a
	// background subagent or command. Its Harness shows it running until that
	// work reports; the Engine does not take the ended turn as over
	// (sessions_gone.go). Any later state given clears it.
	AwaitsOwnWork bool        `json:"awaits_own_work,omitempty"`
	ContinuedFrom *SessionRef `json:"continued_from,omitempty"`
	Context       *Context    `json:"context,omitempty"`
	TurnStartedAt *time.Time  `json:"turn_started_at,omitempty"` // when its current turn began
	ResumeCommand string      `json:"resume_command,omitempty"`  // resumes it in a terminal; from the Runner or the Harness's hooks
	// AppCommand brings it forward in the Harness's own app, when it runs
	// there; from the Harness's hooks.
	AppCommand string `json:"app_command,omitempty"`
	// AppName is what the app AppCommand opens is called, so an interface can
	// say where the Session opens.
	AppName string `json:"app_name,omitempty"`
	// PermissionMode is the Harness's own name for how the Session last
	// settled tool calls (Claude Code: "default", "auto", ...), when it says.
	PermissionMode string `json:"permission_mode,omitempty"`
	// PID is the Harness process the Session runs in, when its hooks say;
	// the Engine ends the Session once that process is gone (sessions_gone.go).
	PID int `json:"pid,omitempty"`
	// Transcript is the file where the Harness writes the Session, when its
	// hooks say; it tells a turn that ended unseen (sessions_gone.go).
	Transcript string `json:"transcript,omitempty"`
	// Stop is the stop asked for the turn it is in or was last in (stop.go).
	Stop      *Stop     `json:"stop,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegisterSession is the body of PUT /sessions: it registers a Session or
// changes one already known. Absent fields stay as they are.
type RegisterSession struct {
	Harness        string      `json:"harness"`
	ID             string      `json:"id"`
	Folder         string      `json:"folder,omitempty"`
	Name           string      `json:"name,omitempty"`         // the name the Harness gives the Session
	FirstPrompt    string      `json:"first_prompt,omitempty"` // its first line is the title when there is no name
	Summary        string      `json:"summary,omitempty"`      // replaces the Session's summary
	StartedBy      string      `json:"started_by,omitempty"`
	State          string      `json:"state,omitempty"`
	ContinuedFrom  *SessionRef `json:"continued_from,omitempty"`
	Context        *Context    `json:"context,omitempty"` // each part stated replaces that part
	ResumeCommand  string      `json:"resume_command,omitempty"`
	PermissionMode string      `json:"permission_mode,omitempty"`
	AppCommand     string      `json:"app_command,omitempty"`
	AppName        string      `json:"app_name,omitempty"`
	PID            int         `json:"pid,omitempty"` // the Harness process it runs in
	Transcript     string      `json:"transcript,omitempty"`
	// NewTurn begins a turn now even when the Session is working already,
	// as when a Pong wakes it. Otherwise a turn begins only when it becomes
	// working, and a prompt folded into a running turn leaves its start.
	NewTurn bool `json:"new_turn,omitempty"`
	// AwaitsOwnWork, with a state, says the Session's turn ended waiting on
	// work it started. A state given without it clears it.
	AwaitsOwnWork bool `json:"awaits_own_work,omitempty"`
}

// Context is how full a Session's context window is, and the figures behind
// it, as its Harness states them. Every part is absent when the Harness does
// not state it; none is estimated.
type Context struct {
	UsedFraction *float64 `json:"used_fraction,omitempty"` // 0 to 1
	Tokens       *int64   `json:"tokens,omitempty"`
	Window       *int64   `json:"window,omitempty"`
	CostUSD      *float64 `json:"cost_usd,omitempty"`
	Model        string   `json:"model,omitempty"`
}

// mergeContext returns was with each part n states replacing that part, so
// figures from two sources of one Harness (a transcript and a status line)
// do not wipe each other.
func mergeContext(was, n *Context) *Context {
	if was == nil {
		return n
	}
	c := *was
	if n.UsedFraction != nil {
		c.UsedFraction = n.UsedFraction
	}
	if n.Tokens != nil {
		c.Tokens = n.Tokens
	}
	if n.Window != nil {
		c.Window = n.Window
	}
	if n.CostUSD != nil {
		c.CostUSD = n.CostUSD
	}
	if n.Model != "" {
		c.Model = n.Model
	}
	return &c
}

func (n RegisterSession) validate() string {
	if n.ID == "" {
		return "id is required: pass the Harness's own identifier for the Session"
	}
	switch n.StartedBy {
	case "", StartedByHuman, StartedByEngine:
	default:
		return `started_by is "` + n.StartedBy + `": use "human", "engine", or leave it out`
	}
	switch n.State {
	case "", SessionWorking, SessionWaiting, SessionIdle, SessionEnded, SessionFailed:
	default:
		return `state is "` + n.State + `": use "working", "waiting", "idle", "ended" or "failed"`
	}
	if n.ContinuedFrom != nil && n.ContinuedFrom.ID == "" {
		return "continued_from needs the id of the Session this one continues"
	}
	if c := n.Context; c != nil && c.UsedFraction != nil && (*c.UsedFraction < 0 || *c.UsedFraction > 1) {
		return "context.used_fraction must be between 0 and 1"
	}
	return ""
}

func (s *projects) session(harness, id string) (Session, error) {
	var out Session
	var body string
	err := s.db.QueryRow(`SELECT body FROM sessions WHERE harness = ? AND id = ?`, harness, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return out, errNotFound
	}
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal([]byte(body), &out)
}

// register records n, creating the Session if it is not yet known.
func (s *projects) register(n RegisterSession) (Session, error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	return s.registerLocked(n)
}

func (s *projects) registerLocked(n RegisterSession) (Session, error) {
	sess, err := s.session(n.Harness, n.ID)
	isNew := errors.Is(err, errNotFound)
	if err != nil && !isNew {
		return sess, err
	}
	now := time.Now().UTC()
	if isNew {
		sess = Session{Harness: n.Harness, ID: n.ID, StartedBy: StartedByHuman, State: SessionWorking, CreatedAt: now}
	}
	if n.Folder != "" && n.Folder != sess.Folder {
		sess.Folder = n.Folder
		p, err := s.forFolder(n.Folder)
		if err != nil {
			return sess, err
		}
		sess.Project = p.ID
	}
	if n.Name != "" {
		sess.Title = n.Name
	} else if sess.Title == "" && strings.TrimSpace(n.FirstPrompt) != "" {
		sess.Title, _, _ = strings.Cut(strings.TrimSpace(n.FirstPrompt), "\n")
		sess.Title = strings.TrimSpace(sess.Title)
	}
	if s := strings.TrimSpace(n.Summary); s != "" {
		sess.Summary = s
	}
	if p := strings.TrimSpace(n.FirstPrompt); p != "" && sess.FirstPrompt == "" {
		sess.FirstPrompt = p
	}
	if n.Transcript != "" {
		sess.Transcript = n.Transcript
	}
	if n.StartedBy != "" {
		sess.StartedBy = n.StartedBy
	}
	if n.State == SessionWorking && (isNew || n.NewTurn || sess.State != SessionWorking) || isNew && n.State == "" {
		sess.TurnStartedAt = &now
	}
	if n.State != "" {
		sess.State = n.State
		sess.AwaitsOwnWork = n.AwaitsOwnWork
	}
	if n.ContinuedFrom != nil {
		sess.ContinuedFrom = n.ContinuedFrom
	}
	if n.Context != nil {
		sess.Context = mergeContext(sess.Context, n.Context)
	}
	if n.ResumeCommand != "" {
		sess.ResumeCommand = n.ResumeCommand
	}
	if n.PermissionMode != "" {
		sess.PermissionMode = n.PermissionMode
	}
	if n.AppCommand != "" {
		sess.AppCommand = n.AppCommand
	}
	if n.AppName != "" {
		sess.AppName = n.AppName
	}
	if n.PID > 0 {
		sess.PID = n.PID
	}
	if stopOver(&sess, n.State) {
		os.Remove(StopMark(s.dir, sess.ID))
	}
	switch n.State {
	case SessionWorking, SessionEnded, SessionFailed:
		os.Remove(StoppedMark(s.dir, sess.ID)) // nothing listens after a stop any more
	}
	switch n.State {
	case SessionIdle, SessionEnded, SessionFailed:
		// The turn is over: a Ping it promised will not come.
		if err := s.pings.noMoreComing(sess.Harness, sess.ID, false); err != nil {
			return sess, err
		}
	}
	if err := s.put(&sess); err != nil {
		return sess, err
	}
	if sess.Project != "" {
		if p, err := s.get(sess.Project); err == nil && p.LastHarness != sess.Harness {
			_, err = s.update(p.ID, func(p *Project) error { p.LastHarness = sess.Harness; return nil })
			return sess, err
		}
	}
	return sess, nil
}

// put stores sess as changed now and says so on the event stream. A Session
// registers at every tool call, so one that did not change is neither
// written nor told: it keeps its stored time. The caller holds smu.
func (s *projects) put(sess *Session) error {
	if old, err := s.session(sess.Harness, sess.ID); err == nil && sameApartFromTime(old, *sess, func(x *Session) { x.UpdatedAt = time.Time{} }) {
		sess.UpdatedAt = old.UpdatedAt
		return nil
	}
	sess.UpdatedAt = time.Now().UTC()
	body, _ := json.Marshal(sess)
	if _, err := s.db.Exec(`INSERT INTO sessions (harness, id, body) VALUES (?,?,?)
		ON CONFLICT (harness, id) DO UPDATE SET body = excluded.body`, sess.Harness, sess.ID, string(body)); err != nil {
		return err
	}
	return s.pings.appendEvent(EventSessionChanged, sess.UpdatedAt, sess)
}

// sessionForPing returns the Ping's Session, registering it if unknown and
// taking the Ping's summary as the Session's, and the Ping's Harness process
// when the Session has none.
func (s *projects) sessionForPing(p Ping) (Session, error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	sess, err := s.session(p.Harness, p.Session)
	if errors.Is(err, errNotFound) || (err == nil && sess.Folder == "" && p.Folder != "") ||
		(err == nil && p.Summary != "" && p.Summary != sess.Summary) ||
		(err == nil && p.PID > 0 && sess.PID == 0) {
		n := RegisterSession{Harness: p.Harness, ID: p.Session, Folder: p.Folder, Summary: p.Summary}
		if sess.PID == 0 {
			n.PID = p.PID // a hook's own word on the process wins
		}
		return s.registerLocked(n)
	}
	return sess, err
}

func (s *projects) sessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT body FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var body string
		var x Session
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Thread is a Session's Pings, with their Pongs, oldest first.
type Thread struct {
	Session Session `json:"session"`
	Pings   []Ping  `json:"pings"`
	// Remote is set on a Thread fetched from another computer.
	Remote bool `json:"remote,omitempty"`
	// TakesText is set when text written in the Thread has somewhere to go
	// with no Ping waiting (thread_text.go). Shown by the local API only.
	TakesText bool `json:"takes_text,omitempty"`
	// StopsTurns is set when the Session's Harness can stop a turn, so an
	// interface offers Stop while it works (stop.go). Shown, never stored.
	StopsTurns bool `json:"stops_turns,omitempty"`
}

func (s *projects) thread(harness, id string) (Thread, error) {
	sess, err := s.session(harness, id)
	if err != nil {
		return Thread{}, err
	}
	rows, err := s.db.Query(`SELECT body FROM pings
		WHERE json_extract(body, '$.session') = ? AND coalesce(json_extract(body, '$.harness'), '') = ?
		ORDER BY created_at, rowid`, id, harness)
	if err != nil {
		return Thread{}, err
	}
	defer rows.Close()
	t := Thread{Session: sess, Pings: []Ping{}}
	for rows.Next() {
		p, err := scanPing(rows)
		if err != nil {
			return t, err
		}
		t.Pings = append(t.Pings, p)
	}
	return t, rows.Err()
}
