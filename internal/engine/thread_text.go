package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Text the human writes in a Thread while no Ping waits there. It is kept in
// the Thread as a record with no question that carries the text as its Pong
// (Ping.Unasked), so it is shown, kept and delivered as any Pong is (ADR
// 0005): sent, then delivered once the Session's Harness has it.
//
// How it gets there depends on where the Session is:
//   - it has ended: Pong opens it again for the human with the text as its
//     next prompt, where their surface can (starter.reopen);
//   - otherwise the Harness's hooks take it: before the agent's next tool call
//     while it works, at the end of its turn, or as it sits idle (takeText,
//     listenText).

// TextRunner is a Runner whose Harness's hooks take text written in a Thread.
// Without it a Session that has not ended takes text only as the Pong on its
// finished Ping.
type TextRunner interface {
	TakesText()
}

func (s *starter) takesText(harness string) bool {
	_, ok := s.runners[harness].(TextRunner)
	return ok
}

// HeldTextMark is the file that says text is kept for the Session until its
// hooks take it. A hook that runs on every tool call looks for it, so a tool
// call costs no request to the Engine while nothing is kept.
func HeldTextMark(dir, session string) string {
	sum := sha256.Sum256([]byte(session))
	return filepath.Join(dir, "held-text", hex.EncodeToString(sum[:]))
}

// createUnasked records text written in sess's Thread, as a sent Pong.
func (s *pings) createUnasked(sess Session, text string) (Ping, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return Ping{}, err
	}
	now := s.clock()
	p := Ping{ID: hex.EncodeToString(buf), Session: sess.ID, Harness: sess.Harness, Project: sess.Project, Folder: sess.Folder,
		Answers: []Answer{}, Unasked: true, State: StateAnswered, CreatedAt: now, UpdatedAt: now,
		Pong: &Pong{Text: text, SentAt: now, Delivery: DeliverySent}}
	s.mu.Lock()
	defer s.mu.Unlock()
	return p, s.save(p, EventPingPonged, true)
}

// keepText records text for a Session whose hooks will take it.
func (s *starter) keepText(sess Session, text string) error {
	if _, err := s.ps.createUnasked(sess, text); err != nil {
		return err
	}
	mark := HeldTextMark(s.dir, sess.ID)
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return err
	}
	return os.WriteFile(mark, nil, 0o600)
}

// keepPong keeps the undelivered Pong on p for its Session's hooks, as the
// human asked by sending it again: it is sent once more, and stays so until
// they take it, however long the Session sits idle.
func (s *starter) keepPong(p Ping, sess Session, from []string) (Ping, error) {
	s.ps.mu.Lock()
	p, err := s.ps.get(p.ID)
	if err == nil && (p.Pong == nil || !slices.Contains(from, p.Pong.Delivery)) {
		err = errState{"only an undelivered Pong, or one that may not have arrived, can be sent again"}
	}
	if err != nil {
		s.ps.mu.Unlock()
		return p, err
	}
	p.Pong.Delivery, p.Pong.NotDelivered, p.Pong.Again = DeliverySent, "", true
	p.UpdatedAt = s.ps.clock()
	err = s.ps.save(p, EventPingState, false)
	s.ps.mu.Unlock()
	if err != nil {
		return p, err
	}
	mark := HeldTextMark(s.dir, sess.ID)
	if err := os.MkdirAll(filepath.Dir(mark), 0o700); err != nil {
		return p, err
	}
	return p, os.WriteFile(mark, nil, 0o600)
}

// againText is a Pong handed to its Session after its Ping stopped waiting,
// with the Ping's question, since the agent no longer waits on that Ping.
func againText(p Ping) string {
	return `Their Pong to your Ping "` + p.Question + `": ` +
		pongText(SendPong{Answer: p.Pong.Answer, Answers: p.Pong.Answers, Text: p.Pong.Text})
}

// takeText hands over the text kept for the Session, oldest first, and
// records each as delivered.
func (s *starter) takeText(ref SessionRef) ([]string, error) {
	t, err := s.pr.thread(ref.Harness, ref.ID)
	if err != nil {
		return nil, err
	}
	texts := []string{}
	for _, p := range t.Pings {
		if p.Pong == nil || !(p.Unasked || p.Pong.Again) || p.Pong.Delivery != DeliverySent {
			continue
		}
		if _, ok, err := s.ps.deliver(p.ID, []string{DeliverySent}, DeliveryDelivered, ""); err != nil {
			return texts, err
		} else if ok && p.Unasked {
			texts = append(texts, p.Pong.Text)
		} else if ok {
			texts = append(texts, againText(p))
		}
	}
	os.Remove(HeldTextMark(s.dir, ref.ID))
	return texts, nil
}

// listenText waits up to hold for text for an idle Session, on behalf of the
// hook that sits at the end of its turn. listening is false once there is
// nothing to wait for: text was taken, or the Session is no longer idle (the
// human typed in the Harness, or it ended).
func (s *starter) listenText(ref SessionRef, hold time.Duration, done <-chan struct{}) (texts []string, listening bool, err error) {
	t := time.NewTimer(hold)
	defer t.Stop()
	for {
		s.ps.mu.Lock()
		changed := s.ps.events
		s.ps.mu.Unlock()
		if texts, err := s.takeText(ref); err != nil || len(texts) > 0 {
			return texts, false, err
		}
		sess, err := s.pr.session(ref.Harness, ref.ID)
		if err != nil || sess.State != SessionIdle || !s.takesText(sess.Harness) {
			return nil, false, err
		}
		select {
		case <-changed:
		case <-t.C:
			return nil, true, nil
		case <-done:
			return nil, true, nil
		case <-s.ps.closing:
			return nil, true, nil
		}
	}
}

// TakenText is the answer of POST /session/take-text.
type TakenText struct {
	Texts []string `json:"texts"`
	// Listening is set when a listen ran out with the Session still idle:
	// ask again.
	Listening bool `json:"listening,omitempty"`
}

func (s *starter) textRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /session/take-text", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ref := SessionRef{q.Get("harness"), q.Get("id")}
		var out TakenText
		var err error
		if q.Get("listen") != "" {
			out.Texts, out.Listening, err = s.listenText(ref, s.hold, r.Context().Done())
		} else {
			out.Texts, err = s.takeText(ref)
		}
		if out.Texts == nil {
			out.Texts = []string{}
		}
		writeStart(w, out, err)
	})
}
