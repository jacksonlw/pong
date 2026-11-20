package engine

import (
	"database/sql"
	"errors"
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

// processAlive reports whether pid is a running process. A process of
// another user (EPERM) counts as running.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// endGone ends every Session not yet ended whose Harness process is gone.
func (s *projects) endGone() error {
	all, err := s.sessions()
	if err != nil {
		return err
	}
	for _, x := range all {
		switch x.State {
		case SessionEnded, SessionFailed:
			continue
		}
		if x.PID <= 0 || processAlive(x.PID) {
			continue
		}
		s.smu.Lock()
		// Read again under the lock: a hook may have moved it to a new process.
		now, err := s.session(x.Harness, x.ID)
		if err == nil && now.PID == x.PID && now.State != SessionEnded && now.State != SessionFailed {
			_, err = s.registerLocked(RegisterSession{Harness: x.Harness, ID: x.ID, State: SessionEnded})
		}
		s.smu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *projects) endGoneEvery(every time.Duration, closing <-chan struct{}) {
	s.endGone()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.endGone()
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
