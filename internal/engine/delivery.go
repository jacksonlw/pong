package engine

import (
	"encoding/json"
	"net/http"
	"time"
)

// Where a Pong stands on its way to the Session's Harness. A Pong
// is sent when the human answers and delivered when the Harness accepted it:
// a held prompt or the wait tool returned it, the Session's hooks took it, or
// Pong opened the Session for the human with it as the prompt. A Pong recorded before delivery was tracked has none, and
// counts as delivered.
const (
	DeliverySent        = "sent"        // kept and retried until delivered
	DeliveryDelivering  = "delivering"  // handed to a wake, not yet seen in the woken turn
	DeliveryDelivered   = "delivered"   // the Harness accepted it
	DeliveryUndelivered = "undelivered" // it cannot be delivered; back with the human
	// DeliveryUnconfirmed is a Pong an earlier Pong was resuming a Session
	// with when it stopped: it may have arrived, so the human is asked before
	// it is sent again.
	DeliveryUnconfirmed = "unconfirmed"
	DeliveryLeft        = "left" // the human chose not to send it again
)

// Event kinds for delivery. A sent Pong is EventPingPonged.
const (
	EventPongDelivered   = "ping.delivered"
	EventPongUndelivered = "ping.undelivered"
)

// DefaultDeliveryGrace is how long a sent Pong waits for a hold, when none
// holds it, before the Engine stops waiting for one.
const DefaultDeliveryGrace = 30 * time.Second

// Reasons a Pong was not delivered, in words the card shows.
const (
	whyEnded       = "The Session has ended."
	whyNotHeld     = "Its prompt is no longer held: the terminal may have closed or the Harness quit."
	whyUnconfirmed = "Pong stopped while resuming the Session with it, so it may or may not have arrived."
	whyNotArrived  = "The Session never took it: it may have moved or closed."
)

// needsHuman reports whether a Pong is back with the human.
func needsHuman(d string) bool { return d == DeliveryUndelivered || d == DeliveryUnconfirmed }

// deliver moves the Pong on Ping id from one of from to to, with why, and
// reports whether it moved. A Pong elsewhere is left as it is.
func (s *pings) deliver(id string, from []string, to, why string) (Ping, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.get(id)
	if err != nil || p.Pong == nil {
		return p, false, err
	}
	ok := false
	for _, f := range from {
		ok = ok || p.Pong.Delivery == f
	}
	if !ok {
		return p, false, nil
	}
	p.Pong.Delivery, p.Pong.NotDelivered = to, why
	p.UpdatedAt = s.clock()
	if to == DeliveryDelivered {
		at := p.UpdatedAt
		p.Pong.DeliveredAt = &at
	}
	kind := EventPingState
	switch to {
	case DeliveryDelivered:
		kind = EventPongDelivered
	case DeliveryUndelivered:
		kind = EventPongUndelivered
	}
	return p, true, s.save(p, kind, false)
}

// held marks the start of a wait that holds Ping id's prompt, and returns
// its end. While none holds a sent Pong, the Engine waits grace for a hold,
// then calls onUnheld.
func (s *pings) held(id string) (release func()) {
	s.mu.Lock()
	s.holders[id]++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.holders[id]--
		if s.holders[id] == 0 {
			delete(s.holders, id)
		}
		s.mu.Unlock()
	}
}

// watch starts the grace period for a sent Pong on Ping id: if it is still
// sent and nothing holds it once grace has passed, onUnheld decides.
func (s *pings) watch(id string) {
	time.AfterFunc(s.grace, func() {
		select {
		case <-s.closing:
			return
		default:
		}
		s.mu.Lock()
		holding := s.holders[id] > 0
		p, err := s.get(id)
		s.mu.Unlock()
		if holding || err != nil || p.Pong == nil || p.Pong.Delivery != DeliverySent {
			return
		}
		if s.onUnheld != nil {
			s.onUnheld(p)
			return
		}
		s.deliver(id, []string{DeliverySent}, DeliveryUndelivered, whyNotHeld)
	})
}

// sent is called once a Pong on p is recorded: if nothing holds its prompt
// now, the grace period starts. Whether anything held it before is not asked:
// the Engine forgets that when it restarts, and a Pong nothing will hold must
// still come back to the human.
func (s *pings) sent(p Ping) {
	s.mu.Lock()
	watch := s.holders[p.ID] == 0
	s.mu.Unlock()
	if watch {
		s.watch(p.ID)
	}
}

