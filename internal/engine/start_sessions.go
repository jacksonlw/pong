package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StartSession is the body of POST /sessions/start: a Session to start in a
// Project (or a folder) with a first prompt.
type StartSession struct {
	Harness string `json:"harness"`
	Project string `json:"project,omitempty"` // the Project's id; its root is the folder
	Folder  string `json:"folder,omitempty"`
	Prompt  string `json:"prompt"`
	// Computer is the computer to start it on, by Device identifier; empty
	// lets Pong choose among the computers that can take it.
	Computer string `json:"computer,omitempty"`
}

// FollowUp is the body of POST /session/follow-up.
type FollowUp struct {
	Prompt string `json:"prompt"`
}

// starter opens the Sessions the human starts or continues from Pong, where
// they work (start_runner.go), and delivers what they write in a Thread.
type starter struct {
	pr      *projects
	ps      *pings
	dir     string   // data directory, where text kept for the hooks is marked
	env     []string // the human's environment, captured at Engine start
	runners map[string]Runner
	launch  func(Launch) error
	// comp places Sessions across the set of Devices; nil on its own.
	comp *computers
	hold time.Duration // how long a listen for text stays open
	name string        // this computer's name, for a stop asked through the local API
}

func newStarter(pr *projects, ps *pings, dir string, env []string, runners []Runner, launch func(Launch) error) *starter {
	if env == nil {
		env = os.Environ()
	}
	if launch == nil {
		launch = OpenLaunch
	}
	s := &starter{pr: pr, ps: ps, dir: dir, env: env, runners: map[string]Runner{}, launch: launch}
	for _, r := range runners {
		s.runners[r.Harness()] = r
	}
	pr.dir = dir
	pr.stopsTurns = func(x Session) bool { return s.stopsTurns(x.Harness) }
	pr.turnOver = func(x Session) (bool, time.Time, bool) {
		if r, ok := s.runners[x.Harness].(TurnReader); ok {
			return r.TurnOver(x.Transcript)
		}
		return false, time.Time{}, false
	}
	ps.onPong = s.onPong
	ps.onUnheld = s.onUnheld
	ps.again = s.sendAgain
	// A Thread takes text where the Harness's hooks do, and for an ended
	// Session wherever Pong can open it again.
	pr.takesText = func(x Session) bool {
		return s.takesText(x.Harness) || x.State == SessionEnded && s.runners[x.Harness] != nil
	}
	return s
}

func (s *starter) runner(harness string) (Runner, error) {
	if r := s.runners[harness]; r != nil {
		return r, nil
	}
	return nil, CannotStart(fmt.Sprintf("Pong cannot start Sessions in the Harness %q", harness))
}

// open opens l, saying why in words for the human when it cannot.
func (s *starter) open(l Launch) error {
	err := s.launch(l)
	var cs CannotStart
	if err != nil && !errors.As(err, &cs) {
		return CannotStart("Pong could not open the Session: " + err.Error())
	}
	return err
}

// start opens a new Session in folder, in a terminal, started on prompt.
// Where the Harness lets Pong choose the Session's id it is registered now;
// otherwise the Session returned has no id, and registers itself through the
// Harness's hooks.
func (s *starter) start(harness, folder, prompt string, from *SessionRef) (Session, error) {
	r, err := s.runner(harness)
	if err != nil {
		return Session{}, err
	}
	if fi, err := os.Stat(folder); err != nil || !fi.IsDir() {
		return Session{}, CannotStart("the folder " + folder + " does not exist")
	}
	if err := r.Check(s.env, folder); err != nil {
		return Session{}, err
	}
	l := r.Start(s.surface(harness), folder, prompt)
	if err := s.open(l); err != nil {
		return Session{}, err
	}
	if l.SessionID == "" {
		return Session{Harness: harness, Folder: folder, StartedBy: StartedByHuman, State: SessionWorking}, nil
	}
	return s.pr.register(RegisterSession{Harness: harness, ID: l.SessionID, Folder: folder, FirstPrompt: prompt,
		State: SessionWorking, ContinuedFrom: from, ResumeCommand: r.ResumeCommand(folder, l.SessionID)})
}

// reopen opens the ended Session sess again, in a terminal, with the Pong on
// p as its next prompt; the Pong is then delivered. When the agent that
// waited for it is gone, or the terminal cannot be opened, the Pong is
// undelivered and back with the human. Sent again by the human, a blocking
// Ping's Pong goes too, with the question it answers (againText).
func (s *starter) reopen(p Ping, sess Session, from []string, again bool) (Ping, error) {
	why := whyEnded
	if r := s.runners[sess.Harness]; r != nil && (again || !p.BlocksAgent) {
		text := pongText(SendPong{Answer: p.Pong.Answer, Answers: p.Pong.Answers, Text: p.Pong.Text})
		if p.BlocksAgent {
			text = againText(p)
		}
		err := s.open(r.Continue(s.surface(sess.Harness), sess.Folder, sess.ID, text))
		if err == nil {
			p, _, err = s.ps.deliver(p.ID, from, DeliveryDelivered, "")
			return p, err
		}
		why = err.Error()
	}
	p, _, err := s.ps.deliver(p.ID, from, DeliveryUndelivered, why)
	return p, err
}

