package enginetest

import (
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/internal/engine"
)

// Book stands in for DNS-SD: every Engine in a test set finds every address
// in it.
type Book struct {
	mu    sync.Mutex
	addrs []string
	// Carrier, when set, makes the carrier for each Engine started with this
	// book.
	Carrier func() devices.Carrier
}

func (b *Book) Add(addr string) {
	b.mu.Lock()
	b.addrs = append(b.addrs, addr)
	b.mu.Unlock()
}

func (b *Book) Advertise(string, int) error { return nil }
func (b *Book) Close()                      {}
func (b *Book) Browse() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.addrs...)
}

// Computer is one Engine of a test set.
type Computer struct {
	Name    string
	C       *client.Client
	Dir     string
	NetAddr string
}

// StartComputer starts a real Engine on loopback that takes part in a set,
// found through book. It is a set of one until paired.
func StartComputer(t testing.TB, book *Book, name string) *Computer {
	t.Helper()
	return StartComputerWith(t, book, name, engine.Config{})
}

// StartComputerWith is StartComputer with cfg, as StartWith takes it.
func StartComputerWith(t testing.TB, book *Book, name string, cfg engine.Config) *Computer {
	t.Helper()
	dir := t.TempDir()
	cfg.Devices = &devices.Config{Name: name, Platform: "linux", NetAddr: "127.0.0.1:0", Discovery: book, DialEvery: 50 * time.Millisecond}
	if book.Carrier != nil {
		cfg.Devices.Carrier = book.Carrier()
	}
	c := startCfg(t, dir, cfg)
	mu.Lock()
	addr := running[dir].Devices.NetAddr
	mu.Unlock()
	book.Add(addr)
	return &Computer{Name: name, C: c, Dir: dir, NetAddr: addr}
}

// RestartComputer restarts a test set's Engine on its data directory. Its
// network address changes, so it is added to book again.
func RestartComputer(t testing.TB, book *Book, m *Computer) {
	t.Helper()
	m.C = Restart(t, m.Dir)
	mu.Lock()
	m.NetAddr = running[m.Dir].Devices.NetAddr
	mu.Unlock()
	book.Add(m.NetAddr)
}

// Stop stops a test set's Engine.
func Stop(m *Computer) {
	mu.Lock()
	e := running[m.Dir]
	delete(running, m.Dir)
	mu.Unlock()
	if e != nil {
		e.Close()
	}
}

// Eventually retries f until it returns true or 5 seconds pass.
func Eventually(t testing.TB, what string, f func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Pair pairs joiner into approver's set the way a human does, checking the
// two codes match.
func Pair(t testing.TB, approver, joiner *Computer) {
	t.Helper()
	p, err := joiner.C.Pair(approver.NetAddr)
	if err != nil {
		t.Fatalf("pairing %s with %s: %v", joiner.Name, approver.Name, err)
	}
	var id string
	Eventually(t, approver.Name+" to see "+joiner.Name+" asking", func() bool {
		s, _ := approver.C.Devices()
		for _, w := range s.Waiting {
			if w.Code == p.Code {
				id = w.ID
				return true
			}
		}
		return false
	})
	if _, err := approver.C.ApproveDevice(id); err != nil {
		t.Fatal(err)
	}
	Eventually(t, joiner.Name+" to join", func() bool {
		s, _ := joiner.C.Devices()
		return s.Pairing != nil && s.Pairing.Joined
	})
}
