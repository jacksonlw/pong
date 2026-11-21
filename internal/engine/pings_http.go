package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// NewPing is the body of POST /pings.
type NewPing struct {
	Session      string   `json:"session"`
	Harness      string   `json:"harness,omitempty"`
	Folder       string   `json:"folder"`
	Question     string   `json:"question"`
	Answers      []Answer `json:"answers,omitempty"`
	Recommended  string   `json:"recommended,omitempty"`
	AllowSeveral bool     `json:"allow_several,omitempty"`
	Approves     string   `json:"approves,omitempty"`
	Context      string   `json:"context,omitempty"`
	Why          string   `json:"why,omitempty"`
	NotVerified  string   `json:"not_verified,omitempty"` // on finished work: what the agent did not check
	Summary      string   `json:"summary,omitempty"`      // where the Session stands now; becomes the Session's summary
	MoreComing   bool     `json:"more_coming,omitempty"`
	BlocksAgent  bool     `json:"blocks_agent,omitempty"`
	// Finished marks a Ping handing over finished work: it gets the Finished
	// mark and offers NothingMore. Pong never infers it.
	Finished bool `json:"finished,omitempty"`
	IsNote   bool `json:"is_note,omitempty"`
	// Thought marks one sentence the agent states as it works:
	// Question holds it, and it takes nothing else but Summary.
	Thought   bool          `json:"thought,omitempty"`
	Resources []NewResource `json:"resources,omitempty"`
	Tags      []Tag         `json:"tags,omitempty"`
	// Captured marks a Ping a Harness adapter captured from the Harness's own
	// prompt rather than one an agent wrote; it is accepted as it comes, so the
	// three strict rules (at most 4 answers, labels of at most 5 words, labels
	// not repeated in the question) are skipped.
	Captured bool `json:"captured,omitempty"`
}

// SendPong is the body of POST /pings/{id}/pong.
type SendPong struct {
	Answer     string   `json:"answer,omitempty"`
	Answers    []string `json:"answers,omitempty"`
	Text       string   `json:"text,omitempty"`
	NewSession bool     `json:"new_session,omitempty"`
}

// validate returns what to fix in n, or "" when it is a well-formed Ping or
// Thought. A captured Ping is accepted as it comes: only what the store and
// the Mac app cannot hold is refused.
func (n NewPing) validate() string {
	if n.Thought {
		return n.validateThought()
	}
	if n.Session == "" {
		return "session is required: pass the Session's identifier"
	}
	if strings.TrimSpace(n.Question) == "" {
		return "question is required: say in one or two lines what you need from the human"
	}
	if k := len(n.offered()); k > 4 && !n.Captured {
		return fmt.Sprintf("a Ping has at most 4 answers, this one has %d: drop or merge some, or send several Pings", k)
	}
	seen := map[string]bool{}
	for i, a := range n.Answers {
		label := strings.TrimSpace(a.Label)
		if label == "" {
			return fmt.Sprintf("answer %d has no label: give each answer a label of at most 5 words", i+1)
		}
		if w := len(strings.Fields(label)); w > 5 && !n.Captured {
			return fmt.Sprintf("answer label %q has %d words: shorten it to at most 5 and put the rest in leads_to", a.Label, w)
		}
		if seen[a.Label] && !n.Captured {
			return fmt.Sprintf("answer label %q appears twice: give each answer a different label", a.Label)
		}
		seen[a.Label] = true
		if strings.Contains(n.Question, a.Label) && !n.Captured && a.Label != NothingMore {
			return fmt.Sprintf("the question repeats the answer %q word for word: ask the question without listing its answers", a.Label)
		}
	}
	if n.Recommended != "" && !seen[n.Recommended] && !n.Captured {
		return fmt.Sprintf("recommended %q is not one of the answers' labels: use a label exactly as given in answers", n.Recommended)
	}
	if n.Approves != "" && n.Approves != ApprovesPlan && n.Approves != ApprovesAction {
		return fmt.Sprintf("approves is %q: use \"plan\", \"action\", or leave it out", n.Approves)
	}
	if n.AllowSeveral && len(n.Answers) < 2 && !n.Captured {
		return "allow_several needs at least 2 answers to tick: add answers or leave allow_several out"
	}
	if msg := validateResources(n.Resources); msg != "" {
		return msg
	}
	if msg := validateTags(n.Tags); msg != "" && !n.Captured {
		return msg
	}
	if n.Finished && n.BlocksAgent {
		return "this Ping is both finished and blocks you, but finished work does not hold you: drop blocks_agent for finished work, or drop finished for a question"
	}
	if !n.Finished && n.NotVerified != "" {
		return "not_verified is only for finished work: drop it, or set finished"
	}
	if !n.Captured && !n.IsNote && !n.BlocksAgent && !n.Finished {
		return "this Ping neither blocks you, nor is finished, nor is a Note: set blocks_agent for a question, finished for finished work, or use send_note for news"
	}
	if n.Approves == ApprovesPlan && !n.Captured && !hasKind(n.Resources, KindDocument) {
		return "a plan to approve must be attached as a document Resource: put the plan in resources with kind document, and keep context to a sentence or two"
	}
	if n.IsNote {
		switch {
		case len(n.Answers) > 0 || n.Recommended != "" || n.AllowSeveral:
			return "a Note needs no Pong, so it takes no answers: drop answers, recommended and allow_several, or send a Ping instead of a Note"
		case n.Approves != "":
			return "a Note needs no Pong, so it approves nothing: drop approves, or send a Ping instead of a Note"
		case n.BlocksAgent:
			return "a Note never blocks the agent: drop blocks_agent, or send a Ping instead of a Note"
		case n.Finished:
			return "a Note is never finished, because it needs no Pong: drop finished, or send a finished Ping instead of a Note"
		}
	}
	return ""
}

