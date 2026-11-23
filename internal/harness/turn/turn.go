// Package turn is the Harness-neutral end of an agent's turn: whether the
// agent already sent a Ping in it, the Ping Pong builds from the turn when it
// sent none (build.go), and waiting for the Pong that continues the idle
// Session. Each Harness
// adapter maps its own payloads and answer shapes onto these.
package turn

import (
	"errors"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// Turn is one ended turn of a Session.
type Turn struct {
	Harness, Session, Folder string
	LastMessage              string // the agent's last message in the turn
	Facts                    Facts  // what the turn left beside its message
	// Background is true when work the agent started in the background,
	// such as a subagent or a command, still runs as the turn ends. The
	// Harness shows the Session running until it reports, and so does Pong.
	Background bool
}

// End decides what an ended turn needs. The agent is never pushed for a Ping
// (a Session reads as it would without Pong): a turn with none of its own
// gets the one Pong builds from it,
// unless background work it started still runs, when the Session stays
// working and nothing is built. waits lists the Pings to wait on, oldest
// first: the questions the agent ended its turn on (blocking Pings it did not
// wait for itself) and its finished Ping, or the one built from the turn. A question the human answered before the turn ended
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
func End(c *client.Client, t Turn) (waits []engine.Ping, err error) {
	sent, err := turnsPings(c, t)
	if err != nil {
		return nil, err
	}
	// A prompt the Harness already settled, such as a permission answered in
	// the terminal, is not the agent's Ping for the turn's end.
	ends := false
	for _, p := range sent {
		ends = ends || p.State == engine.StateWaiting || onItsWay(p) || p.Finished
	}
	if !ends {
		if err := idle(c, t); err != nil || t.Background {
			return nil, err
		}
		p, err := Built(c, t)
		if err != nil {
			return nil, err
		}
		return []engine.Ping{p}, nil
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
			return nil, err
		}
	}
	return waits, nil
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

// Built sends the Ping Pong builds from the turn (Build), for a turn that
// ended with no Ping of the agent's own.
func Built(c *client.Client, t Turn) (engine.Ping, error) {
	n := Build(t.LastMessage, t.Facts)
	n.Session, n.Harness, n.Folder = t.Session, t.Harness, t.Folder
	return c.CreatePing(n)
}

func onItsWay(p engine.Ping) bool {
	return p.State == engine.StateAnswered && p.Pong != nil && p.Pong.Delivery == engine.DeliverySent
}

// turnsPings lists the Pings (not Notes) the Session sent since its turn
// began, earlier ones whose Pong is still on its way, and earlier questions
// still waiting for the human, oldest first. A question still waiting is
// already in the human's Queue, so a turn that ends while it waits, such as
// one a background task's report started, is not nudged for another Ping.
func turnsPings(c *client.Client, t Turn) (out []engine.Ping, err error) {
	sess, err := c.Session(t.Harness, t.Session)
	var ce *client.Error
	if err != nil && !(errors.As(err, &ce) && ce.Status == 404) {
		return nil, err
	}
	all, err := c.Pings("")
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		// Text the human wrote in the Thread (Unasked) is not the agent's.
		if p.Session != t.Session || p.Unasked || (p.Harness != "" && p.Harness != t.Harness) {
			continue
		}
		inTurn := sess.TurnStartedAt == nil || !p.CreatedAt.Before(*sess.TurnStartedAt)
		if p.IsNote {
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
	return out, nil
}

// Wait waits for the finished Ping's Pong. continueWith is the Pong to
// continue the Session with; it is nil when the Ping ended another way
// (answered in the Harness, handed back, withdrawn), when the human chose
// engine.NothingMore with no text, or when the Pong asks for a new Session.
func Wait(c *client.Client, id string) (continueWith *engine.Pong, err error) {
	return wait(c, id, false)
}

func wait(c *client.Client, id string, toWake bool) (continueWith *engine.Pong, err error) {
	wait := c.Wait
	if toWake {
		wait = c.WaitToWake
	}
	p, err := wait(id)
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
func WaitAll(c *client.Client, ps []engine.Ping) ([]Answered, error) { return waitAll(c, ps, false) }

// WaitAllToWake is WaitAll for a hook that wakes its Session with the Pongs
// and cannot know the wake arrived: each is delivering until Arrived finds
// it in the woken turn's prompt.
func WaitAllToWake(c *client.Client, ps []engine.Ping) ([]Answered, error) {
	return waitAll(c, ps, true)
}

func waitAll(c *client.Client, ps []engine.Ping, toWake bool) ([]Answered, error) {
	var out []Answered
	for _, p := range ps {
		pong, err := wait(c, p.ID, toWake)
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
// answer as the Harness reads one. It carries the Pongs only: the agent is
// never asked to quote them back, which a Session without Pong would not
// show.
func WakeText(as []Answered, answer func(*engine.Pong) string) string {
	if len(as) == 1 && !as[0].Ping.BlocksAgent {
		return "The human answered your finished Ping in Pong: " + answer(as[0].Pong)
	}
	s := "The human answered your Pings in Pong:"
	for _, a := range as {
		s += "\n- " + a.Ping.Question + ": " + answer(a.Pong)
	}
	return s
}

// Arrived marks delivered each of session's Pongs handed to a wake whose
// line, as WakeText writes it, is in prompt: the prompt of a turn the
// Harness woke. A wake the Harness dropped leaves its Pongs delivering, and
// the Engine brings them back to the human.
func Arrived(c *client.Client, harness, session, prompt string, answer func(*engine.Pong) string) error {
	ps, err := c.Pings(engine.StateAnswered)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Session != session || (p.Harness != "" && p.Harness != harness) || p.Pong == nil ||
			p.Pong.Delivery != engine.DeliveryDelivering {
			continue
		}
		a := answer(p.Pong)
		if strings.Contains(prompt, "\n- "+p.Question+": "+a) || strings.Contains(prompt, "finished Ping in Pong: "+a) {
			if _, err := c.Arrived(p.ID); err != nil {
				return err
			}
		}
	}
	return nil
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