// followUp sends text the human wrote to a Session: as the Pong on its
// finished Ping while that waits, by opening it again when it has ended, and
// otherwise by keeping it for the Harness's hooks (thread_text.go). A
// Harness whose hooks take no text takes it only on its finished Ping.
func (s *starter) followUp(ref SessionRef, prompt string) (Session, error) {
	sess, err := s.pr.session(ref.Harness, ref.ID)
	if err != nil {
		return sess, err
	}
	if sess.State == SessionEnded {
		if s.runners[sess.Harness] == nil {
			return sess, errState{"the Session has ended: start a new Session"}
		}
		p, err := s.ps.createUnasked(sess, prompt)
		if err != nil {
			return sess, err
		}
		_, err = s.reopen(p, sess, []string{DeliverySent}, false)
		return sess, err
	}
	finished, ok, err := s.waitingFinished(sess)
	if err != nil {
		return sess, err
	}
	if ok {
		p, err := s.ps.end(finished.ID, StateAnswered, func(Ping) error { return nil }, &Pong{Text: prompt})
		if err != nil {
			return sess, err
		}
		s.ps.sent(p)
		return sess, nil
	}
	if !s.takesText(sess.Harness) {
		return sess, errState{"nothing waits for a Pong in this Session: write once its turn ends"}
	}
	return sess, s.keepText(sess, prompt)
}

// waitingFinished is the Session's latest waiting Ping that does not block
// the agent: the finished Ping its process waits on.
func (s *starter) waitingFinished(sess Session) (Ping, bool, error) {
	ps, err := s.ps.list(StateWaiting)
	if err != nil {
		return Ping{}, false, err
	}
	var out Ping
	ok := false
	for _, p := range ps {
		if p.Session == sess.ID && (p.Harness == "" || p.Harness == sess.Harness) && !p.IsNote && !p.BlocksAgent &&
			(!ok || !p.CreatedAt.Before(out.CreatedAt)) {
			out, ok = p, true
		}
	}
	return out, ok, nil
}

// sessionOf is the Ping's Session. A Ping sent through Pong's MCP tools
// names its Session but may not name its Harness: the Session of that id in
// a named Harness is taken then.
func (s *starter) sessionOf(p Ping) (Session, error) {
	if p.Harness != "" {
		return s.pr.session(p.Harness, p.Session)
	}
	all, err := s.pr.sessions()
	if err != nil {
		return Session{}, err
	}
	var out Session
	found := false
	for _, x := range all {
		if x.ID == p.Session && (!found || out.Harness == "") {
			out, found = x, true
		}
	}
	if !found {
		return out, errNotFound
	}
	return out, nil
}

