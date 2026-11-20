package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// A stop ends the turn a working Session is in. It is asked for
// from any Device, kept on the Session's record, and acted on by the
// Harness's own hook at the agent's next tool call. The Session stays open.
// A stop belongs to one turn: it is over when that turn ends, and is cleared
// when the next one begins.

// Stop states.
const (
	StopSent    = "sent"    // asked for; the turn still runs
	StopStopped = "stopped" // the Harness's hook ended the turn
	StopMissed  = "missed"  // the turn ended by itself first, or the Session ended
)

// Stop is a stop asked for a Session's turn.
type Stop struct {
	State   string    `json:"state"`
	AskedAt time.Time `json:"asked_at"`
	Device  string    `json:"device"` // the name of the Device it was asked from
}

// StopRunner is a Runner whose Harness's hooks can stop a turn. Without it
// the Engine refuses a stop for the Harness's Sessions.
type StopRunner interface {
	StopsTurns()
}

func (s *starter) stopsTurns(harness string) bool {
	_, ok := s.runners[harness].(StopRunner)
	return ok
}

// StopMark is the file that says a stop was asked for the Session and has
// not yet taken or been missed. A hook that runs on every tool call looks
// for it, as it does for HeldTextMark.
func StopMark(dir, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(dir, "stops", hex.EncodeToString(sum[:]))
}

// askStop asks for a stop of the turn the Session is in, from the Device
// named device. A second stop for the same turn is the first one.
func (s *starter) askStop(ref SessionRef, device string) (Stop, error) {
	s.pr.smu.Lock()
	defer s.pr.smu.Unlock()
	sess, err := s.pr.session(ref.Harness, ref.ID)
	if err != nil {
		return Stop{}, err
	}
	if !s.stopsTurns(sess.Harness) {
		return Stop{}, errState{"Pong cannot stop a turn in this Harness"}
	}
	switch sess.State {
	case SessionWorking:
	case SessionIdle:
		return Stop{}, errState{"the Session is idle: it has no turn to stop"}
	case SessionWaiting:
		return Stop{}, errState{"the Session is waiting on a Ping: answer the Ping"}
	default:
		return Stop{}, errState{"the Session has ended"}
	}
	if sess.Stop != nil {
		return *sess.Stop, nil
	}
	mark := StopMark(s.dir, sess.ID)
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return Stop{}, err
	}
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		return Stop{}, err
	}
	sess.Stop = &Stop{State: StopSent, AskedAt: time.Now().UTC(), Device: device}
	return *sess.Stop, s.pr.put(&sess)
}

// StoppedMark is the file that says the Session's last turn was stopped and
// no turn has begun since. The hook that listens for text after a stop looks
// for it. While a stop takes it is written before StopMark is removed, so a
// hook that looks for StopMark first and then for this one never finds
// neither.
func StoppedMark(dir, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(dir, "stopped", hex.EncodeToString(sum[:]))
}

// StoppedLine is what the Thread says when a stop took.
func StoppedLine(device string) string { return "You stopped this turn from " + device + "." }

// TakenStop is the answer of POST /session/take-stop.
type TakenStop struct {
	Stopped bool `json:"stopped"` // a stop was sent: the hook ends the turn now
}

// takeStop is called by the Harness's hook before a tool call. With a stop
// sent for the Session's turn, the stop is now stopped, the Session is idle
// and the Thread says who stopped the turn; the hook ends the turn.
func (s *starter) takeStop(ref SessionRef) (bool, error) {
	s.pr.smu.Lock()
	defer s.pr.smu.Unlock()
	sess, err := s.pr.session(ref.Harness, ref.ID)
	if err != nil || sess.Stop == nil || sess.Stop.State != StopSent {
		return false, err
	}
	stopped := *sess.Stop
	stopped.State = StopStopped
	sess.Stop = &stopped
	mark := StoppedMark(s.dir, sess.ID)
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(mark, nil, 0o600); err != nil {
		return false, err
	}
	if err := s.pr.put(&sess); err != nil {
		return false, err
	}
	os.Remove(StopMark(s.dir, sess.ID))
	if err := s.ps.createLine(sess, StoppedLine(stopped.Device)); err != nil {
		return true, err
	}
	// The turn is over, and nobody is nudged for a finished Ping.
	_, err = s.pr.registerLocked(RegisterSession{Harness: sess.Harness, ID: sess.ID, State: SessionIdle})
	return true, err
}

// createLine records one of Pong's own lines in sess's Thread.
func (s *pings) createLine(sess Session, line string) error {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	now := s.clock()
	p := Ping{ID: hex.EncodeToString(buf), Session: sess.ID, Harness: sess.Harness, Project: sess.Project, Folder: sess.Folder,
		Question: line, Answers: []Answer{}, IsNote: true, Line: true, State: StateDismissed, CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(p, EventPingState, true)
}

// stopOver settles sess's stop as its state becomes state: the next turn
// beginning clears it, and a turn or Session that ends with the stop still
// sent missed it. It reports whether the mark is to be removed.
func stopOver(sess *Session, state string) (unmark bool) {
	if sess.Stop == nil {
		return false
	}
	switch state {
	case SessionWorking:
		sess.Stop = nil
		return true
	case SessionIdle, SessionEnded, SessionFailed:
		if sess.Stop.State == StopSent {
			missed := *sess.Stop
			missed.State = StopMissed
			sess.Stop = &missed
			return true
		}
	}
	return false
}

func (s *starter) stopRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /session/stop", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ref := SessionRef{q.Get("harness"), q.Get("id")}
		stop, err := s.askStop(ref, s.name)
		if errors.Is(err, errNotFound) && s.comp != nil {
			// A Session on another computer: that computer decides.
			stop, err = s.comp.stopOn(ref)
		}
		var un errUnreachable
		if errors.As(err, &un) {
			writeError(w, http.StatusServiceUnavailable, "Can't reach "+un.name+".")
			return
		}
		writeStart(w, stop, err)
	})
	mux.HandleFunc("POST /session/take-stop", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		stopped, err := s.takeStop(SessionRef{q.Get("harness"), q.Get("id")})
		writeStart(w, TakenStop{Stopped: stopped}, err)
	})
}