// MaxThought is the most characters a Thought may have after trimming.
const MaxThought = 140

// errThought refuses anything done to a Thought once it is kept.
var errThought = errState{"a Thought is the agent's own words in the Thread: it cannot be answered, handed back, withdrawn or dismissed"}

// validateThought returns what to fix in n, a Thought, or "" when it is
// well formed.
func (n NewPing) validateThought() string {
	text := strings.TrimSpace(n.Question)
	switch {
	case n.Session == "":
		return "session is required: pass the Session's identifier"
	case text == "":
		return "a Thought needs its sentence: say in one sentence what you found, decided or changed"
	case utf8.RuneCountInString(text) > MaxThought:
		return fmt.Sprintf("a Thought has at most %d characters, this one has %d: say it in one shorter sentence", MaxThought, utf8.RuneCountInString(text))
	case len(n.Answers) > 0 || n.Recommended != "" || n.AllowSeveral || n.Approves != "":
		return "a Thought takes no answers: drop them, or send a Ping to ask something"
	case len(n.Resources) > 0 || len(n.Tags) > 0 || n.Context != "" || n.Why != "":
		return "a Thought is its sentence alone: drop resources, tags, context and why, or send a Note"
	case n.BlocksAgent || n.Finished || n.MoreComing || n.Captured:
		return "a Thought never blocks you, is never finished and promises nothing: drop blocks_agent, finished and more_coming"
	}
	return ""
}

