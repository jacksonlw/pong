package engine_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// threeComputers pairs three Engines: a desktop where Sessions run, a
// laptop the human answers from, and a box that shows the same Queue.
func threeComputers(t *testing.T) (desk, laptop, box *enginetest.Computer) {
	t.Helper()
	book := &enginetest.Book{}
	desk = enginetest.StartComputer(t, book, "Desktop")
	laptop = enginetest.StartComputer(t, book, "Laptop")
	box = enginetest.StartComputer(t, book, "Box")
	enginetest.Pair(t, desk, laptop)
	enginetest.Pair(t, desk, box)
	for _, c := range []*enginetest.Computer{desk, laptop, box} {
		enginetest.Eventually(t, c.Name+" to reach the others", func() bool {
			s, _ := c.C.Devices()
			n := 0
			for _, d := range s.Devices {
				if d.Reachable {
					n++
				}
			}
			return n == 3
		})
	}
	return
}

func shows(t *testing.T, c *enginetest.Computer, what string, f func(engine.Ping) bool, id string) engine.Ping {
	t.Helper()
	var got engine.Ping
	enginetest.Eventually(t, c.Name+" to show "+what, func() bool {
		p, err := c.C.Ping(id)
		if err != nil {
			return false
		}
		got = p
		return f(p)
	})
	return got
}

func TestAPingOnOneComputerIsAnsweredFromAnother(t *testing.T) {
	desk, laptop, box := threeComputers(t)
	p := newPing(t, desk.C)

	got := shows(t, laptop, "the desktop's Ping", func(p engine.Ping) bool { return p.State == engine.StateWaiting }, p.ID)
	if !got.Remote || got.Computer == nil || got.Computer.Name != "Desktop" || !got.Computer.Reachable || !got.Finished {
		t.Fatalf("the laptop does not mark the Ping as the desktop's: %+v", got)
	}
	if mine, _ := desk.C.Ping(p.ID); mine.Remote || mine.Computer == nil || mine.Computer.Name != "Desktop" {
		t.Fatalf("the desktop marks its own Ping wrongly: %+v", mine)
	}

	// A draft typed on the laptop shows on the box.
	if _, err := laptop.C.Draft(p.ID, map[string]string{"text": "after lunch"}); err != nil {
		t.Fatal(err)
	}
	shows(t, box, "the laptop's draft", func(p engine.Ping) bool { return strings.Contains(string(p.Draft), "after lunch") }, p.ID)

	// The Pong is sent from the laptop and applied on the desktop, where the Session waits.
	waited := make(chan engine.Ping, 1)
	go func() { w, _ := desk.C.Wait(p.ID); waited <- w }()
	sent, err := laptop.C.Pong(p.ID, client.SendPong{Answer: "Staging first", Text: "after lunch"})
	if err != nil {
		t.Fatal(err)
	}
	if sent.State != engine.StateAnswered {
		t.Fatalf("laptop got %+v", sent)
	}
	if w := <-waited; w.Pong == nil || w.Pong.Answer != "Staging first" {
		t.Fatalf("the Session on the desktop got %+v", w)
	}
	shows(t, box, "the Ping answered", func(p engine.Ping) bool { return p.State == engine.StateAnswered && p.Pong != nil }, p.ID)

	// A later Pong from the box loses and is told which Pong won.
	_, err = box.C.Pong(p.ID, client.SendPong{Answer: "Straight to production"})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusConflict || !strings.Contains(ce.Message, "Staging first") {
		t.Fatalf("the losing Pong was not told which won: %v", err)
	}
}

func TestFirstPongWinsWhenTwoDevicesAnswerAtOnce(t *testing.T) {
	desk, laptop, box := threeComputers(t)
	p := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	shows(t, box, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	errs := make(chan error, 2)
	go func() { _, err := laptop.C.Pong(p.ID, client.SendPong{Answer: "Staging first"}); errs <- err }()
	go func() { _, err := box.C.Pong(p.ID, client.SendPong{Answer: "Straight to production"}); errs <- err }()
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("want exactly one winner: %v, %v", e1, e2)
	}
}