// recover runs when the Engine starts. No Ping still says more are coming
// (see noMoreComing). A Pong left delivering becomes unconfirmed; every Pong
// still sent gets the grace period, for its holder to come back before
// onUnheld settles it.
func (s *pings) recover() error {
	if err := s.noMoreComing("", "", true); err != nil {
		return err
	}
	ps, err := s.list("")
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Pong == nil {
			continue
		}
		switch p.Pong.Delivery {
		case DeliveryDelivering:
			if _, _, err := s.deliver(p.ID, []string{DeliveryDelivering}, DeliveryUnconfirmed, whyUnconfirmed); err != nil {
				return err
			}
		case DeliverySent:
			if !p.Pong.Again { // that one waits for the Session's hooks, however long
				s.watch(p.ID)
			}
		}
	}
	return nil
}

// SendAgain is the body of POST /pings/{id}/send-again.
type SendAgain struct {
	NewSession bool `json:"new_session,omitempty"`
}

// onUnheld settles a sent Pong whose prompt nobody holds: it is undelivered
// and back with the human. A Session a Harness named that is still at work
// is left to take it at its turn's end (the hooks wait on a Pong still on
// its way), and is looked at again after another grace. One no Harness named
// has no hooks to end its turn, so it is never left. Pong does not open a
// Session by itself once a wait has run out, and never sends the Pong to
// another one.
func (s *starter) onUnheld(p Ping) {
	why := whyNotHeld
	sess, err := s.sessionOf(p)
	switch {
	case err == nil && sess.Harness != "" && (sess.State == SessionWorking || sess.State == SessionWaiting):
		s.ps.watch(p.ID)
		return
	case err == nil && sess.State == SessionEnded:
		why = whyEnded
	}
	s.ps.deliver(p.ID, []string{DeliverySent}, DeliveryUndelivered, why)
}

// sendAgain sends an undelivered or unconfirmed Pong again, as the human
// asked: as a new Session continued from its own, or into its own Session.
// One that has ended is opened again with it (reopen); one that has not is
// left it for its hooks to take as it next works, wakes or sits idle
// (keepPong), when its Harness's hooks take text.
func (s *starter) sendAgain(id string, in SendAgain) (Ping, error) {
	p, err := s.ps.get(id)
	if err != nil {
		return p, err
	}
	if p.Pong == nil || !needsHuman(p.Pong.Delivery) {
		return p, errState{"only an undelivered Pong, or one that may not have arrived, can be sent again"}
	}
	from := []string{DeliveryUndelivered, DeliveryUnconfirmed}
	sess, serr := s.sessionOf(p)
	if in.NewSession {
		h, folder := p.Harness, p.Folder
		if serr == nil {
			h = sess.Harness
			if sess.Folder != "" {
				folder = sess.Folder
			}
		}
		pg := *p.Pong
		if _, err := s.start(h, folder, s.newSessionPrompt(p, SendPong{Answer: pg.Answer, Answers: pg.Answers, Text: pg.Text}), &SessionRef{h, p.Session}); err != nil {
			return p, err
		}
		p, _, err = s.ps.deliver(id, from, DeliveryDelivered, "")
		return p, err
	}
	switch {
	case serr != nil:
	case sess.State == SessionEnded:
		return s.reopen(p, sess, from, true)
	case s.takesText(sess.Harness):
		return s.keepPong(p, sess, from)
	}
	return p, errState{"Pong cannot reach this Session with it any more: send it as a new Session"}
}

func (s *starter) deliveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /pings/{id}/send-again", func(w http.ResponseWriter, r *http.Request) {
		var in SendAgain
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeError(w, http.StatusBadRequest, "body is not a send-again: "+err.Error())
				return
			}
		}
		if set := s.ps.set; set != nil && set.has(r.PathValue("id")) {
			again := "same_session"
			if in.NewSession {
				again = "new_session"
			}
			p, err := set.forwardAgain(r.PathValue("id"), again)
			s.ps.writePing(w, p, err)
			return
		}
		p, err := s.sendAgain(r.PathValue("id"), in)
		if err != nil {
			writeStart(w, nil, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /pings/{id}/withdraw-pong", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if set := s.ps.set; set != nil && set.has(id) {
			p, err := set.withdrawPong(id)
			s.ps.writePing(w, p, err)
			return
		}
		p, err := s.ps.unsend(id)
		s.ps.writePing(w, p, err)
	})
	mux.HandleFunc("POST /pings/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
		if set := s.ps.set; set != nil && set.has(r.PathValue("id")) {
			p, err := set.forwardAgain(r.PathValue("id"), "leave")
			s.ps.writePing(w, p, err)
			return
		}
		p, ok, err := s.ps.deliver(r.PathValue("id"), []string{DeliveryUndelivered, DeliveryUnconfirmed}, DeliveryLeft, "")
		if err == nil && !ok {
			err = errState{"only an undelivered Pong, or one that may not have arrived, can be left"}
		}
		writePing(w, p, err)
	})
}
