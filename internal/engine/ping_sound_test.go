package engine_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// fakeClock is the Engine's clock in tests that move time on.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// A Ping is its Session's first when no other Ping of the Session awaits a
// Pong and the last one was answered more than five minutes ago, or there is
// none. Notes do not count, and are never first.
func TestFirstPingOrFollowUp(t *testing.T) {
	type step func(c *client.Client, clock *fakeClock)
	ping := func(c *client.Client, note bool) engine.Ping {
		t.Helper()
		in := client.NewPing{Session: "s1", Folder: "/tmp/project", Question: "Ship the fix to staging first?", IsNote: note, Finished: !note}
		if !note {
			in.Answers = []engine.Answer{{Label: "Staging first"}, {Label: "Straight to production"}}
		}
		p, err := c.CreatePing(in)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	answeredAgo := func(d time.Duration) step {
		return func(c *client.Client, clock *fakeClock) {
			p := ping(c, false)
			if _, err := c.Pong(p.ID, client.SendPong{Answer: "Staging first"}); err != nil {
				t.Fatal(err)
			}
			clock.add(d)
		}
	}
	cases := []struct {
		name   string
		before step
		first  bool
	}{
		{"no earlier Ping", func(*client.Client, *fakeClock) {}, true},
		{"one awaiting a Pong", func(c *client.Client, _ *fakeClock) { ping(c, false) }, false},
		{"last answered four minutes ago", answeredAgo(4 * time.Minute), false},
		{"last answered six minutes ago", answeredAgo(6 * time.Minute), true},
		{"earlier Pings are Notes", func(c *client.Client, _ *fakeClock) { ping(c, true); ping(c, true) }, true},
		{"a Note after a recent answer", func(c *client.Client, clock *fakeClock) {
			answeredAgo(time.Minute)(c, clock)
			ping(c, true)
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
			c := enginetest.StartWith(t, engine.Config{Now: clock.now})
			tc.before(c, clock)
			if got := ping(c, false); got.First != tc.first {
				t.Fatalf("first = %v, want %v", got.First, tc.first)
			}
		})
	}
	t.Run("a Note is never first", func(t *testing.T) {
		c, _ := enginetest.Start(t)
		if p := ping(c, true); p.First {
			t.Fatalf("got %+v", p)
		}
	})
	t.Run("another Session's Ping does not count", func(t *testing.T) {
		c, _ := enginetest.Start(t)
		ping(c, false)
		p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s2", Folder: "/tmp/project", Question: "Ship the fix to staging first?",
			Answers: []engine.Answer{{Label: "Staging first"}, {Label: "Straight to production"}}})
		if err != nil || !p.First {
			t.Fatalf("got %+v, %v", p, err)
		}
	})
}