func TestOtherComputersPingsJoinTheQueueAndTheEventStream(t *testing.T) {
	desk, laptop, _ := threeComputers(t)
	events, stop, err := laptop.C.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	p := newPing(t, desk.C)
	for e := range events {
		if e.Kind != engine.EventPingCreated {
			continue
		}
		var got engine.Ping
		json.Unmarshal(e.Data, &got)
		if got.ID == p.ID {
			if got.Computer == nil || got.Computer.Name != "Desktop" {
				t.Fatalf("event Ping not marked with its computer: %+v", got)
			}
			break
		}
	}
	list, err := laptop.C.Pings(engine.StateWaiting)
	if err != nil || len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("laptop Queue %+v %v", list, err)
	}
}

func TestHandBackHappensOnlyAtTheComputerRunningTheSession(t *testing.T) {
	desk, laptop, _ := threeComputers(t)
	p := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	_, err := laptop.C.HandBack(p.ID)
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusConflict || !strings.Contains(ce.Message, "Desktop") {
		t.Fatalf("hand back from the laptop: %v", err)
	}
}

func TestAComputerThatReturnsSendsItsPingsAgain(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	p := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	enginetest.RestartComputer(t, book, laptop) // its memory of the desktop's Pings is gone
	shows(t, laptop, "the Ping again", func(p engine.Ping) bool { return p.State == engine.StateWaiting }, p.ID)
}

func TestAnUnreachableComputersPingsSayso(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	p := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	enginetest.Stop(desk)
	shows(t, laptop, "the desktop unreachable", func(p engine.Ping) bool { return p.Computer != nil && !p.Computer.Reachable }, p.ID)
}

func TestTheQueueAcrossComputersFollowsTheLastPongOnAnyDevice(t *testing.T) {
	desk, laptop, box := threeComputers(t)
	ask := func(c *client.Client, session, q string, blocks bool) engine.Ping {
		p, err := c.CreatePing(client.NewPing{Session: session, Folder: "/tmp/" + session, Question: q,
			Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}, BlocksAgent: blocks, Finished: !blocks})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := ask(box.C, "box-session", "First question?", false)
	shows(t, laptop, "the box's Ping", func(engine.Ping) bool { return true }, first.ID)
	if _, err := laptop.C.Pong(first.ID, client.SendPong{Answer: "Yes"}); err != nil {
		t.Fatal(err)
	}
	older := ask(desk.C, "desk-session", "Older question?", false)
	newer := ask(box.C, "box-session", "Newer question?", false)
	blocking := ask(desk.C, "desk-other", "Run the migration?", true)
	var ids []string
	enginetest.Eventually(t, "the laptop's Queue", func() bool {
		ps, _ := laptop.C.Pings(engine.StateWaiting)
		ids = nil
		for _, p := range ps {
			ids = append(ids, p.ID)
		}
		return len(ids) == 3
	})
	want := []string{blocking.ID, newer.ID, older.ID}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("Queue %v, want blocking, then the last Pong's Session, then oldest: %v", ids, want)
		}
	}
}

func TestARevokedDevicesPingsDisappearAndItsPongsAreRefused(t *testing.T) {
	desk, laptop, box := threeComputers(t)
	boxPing := newPing(t, box.C)
	deskPing := newPing(t, desk.C)
	shows(t, laptop, "the box's Ping", func(engine.Ping) bool { return true }, boxPing.ID)
	shows(t, box, "the desktop's Ping", func(engine.Ping) bool { return true }, deskPing.ID)

	s, _ := box.C.Devices()
	if _, err := laptop.C.RevokeDevice(s.This); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*enginetest.Computer{desk, laptop} {
		enginetest.Eventually(t, c.Name+" to stop showing the box's Ping", func() bool {
			_, err := c.C.Ping(boxPing.ID)
			return err != nil
		})
		enginetest.Eventually(t, c.Name+" to drop the box from the set", func() bool {
			s, _ := c.C.Devices()
			return len(s.Devices) == 2
		})
	}
	// The box still shows the desktop's Ping from before; its Pong waits on the box and never applies.
	box.C.Pong(deskPing.ID, client.SendPong{Answer: "Staging first"})
	time.Sleep(300 * time.Millisecond)
	if p, _ := desk.C.Ping(deskPing.ID); p.State != engine.StateWaiting {
		t.Fatalf("the desktop's Ping changed after a revoked Device's Pong: %+v", p)
	}
}