// hasKind reports whether rs has a Resource of kind.
func hasKind(rs []NewResource, kind string) bool {
	for _, r := range rs {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

// NothingMore is the answer that clears a finished Ping and leaves the
// Session idle. A finished Ping offers it last, and so does a captured Ping
// that does not block the agent, such as the one Pong writes from a turn's
// last message; it does not count against the four answers and is never the
// recommended one.
const NothingMore = "Nothing more"

// offersNothingMore reports whether n is a Ping that offers NothingMore.
func (n NewPing) offersNothingMore() bool {
	return n.Finished || (n.Captured && !n.BlocksAgent && !n.IsNote)
}

// offered is n's answers, less NothingMore on a Ping that offers it anyway.
func (n NewPing) offered() []Answer {
	if !n.offersNothingMore() {
		return n.Answers
	}
	var out []Answer
	for _, a := range n.Answers {
		if a.Label != NothingMore {
			out = append(out, a)
		}
	}
	return out
}

func (n NewPing) ping() Ping {
	if n.Thought {
		return Ping{Session: n.Session, Harness: n.Harness, Folder: n.Folder, Question: strings.TrimSpace(n.Question),
			Summary: strings.TrimSpace(n.Summary), IsNote: true, Thought: true}
	}
	p := Ping{Session: n.Session, Harness: n.Harness, Folder: n.Folder, Question: n.Question, AllowSeveral: n.AllowSeveral,
		Approves: n.Approves, Context: n.Context, Why: n.Why, NotVerified: strings.TrimSpace(n.NotVerified), Summary: strings.TrimSpace(n.Summary), MoreComing: n.MoreComing,
		BlocksAgent: n.BlocksAgent, Finished: n.Finished, IsNote: n.IsNote, Tags: n.Tags}
	if n.Captured {
		p.Tags = goodTags(n.Tags)
	}
	if n.Finished {
		p.Tags = withoutFinished(p.Tags)
	}
	for _, a := range n.offered() {
		if a.Label == n.Recommended {
			p.Answers = append([]Answer{a}, p.Answers...)
		} else {
			p.Answers = append(p.Answers, a)
		}
	}
	if n.offersNothingMore() {
		last := Answer{Label: NothingMore, LeadsTo: "the Session stays idle"}
		for _, a := range n.Answers {
			if a.Label == NothingMore {
				last = a
			}
		}
		p.Answers = append(p.Answers, last)
	}
	return p
}

// check returns what to fix in a Pong for p, or nil.
func (in SendPong) check(p Ping) error {
	if p.Thought {
		return errThought
	}
	if p.IsNote {
		return errState{"a Note takes no Pong: dismiss it instead"}
	}
	if in.NewSession && p.BlocksAgent {
		return badRequest("this Ping blocks its agent, which waits for the Pong in its own Session: send it without new_session")
	}
	if in.Answer == "" && len(in.Answers) == 0 && strings.TrimSpace(in.Text) == "" {
		return badRequest("a Pong needs an answer, ticked answers, text, or a mix of these")
	}
	if in.Answer != "" && !hasAnswer(p, in.Answer) {
		return badRequest(fmt.Sprintf("answer %q is not one of this Ping's answers: use a label exactly as the Ping gives it", in.Answer))
	}
	if len(in.Answers) > 0 && !p.AllowSeveral {
		return badRequest("this Ping does not allow several answers: send one answer instead of answers")
	}
	for _, l := range in.Answers {
		if !hasAnswer(p, l) {
			return badRequest(fmt.Sprintf("ticked answer %q is not one of this Ping's answers: use labels exactly as the Ping gives them", l))
		}
	}
	return nil
}

type badRequest string

func (e badRequest) Error() string { return string(e) }

func (s *pings) routes(mux *http.ServeMux, hold time.Duration) {
	mux.HandleFunc("POST /pings", func(w http.ResponseWriter, r *http.Request) {
		var in NewPing
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a Ping: "+err.Error())
			return
		}
		if msg := in.validate(); msg != "" {
			status := http.StatusBadRequest
			if in.Thought {
				status = http.StatusUnprocessableEntity
			}
			writeError(w, status, msg)
			return
		}
		p, err := s.createWithResources(in)
		if err != nil {
			writePing(w, p, err)
			return
		}
		writeJSON(w, http.StatusCreated, p)
	})
	mux.HandleFunc("GET /pings", func(w http.ResponseWriter, r *http.Request) {
		// The seq the list is at least as new as: a client that follows /events
		// from it misses nothing, without reading the log from its start.
		seq, _ := s.lastSeq()
		w.Header().Set("X-Pong-Seq", strconv.FormatInt(seq, 10))
		ps, err := s.list(r.URL.Query().Get("state"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range ps {
			s.decorate(&ps[i])
		}
		writeJSON(w, http.StatusOK, ps)
	})
	mux.HandleFunc("GET /pings/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.getAny(r.PathValue("id"))
		s.writePing(w, p, err)
	})
	mux.HandleFunc("GET /pings/{id}/wait", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.wait(r.PathValue("id"), hold, r.Context().Done(), r.URL.Query().Get("held") == "1")
		writePing(w, p, err)
	})
	mux.HandleFunc("POST /pings/{id}/pong", func(w http.ResponseWriter, r *http.Request) {
		var in SendPong
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a Pong: "+err.Error())
			return
		}
		id := r.PathValue("id")
		if s.set != nil && s.set.has(id) {
			p, err := s.set.forwardPong(id, in)
			s.writePing(w, p, err)
			return
		}
		p, err := s.applyPong(id, in)
		var se startError
		if errors.As(err, &se) {
			writeStart(w, nil, se.err)
			return
		}
		s.writePing(w, p, err)
	})
	notNote := func(what string) func(Ping) error {
		return func(p Ping) error {
			if p.IsNote {
				return errState{"a Note cannot be " + what + ": dismiss it instead"}
			}
			return nil
		}
	}
	ends := map[string]struct {
		state string
		check func(Ping) error
	}{
		"hand-back":           {StateHandedBack, notNote("handed back")},
		"withdraw":            {StateWithdrawn, func(Ping) error { return nil }},
		"answered-in-harness": {StateAnsweredInHarness, notNote("answered in the Harness")},
		"dismiss": {StateDismissed, func(p Ping) error {
			if !p.IsNote {
				return errState{"only a Note can be dismissed: send a Pong, or hand the Ping back to its Harness"}
			}
			return nil
		}},
	}
	for path, e := range ends {
		mux.HandleFunc("POST /pings/{id}/"+path, func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if s.set != nil && s.set.has(id) {
				p, err := s.set.forwardEnd(id, path)
				s.writePing(w, p, err)
				return
			}
			p, err := s.end(id, e.state, e.check, nil)
			s.writePing(w, p, err)
		})
	}
	mux.HandleFunc("PUT /pings/{id}/draft", func(w http.ResponseWriter, r *http.Request) {
		var draft json.RawMessage
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&draft); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a draft: send the draft as JSON")
			return
		}
		id := r.PathValue("id")
		p, err := s.get(id)
		if errors.Is(err, errNotFound) && s.set != nil {
			p, err = s.set.ping(id)
		}
		if err != nil {
			s.writePing(w, p, err)
			return
		}
		s.setDraft(id, draft, true)
		p, _ = s.getAny(id)
		s.writePing(w, p, nil)
	})
	mux.HandleFunc("GET /events", s.serveEvents)
	mux.HandleFunc("GET /pings/{id}/resources/{rid}", s.serveResource)
}

