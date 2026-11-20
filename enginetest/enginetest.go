// Package enginetest starts a real Engine for black-box tests.
package enginetest

import (
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// WaitHold is how long a wait for a Pong stays open in tests.
const WaitHold = 50 * time.Millisecond

var (
	mu      sync.Mutex
	running = map[string]*engine.Engine{} // by data directory
	configs = map[string]engine.Config{}  // by data directory, for Restart
)

// Start runs a real Engine on a temporary data directory and an ephemeral
// loopback port, and stops it when the test ends. The returned client carries
// the address and secret; Dir is the data directory holding the address file.
func Start(t testing.TB) (c *client.Client, dir string) {
	t.Helper()
	dir = t.TempDir()
	return start(t, dir), dir
}

// Restart stops the Engine started on dir and starts a new one on the same
// data directory, returning a client for it.
func Restart(t testing.TB, dir string) *client.Client {
	t.Helper()
	mu.Lock()
	e := running[dir]
	cfg := configs[dir]
	mu.Unlock()
	if e != nil {
		e.Close()
	}
	return startCfg(t, dir, cfg)
}

// StartWith is Start with cfg's Retention, SweepEvery, Env, Runners, Now and
// DeliveryGrace.
func StartWith(t testing.TB, cfg engine.Config) *client.Client {
	t.Helper()
	return startCfg(t, t.TempDir(), cfg)
}

// StartWithDir is StartWith, also returning the data directory.
func StartWithDir(t testing.TB, cfg engine.Config) (*client.Client, string) {
	t.Helper()
	dir := t.TempDir()
	return startCfg(t, dir, cfg), dir
}

// RestartWith is Restart with cfg, as StartWith takes it.
func RestartWith(t testing.TB, dir string, cfg engine.Config) *client.Client {
	t.Helper()
	mu.Lock()
	e := running[dir]
	mu.Unlock()
	if e != nil {
		e.Close()
	}
	return startCfg(t, dir, cfg)
}

func start(t testing.TB, dir string) *client.Client {
	t.Helper()
	return startCfg(t, dir, engine.Config{})
}

func startCfg(t testing.TB, dir string, cfg engine.Config) *client.Client {
	t.Helper()
	if cfg.Launch == nil {
		// A test never opens a terminal or an app on the machine it runs on.
		cfg.Launch = func(engine.Launch) error { return nil }
	}
	e, err := engine.Start(engine.Config{Launch: cfg.Launch, Dir: dir, Port: 0, WaitHold: WaitHold, Retention: cfg.Retention, SweepEvery: cfg.SweepEvery, Env: cfg.Env, Runners: cfg.Runners, Harnesses: cfg.Harnesses, Now: cfg.Now, Keychain: cfg.Keychain, DeliveryGrace: cfg.DeliveryGrace, Devices: cfg.Devices, Diagnostics: cfg.Diagnostics, SessionCheckEvery: cfg.SessionCheckEvery})
	if err != nil {
		t.Fatalf("starting engine: %v", err)
	}
	mu.Lock()
	running[dir] = e
	configs[dir] = cfg
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if running[dir] == e {
			delete(running, dir)
			e.Close()
		}
	})
	return client.New(e.Addr.Address, e.Addr.Secret)
}
