// Package turn is the Harness-neutral end of an agent's turn: whether the
// agent already sent a Ping in it, the one nudge to send a finished Ping, the
// Ping built from the agent's last message when it sends none,
// and waiting for the Pong that continues the idle Session. Each Harness
// adapter maps its own payloads and answer shapes onto these.
package turn

import (
	"errors"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// Nudge is what the agent is told when its turn ends with no Ping in it. An
// agent whose work is not finished, because it waits on work it started such
// as a subagent or a background command, answers with a Note instead: told
// only to send a finished Ping, such agents refused and wrote why, and that
// message came to the human as a plain Ping that asked nothing.
const Nudge = "Before you stop, send a finished Ping with Pong's send_ping tool, with finished: true and what you suggest next as its answers, the recommended one first. Your human reads the Ping, not your closing message: say in it what changed and why, and in not_verified what you did not check. If your work is not finished because you are waiting on work you started, such as a subagent or a background command, send a Note with send_note saying what you are waiting on instead. Then stop."

// Turn is one ended turn of a Session.
type Turn struct {
	Harness, Session, Folder string
	LastMessage              string // the agent's last message in the turn
	Nudged                   bool   // the agent was already nudged in this turn
	// Background is true when work the agent started in the background,
	// such as a subagent or a command, still runs as the turn ends. The
	// Harness shows the Session running until it reports, and so does Pong.
	Background bool
}

// End decides what an ended turn needs. nudge is true when the agent sent no
// Ping in the turn and has not been nudged yet: the adapter holds the turn
// with Nudge and does nothing else. A nudged turn with a Note in it (not a
// Thought) ends idle with nothing to wait on: the agent said it is waiting on
// its own work. Otherwise waits lists the Pings to wait
// on, oldest first: the questions the agent ended its turn on (blocking Pings
// it did not wait for itself) and its finished Ping, or a plain one built
// from its last message. A question the human answered before the turn ended
// is among them while its Pong is still on its way: waiting on it delivers
// the Pong at once. The Session is now idle, unless every Pong in waits is
// already sent and one of them continues it: then it wakes at once and was
// never done. A Pong that wakes nothing, such as "Nothing more", leaves it
// idle. waits is empty when
// the agent's Pings in the turn are all answered and delivered already.
//
// A Pong still on its way to a Ping from before the turn began is among
// waits too, and counts as a Ping in the turn: no hook waited on it, and a
// sent Pong is kept until it is delivered.
func End(c *client.Client, t Turn) (nudge bool, waits []engine.Ping, err error) {
	sent, noted, err := turnsPings(c, t)
	if err != nil {
		return false, nil, err
	}
	if len(sent) == 0 && !t.Nudged {
		return true, nil, nil
	}
	if len(sent) == 0 {
		if err := idle(c, t); err != nil || noted {
			return false, nil, err
		}
		p, err := Plain(c, t)
		if err != nil {
			return false, nil, err
		}
		return false, []engine.Ping{p}, nil
	}
	var finished *engine.Ping
	asking, waking := false, false
	for i := range sent {
		p := sent[i]
		if p.State != engine.StateWaiting && !onItsWay(p) {
			continue
		}
		asking = asking || p.State == engine.StateWaiting
		waking = waking || (onItsWay(p) && continues(p.Pong))
		if p.BlocksAgent {
			waits = append(waits, p)
		} else {
			finished = &sent[i]
		}
	}
	if finished != nil {
		waits = append(waits, *finished)
	}
	if asking || !waking {
		if err := idle(c, t); err != nil {
			return false, nil, err
		}
	}
	return false, waits, nil
}

// idle marks the Session's turn over: idle, or still working while its
// background work runs.
func idle(c *client.Client, t Turn) error {
	r := engine.RegisterSession{Harness: t.Harness, ID: t.Session, Folder: t.Folder, State: engine.SessionIdle}
	if t.Background {
		r.State, r.AwaitsOwnWork = engine.SessionWorking, true
	}
	_, err := c.RegisterSession(r)
	return err
}

// Plain sends the Ping built from the agent's last message, for a turn that
// ended with no Ping of the agent's own. The question is the message whole,
// trimmed of surrounding whitespace and nothing else. It offers
// engine.NothingMore and is not finished: the agent never said it was.
func Plain(c *client.Client, t Turn) (engine.Ping, error) {
	q := strings.TrimSpace(t.LastMessage)
	if q == "" {
		q = "The agent finished its turn without a message."
	}
	return c.CreatePing(engine.NewPing{Session: t.Session, Harness: t.Harness, Folder: t.Folder, Question: q,
		Answers: []engine.Answer{{Label: engine.NothingMore, LeadsTo: "the Session stays idle"}}, Captured: true})
}

func onItsWay(p engine.Ping) bool {
	return p.State == engine.StateAnswered && p.Pong != nil && p.Pong.Delivery == engine.DeliverySent
}

// turnsPings lists the Pings (not Notes) the Session sent since its turn
// began, earlier ones whose Pong is still on its way, and earlier questions
// still waiting for the human, oldest first. A question still waiting is
// already in the human's Queue, so a turn that ends while it waits, such as
// one a background task's report started, is not nudged for another Ping.
// noted reports whether it also sent a Note, not a Thought, in its turn.
func turnsPings(c *client.Client, t Turn) (out []engine.Ping, noted bool, err error) {
	sess, err := c.Session(t.Harness, t.Session)
	var ce *client.Error
	if err != nil && !(errors.As(err, &ce) && ce.Status == 404) {
		return nil, false, err
	}
	all, err := c.Pings("")
	if err != nil {
		return nil, false, err
	}
	for _, p := range all {
		// Text the human wrote in the Thread (Unasked) is not the agent's.
		if p.Session != t.Session || p.Unasked || (p.Harness != "" && p.Harness != t.Harness) {
			continue
		}
		inTurn := sess.TurnStartedAt == nil || !p.CreatedAt.Before(*sess.TurnStartedAt)
		if p.IsNote {
			noted = noted || (inTurn && !p.Thought)
			continue
		}
		if !inTurn && !onItsWay(p) && !(p.BlocksAgent && p.State == engine.StateWaiting) {
			continue
		}
		out = append(out, p)
	}
	// GET /pings puts blocking Pings first; order by creation.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, noted, nil
}

// Wait waits for the finished Ping's Pong. continueWith is the Pong to
// continue the Session with; it is nil when the Ping ended another way
// (answered in the Harness, handed back, withdrawn), when the human chose
// engine.NothingMore with no text, or when the Pong asks for a new Session.
func Wait(c *client.Client, id string) (continueWith *engine.Pong, err error) {
	p, err := c.Wait(id)
	if err != nil || !continues(p.Pong) {
		return nil, err
	}
	return p.Pong, nil
}

// continues reports whether pong wakes the Session: not when the human chose
// engine.NothingMore with no text, or asked for a new Session.
func continues(pong *engine.Pong) bool {
	return pong != nil && !pong.NewSession &&
		!(pong.Answer == engine.NothingMore && strings.TrimSpace(pong.Text) == "")
}

// Answered is a Pong that continues the Session, beside its Ping.
type Answered struct {
	Ping engine.Ping
	Pong *engine.Pong
}

// WaitAll waits for the Pongs to ps in order and returns those that continue
// the Session.
func WaitAll(c *client.Client, ps []engine.Ping) ([]Answered, error) {
	var out []Answered
	for _, p := range ps {
		pong, err := Wait(c, p.ID)
		if err != nil {
			return nil, err
		}
		if pong != nil {
			out = append(out, Answered{p, pong})
		}
	}
	return out, nil
}

// WakeText is what wakes the Session with the Pongs in as, each written by
// answer as the Harness reads one. A Harness shows its human the wake folded
// away or not at all, so the text ends by asking the agent to quote the Pongs
// in its reply: the Session then shows what was answered.
func WakeText(as []Answered, answer func(*engine.Pong) string) string {
	if len(as) == 1 && !as[0].Ping.BlocksAgent {
		return "The human answered your finished Ping in Pong: " + answer(as[0].Pong) +
			"\nOpen your reply with one line that quotes this Pong, starting \"You answered in Pong:\"."
	}
	s := "The human answered your Pings in Pong:"
	for _, a := range as {
		s += "\n- " + a.Ping.Question + ": " + answer(a.Pong)
	}
	return s + "\nOpen your reply by quoting each Pong on a line of its own, starting \"You answered in Pong:\"."
}

// Began is called when a hook wakes the Session into a new turn, as a Pong
// does. The Session is working again and its turn begins now, even if it
// never went idle. Pings still waiting for it stay waiting: nobody answered
// them.
func Began(c *client.Client, harness, session, folder string) error {
	_, err := c.RegisterSession(engine.RegisterSession{Harness: harness, ID: session, Folder: folder, State: engine.SessionWorking, NewTurn: true})
	return err
}

// HarnessPrompted is called when the Harness itself submits a prompt, such
// as a subagent's report. The Session is working, and a turn begins only if
// it was not working already: a report folded into a running turn leaves
// that turn's start, so the Pings the turn sent before it are still its own.
// Pings still waiting stay waiting: nobody answered them.
func HarnessPrompted(c *client.Client, harness, session, folder string) error {
	_, err := c.RegisterSession(engine.RegisterSession{Harness: harness, ID: session, Folder: folder, State: engine.SessionWorking})
	return err
}

// Prompted is called when the human themself submits a prompt in the
// Harness, and for nothing else: a turn begins as in Began, and any Ping
// still waiting for the Session (its finished Ping, or the questions its last
// turn ended on) is answered in the Harness.
func Prompted(c *client.Client, harness, session, folder string) error {
	if err := Began(c, harness, session, folder); err != nil {
		return err
	}
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Session == session && (p.Harness == "" || p.Harness == harness) && !p.IsNote {
			if _, err := c.AnsweredInHarness(p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