// onPong runs before a Pong is recorded on p. A Pong that asks for a new
// Session opens one continued from the old one; if it cannot be opened the
// error is returned and the Pong is not recorded, so the Ping still waits.
// Once recorded, after delivers it: a Pong whose Session has ended opens that
// Session again in a terminal (Pong never sends it to another Session by
// itself). Any other Pong is the
// Session's hooks' to deliver. A Pong of exactly NothingMore hands nothing
// over.
func (s *starter) onPong(p Ping, in SendPong) (undo func(), after func(Ping) Ping, err error) {
	if p.IsNote || p.BlocksAgent {
		return nil, nil, nil
	}
	sess, err := s.sessionOf(p)
	if errors.Is(err, errNotFound) {
		if !in.NewSession {
			return nil, nil, nil
		}
		sess, err = Session{Harness: p.Harness, ID: p.Session, Folder: p.Folder}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	delivered := func(p Ping) Ping {
		q, _, _ := s.ps.deliver(p.ID, []string{DeliverySent}, DeliveryDelivered, "")
		return q
	}
	switch {
	case in.NewSession:
		folder := sess.Folder
		if folder == "" {
			folder = p.Folder
		}
		if _, err := s.start(sess.Harness, folder, s.newSessionPrompt(p, in), &SessionRef{sess.Harness, sess.ID}); err != nil {
			return nil, nil, err
		}
		// Nothing to undo: an opened Session is the human's to close.
		return nil, delivered, nil
	case sess.State != SessionEnded:
		return nil, nil, nil
	case nothingMore(in):
		return nil, delivered, nil
	}
	return nil, func(p Ping) Ping {
		q, _ := s.reopen(p, sess, []string{DeliverySent}, false)
		return q
	}, nil
}

// nothingMore reports whether a Pong is exactly NothingMore, which leaves the
// Session idle.
func nothingMore(in SendPong) bool {
	return in.Answer == NothingMore && len(in.Answers) == 0 && strings.TrimSpace(in.Text) == ""
}

// pongText is the Pong as one line: the chosen and ticked answers, then the text.
func pongText(in SendPong) string {
	labels := in.Answers
	if in.Answer != "" {
		labels = append([]string{in.Answer}, labels...)
	}
	out := strings.Join(labels, ", ")
	if t := strings.TrimSpace(in.Text); t != "" {
		if out != "" {
			out += ". "
		}
		out += t
	}
	return out
}

// newSessionPrompt is the first prompt of a Session started from a Pong: the
// Pong first (its first line titles the Session), then the Ping it answers
// with its context and Resources.
func (s *starter) newSessionPrompt(p Ping, in SendPong) string {
	var b strings.Builder
	b.WriteString(pongText(in))
	b.WriteString("\n\nThis continues an earlier Session")
	if p.Session != "" {
		b.WriteString(" (" + p.Session + ")")
	}
	b.WriteString(". The line above is the human's answer, sent through Pong, to what the agent there asked:\n\n")
	b.WriteString(p.Question)
	b.WriteString("\n")
	if len(p.Answers) > 0 {
		b.WriteString("\nThe answers it offered:\n")
		for _, a := range p.Answers {
			b.WriteString("- " + a.Label)
			if a.LeadsTo != "" {
				b.WriteString(": " + a.LeadsTo)
			}
			b.WriteString("\n")
		}
	}
	if c := strings.TrimSpace(p.Context); c != "" {
		b.WriteString("\nContext:\n" + c + "\n")
	}
	if len(p.Resources) > 0 {
		b.WriteString("\nResources:\n")
		for _, r := range p.Resources {
			path := filepath.Join(s.ps.resourceDir(), r.ID)
			if r.File == "" {
				content, err := os.ReadFile(path)
				if err == nil {
					fmt.Fprintf(&b, "- %s (%s):\n%s\n", r.Title, r.Kind, strings.TrimRight(string(content), "\n"))
					continue
				}
			}
			fmt.Fprintf(&b, "- %s (%s", r.Title, r.Kind)
			if r.File != "" {
				fmt.Fprintf(&b, ", a copy of %s taken when it was sent", r.File)
			}
			fmt.Fprintf(&b, "): %s\n", path)
		}
	}
	return b.String()
}

func (s *starter) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /sessions/start", func(w http.ResponseWriter, r *http.Request) {
		var in StartSession
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a Session to start: "+err.Error())
			return
		}
		if strings.TrimSpace(in.Prompt) == "" {
			writeError(w, http.StatusBadRequest, "prompt is required: say what the Session should do")
			return
		}
		if in.Harness == "" {
			writeError(w, http.StatusBadRequest, `harness is required, such as "claude-code"`)
			return
		}
		if s.comp != nil && in.Project != "" && in.Folder == "" && (in.Computer != "" || s.comp.a.node.Computers() > 1) {
			raw, err := s.comp.startPlaced(in)
			if p, ok := asNoComputer(err); ok {
				writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "computers": p.Candidates})
				return
			}
			var un errUnreachable
			if errors.As(err, &un) {
				writeError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			if err != nil {
				writeStart(w, nil, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write(raw)
			return
		}
		folder := in.Folder
		if in.Project != "" {
			p, err := s.pr.get(in.Project)
			if err != nil {
				writeStart(w, nil, err)
				return
			}
			folder = p.Root
		}
		if folder == "" {
			writeError(w, http.StatusBadRequest, "project or folder is required: say where the Session runs")
			return
		}
		sess, err := s.start(in.Harness, folder, in.Prompt, nil)
		if err == nil {
			writeJSON(w, http.StatusCreated, sess)
			return
		}
		writeStart(w, sess, err)
	})
	ref := func(r *http.Request) SessionRef {
		q := r.URL.Query()
		return SessionRef{q.Get("harness"), q.Get("id")}
	}
	mux.HandleFunc("POST /session/follow-up", func(w http.ResponseWriter, r *http.Request) {
		var in FollowUp
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Prompt) == "" {
			writeError(w, http.StatusBadRequest, "prompt is required: say what the Session should do next")
			return
		}
		sess, err := s.followUp(ref(r), in.Prompt)
		writeStart(w, sess, err)
	})
	s.deliveryRoutes(mux)
	s.textRoutes(mux)
	s.stopRoutes(mux)
	s.surfaceRoutes(mux)
}

// writeStart writes v, or the error: 422 when the Session cannot be started.
func writeStart(w http.ResponseWriter, v any, err error) {
	var cs CannotStart
	var es errState
	switch {
	case errors.As(err, &cs):
		writeError(w, http.StatusUnprocessableEntity, cs.Error())
	case errors.As(err, &es):
		writeError(w, http.StatusConflict, es.Error())
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "no such Session or Project: check harness and id")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

// wait is waitOnce, recording a Pong it returns as delivered: waits are
// how a Session collects Pongs (its hooks and Pong's MCP tools wait).
func (s *pings) wait(id string, hold time.Duration, done <-chan struct{}, holds bool) (Ping, error) {
	p, err := s.waitOnce(id, hold, done, holds)
	if err == nil && p.Pong != nil && p.Pong.Delivery == DeliverySent {
		if q, ok, derr := s.deliver(p.ID, []string{DeliverySent}, DeliveryDelivered, ""); derr == nil && ok {
			p = q
		}
	}
	return p, err
}