// serveEvents streams events as Server-Sent Events. A client catches up with
// ?since=SEQ or the Last-Event-ID header (events after that seq); with
// neither it gets only events from now on.
func (s *pings) serveEvents(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("since")
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		cursor = id
	}
	var seq int64
	var err error
	if cursor == "" {
		seq, err = s.lastSeq()
	} else if seq, err = strconv.ParseInt(cursor, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "since must be the seq of the last event you received, or 0 for all")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		evs, next, err := s.eventsAfter(seq)
		if err != nil {
			return
		}
		for _, e := range evs {
			e = s.decorateEvent(e)
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, b)
			seq = e.Seq
		}
		flusher.Flush()
		select {
		case <-next:
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		}
	}
}

func hasAnswer(p Ping, label string) bool {
	for _, a := range p.Answers {
		if a.Label == label {
			return true
		}
	}
	return false
}

// writePing writes p as the local API shows it, marked with its computer
// and draft, or the error.
func (s *pings) writePing(w http.ResponseWriter, p Ping, err error) {
	if err == nil {
		s.decorate(&p)
	}
	writePing(w, p, err)
}

func writePing(w http.ResponseWriter, p Ping, err error) {
	var st errState
	var br badRequest
	var lost errLost
	var un errUnreachable
	switch {
	case errors.As(err, &lost):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "winner": lost.winner})
	case errors.As(err, &un):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "no such Ping: check the id")
	case errors.As(err, &br):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &st):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, p)
	}
}