func TestADeviceCannotRevokeItself(t *testing.T) {
	desk, laptop, _ := threeComputers(t)
	_ = desk
	s, _ := laptop.C.Devices()
	if _, err := laptop.C.RevokeDevice(s.This); err == nil {
		t.Fatal("a Device revoked itself")
	}
}

func TestAPongForAStoppedComputerWaitsAndIsDeliveredWhenItReturns(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	p := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	enginetest.Stop(desk)
	shows(t, laptop, "the desktop unreachable", func(p engine.Ping) bool { return p.Computer != nil && !p.Computer.Reachable }, p.ID)

	got, err := laptop.C.Pong(p.ID, client.SendPong{Answer: "Staging first"})
	if err != nil || got.State != engine.StateAnswered || got.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("the Pong was not kept as sent: %+v %v", got.Pong, err)
	}
	enginetest.RestartComputer(t, book, laptop) // the outbox survives the sender's restart
	enginetest.RestartComputer(t, book, desk)
	shows(t, desk, "the Pong delivered to the desktop's Ping", func(p engine.Ping) bool {
		return p.State == engine.StateAnswered && p.Pong.Answer == "Staging first"
	}, p.ID)
	shows(t, laptop, "the Ping answered", func(p engine.Ping) bool { return p.State == engine.StateAnswered && p.Pong != nil }, p.ID)
}

