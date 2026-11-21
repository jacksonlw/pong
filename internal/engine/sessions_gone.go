package engine

import (
	"database/sql"
	"errors"
	"log"
	"syscall"
	"time"
)

// A Harness tells the Engine a Session ended from a hook (Claude Code's
// SessionEnd). When its process dies first (killed, or ended while the Engine
// restarted for a reinstall), nothing would tell it, and the Session would
// stay working for good. So the hooks also give the Harness process's id,
// and the Engine ends a Session once that process is gone: when it starts,
// and every SessionCheckEvery after.

// DefaultSessionCheckEvery is how often the Engine looks for Sessions whose
// Harness process is gone.
const DefaultSessionCheckEvery = 30 * time.Second

// Some Sessions have no Harness process of their own to watch: one whose
// hooks gave none (a Pong MCP server with no Session id, an older install),
// or one whose process is shared with other Sessions, as Codex runs every
// thread in one long-lived app-server. Nothing would end those, so the Engine
// ends one once it has not changed for DefaultSessionQuietFor. If it was
// still there after all, its next hook brings it back.
const DefaultSessionQuietFor = time.Hour

// processAlive reports whether pid is a running process. A process of
// another user (EPERM) counts as running; one that has exited but that its
// parent has not reaped (a zombie) does not.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return (err == nil || errors.Is(err, syscall.EPERM)) && !zombie(pid)
}

// endGone ends every Session not yet ended whose Harness process is gone,
// and every one with no process of its own that has been quiet for quiet.
func (s *projects) endGone(quiet time.Duration) error {
	all, err := s.sessions()
	if err != nil {
		return err
	}
	var first error
	open := func(x Session) bool { return x.State != SessionEnded && x.State != SessionFailed }
	sharing := map[int]int{} // how many open Sessions name each process
	for _, x := range all {
		if open(x) && x.PID > 0 {
			sharing[x.PID]++
		}
	}
	for _, x := range all {
		if !open(x) {
			continue
		}
		own := x.PID > 0 && sharing[x.PID] == 1
		switch {
		case x.PID > 0 && !processAlive(x.PID):
		case !own && time.Since(x.UpdatedAt) > quiet:
		default:
			continue
		}
		s.smu.Lock()
		// Read again under the lock: a hook may have moved or touched it.
		now, err := s.session(x.Harness, x.ID)
		if err == nil && now.PID == x.PID && now.UpdatedAt.Equal(x.UpdatedAt) && open(now) {
			_, err = s.registerLocked(RegisterSession{Harness: x.Harness, ID: x.ID, State: SessionEnded})
		}
		s.smu.Unlock()
		if err != nil && first == nil {
			first = err // one Session that cannot be ended leaves the others to end
		}
	}
	return first
}

// A turn can also end unseen while its process lives on: the human stopped
// it in the Harness (Claude Code calls no Stop hook then), or the Stop hook
// could not reach the Engine while it restarted. The Session would show as
// working for good. Where the Harness writes a transcript the Engine reads
// it (TurnReader): a turn the transcript shows ended, and one whose
// transcript has not changed for the quiet time with nothing waiting on the
// human, is over, and the Session idle. Its next hook makes it working again.

// TurnReader is a Runner that can tell from a Session's transcript whether
// its turn is over.
type TurnReader interface {
	// TurnOver reads the transcript: ended is true when its last entry
	// closes a turn, and written is when it last changed. ok is false when
	// it cannot be read.
	TurnOver(transcript string) (ended bool, written time.Time, ok bool)
}

// turnSettles is how long a transcript that shows a turn ended must stay
// unchanged before the Engine takes the turn as over: the Stop hook says so
// itself well within it, and may wake the Session into a new turn.
const turnSettles = 2 * time.Minute

// idleLost makes idle every working Session whose turn is over by its
// transcript.
func (s *projects) idleLost(quiet time.Duration) error {
	if s.turnOver == nil {
		return nil
	}
	all, err := s.sessions()
	if err != nil {
		return err
	}
	waiting, err := s.pings.list(StateWaiting)
	if err != nil {
		return err
	}
	asks := map[SessionRef]bool{} // Sessions waiting on the human
	for _, p := range waiting {
		if !p.IsNote && !p.Thought && p.BlocksAgent {
			asks[SessionRef{p.Harness, p.Session}] = true
		}
	}
	var first error
	for _, x := range all {
		if x.State != SessionWorking || x.Transcript == "" {
			continue
		}
		ended, written, ok := s.turnOver(x)
		since := time.Since(written)
		if !ok || !(ended && since > turnSettles || since > quiet && !asks[SessionRef{x.Harness, x.ID}]) {
			continue
		}
		s.smu.Lock()
		now, err := s.session(x.Harness, x.ID)
		if err == nil && now.State == SessionWorking && now.UpdatedAt.Equal(x.UpdatedAt) {
			_, err = s.registerLocked(RegisterSession{Harness: x.Harness, ID: x.ID, State: SessionIdle})
		}
		s.smu.Unlock()
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// logEndGone runs endGone and idleLost, writing to the Engine's log why it
// could not.
func (s *projects) logEndGone(quiet time.Duration) {
	if err := s.endGone(quiet); err != nil {
		log.Printf("ending gone Sessions: %v", err)
	}
	if err := s.idleLost(quiet); err != nil {
		log.Printf("ending lost turns: %v", err)
	}
}

func (s *projects) endGoneEvery(every, quiet time.Duration, closing <-chan struct{}) {
	s.logEndGone(quiet)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.logEndGone(quiet)
		case <-closing:
			return
		}
	}
}

// onlyHarnessOf returns the Harness of the one Session known by id, or ""
// when none or several are.
func (s *projects) onlyHarnessOf(id string) (string, error) {
	var h string
	err := s.db.QueryRow(`SELECT harness FROM sessions WHERE id = ? GROUP BY id HAVING count(*) = 1`, id).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return h, err
}