func TestAnUndeliveredPongComesBackToTheDeviceThatSentIt(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputerWith(t, book, "Desktop", engine.Config{DeliveryGrace: 100 * time.Millisecond})
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	p, err := desk.C.CreatePing(engine.NewPing{Session: "s", Question: "Run the migration?", BlocksAgent: true, Captured: true,
		Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	if err != nil {
		t.Fatal(err)
	}
	holdPrompt(t, desk.C, p.ID)() // the prompt that waited for it goes away
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	if _, err := laptop.C.Pong(p.ID, client.SendPong{Answer: "Yes"}); err != nil {
		t.Fatal(err)
	}
	shows(t, laptop, "the Pong undelivered", func(p engine.Ping) bool {
		return p.Pong != nil && p.Pong.Delivery == engine.DeliveryUndelivered && p.Pong.NotDelivered != ""
	}, p.ID)
	if _, err := laptop.C.Leave(p.ID); err != nil {
		t.Fatal(err)
	}
	shows(t, desk, "the Pong left unsent", func(p engine.Ping) bool { return p.Pong.Delivery == engine.DeliveryLeft }, p.ID)
}

func TestAPongWaitingToBeDeliveredCanBeWithdrawn(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputerWith(t, book, "Desktop", engine.Config{DeliveryGrace: time.Minute})
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)

	// Still in the laptop's outbox: taken back there.
	waiting := newPing(t, desk.C)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, waiting.ID)
	enginetest.Stop(desk)
	shows(t, laptop, "the desktop unreachable", func(p engine.Ping) bool { return !p.Computer.Reachable }, waiting.ID)
	laptop.C.Pong(waiting.ID, client.SendPong{Answer: "Staging first"})
	if p, err := laptop.C.WithdrawPong(waiting.ID); err != nil || p.State != engine.StateWaiting {
		t.Fatalf("withdraw from the outbox: %+v %v", p, err)
	}
	enginetest.RestartComputer(t, book, desk)
	shows(t, laptop, "the desktop back", func(p engine.Ping) bool { return p.Computer.Reachable }, waiting.ID)
	time.Sleep(200 * time.Millisecond)
	if p, _ := desk.C.Ping(waiting.ID); p.State != engine.StateWaiting {
		t.Fatalf("a withdrawn Pong arrived: %+v", p.Pong)
	}

	// Taken by the desktop but not yet delivered: the desktop takes it back.
	held, _ := desk.C.CreatePing(engine.NewPing{Session: "s2", Question: "Run the migration?", BlocksAgent: true, Captured: true,
		Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	holdPrompt(t, desk.C, held.ID)()
	shows(t, laptop, "the held Ping", func(engine.Ping) bool { return true }, held.ID)
	if _, err := laptop.C.Pong(held.ID, client.SendPong{Answer: "Yes"}); err != nil {
		t.Fatal(err)
	}
	shows(t, laptop, "the Pong sent", func(p engine.Ping) bool { return p.Pong != nil && p.Pong.Delivery == engine.DeliverySent }, held.ID)
	if _, err := laptop.C.WithdrawPong(held.ID); err != nil {
		t.Fatal(err)
	}
	shows(t, desk, "the Ping waiting again", func(p engine.Ping) bool { return p.State == engine.StateWaiting && p.Pong == nil }, held.ID)
}

func TestOnlyTheDeviceThatSentAPongCanWithdrawIt(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputerWith(t, book, "Desktop", engine.Config{DeliveryGrace: time.Minute})
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	p, _ := desk.C.CreatePing(engine.NewPing{Session: "s", Question: "Run the migration?", BlocksAgent: true, Captured: true,
		Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	holdPrompt(t, desk.C, p.ID)()
	if _, err := desk.C.Pong(p.ID, client.SendPong{Answer: "Yes"}); err != nil { // answered at the desktop
		t.Fatal(err)
	}
	shows(t, laptop, "the Pong sent", func(p engine.Ping) bool { return p.Pong != nil && p.Pong.Delivery == engine.DeliverySent }, p.ID)
	laptop.C.WithdrawPong(p.ID)
	time.Sleep(300 * time.Millisecond)
	if got, _ := desk.C.Ping(p.ID); got.State != engine.StateAnswered {
		t.Fatalf("the laptop withdrew a Pong sent at the desktop: %+v", got)
	}
}

func TestResourcesAndThreadsAreFetchedFromAnotherComputer(t *testing.T) {
	book := &enginetest.Book{}
	desk := enginetest.StartComputer(t, book, "Desktop")
	laptop := enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	folder := t.TempDir()
	big := make([]byte, 600*1024) // more than three parts
	for i := range big {
		big[i] = byte(i * 7)
	}
	copy(big, pngBytes)
	os.WriteFile(filepath.Join(folder, "shot.png"), big, 0o600)
	diff := "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"
	p, err := desk.C.CreatePing(client.NewPing{Finished: true, Session: "s1", Folder: folder, Question: "Ship this change?",
		Answers: []engine.Answer{{Label: "Ship it"}, {Label: "Hold"}},
		Resources: []engine.NewResource{
			{Title: "Checkout page screenshot", Kind: engine.KindImage, File: filepath.Join(folder, "shot.png")},
			{Title: "The change", Kind: engine.KindChange, Content: diff},
		}})
	if err != nil {
		t.Fatal(err)
	}
	got := shows(t, laptop, "the Ping", func(p engine.Ping) bool { return len(p.Resources) == 2 }, p.ID)
	img, ctype, err := laptop.C.Resource(p.ID, got.Resources[0].ID)
	if err != nil || !bytes.Equal(img, big) || !strings.HasPrefix(ctype, "image/png") {
		t.Fatalf("image: %d bytes, %q, %v", len(img), ctype, err)
	}
	code, _, err := laptop.C.Resource(p.ID, got.Resources[1].ID)
	if err != nil || string(code) != diff {
		t.Fatalf("code change: %q %v", code, err)
	}
	th, err := laptop.C.Thread("", "s1")
	if err != nil || len(th.Pings) == 0 || !th.Remote {
		t.Fatalf("thread from the desktop: %+v %v", th, err)
	}

	enginetest.Stop(desk)
	shows(t, laptop, "the desktop unreachable", func(p engine.Ping) bool { return !p.Computer.Reachable }, p.ID)
	if _, _, err := laptop.C.Resource(p.ID, got.Resources[1].ID); !isStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("a Resource from an unreachable computer: %v", err)
	}
}
