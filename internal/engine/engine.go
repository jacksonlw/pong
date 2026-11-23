// Package engine is the Engine: it serves the local API on the loopback address.
package engine

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jacksonlw/pong/internal/devices"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Version is the Engine's version, set at build time with -ldflags "-X pong/internal/engine.Version=...".
var Version = "dev"

// DefaultPort is the fixed port the Engine tries first.
const DefaultPort = 47474

// AddressFile is the name of the file, inside the data directory, that holds
// the Engine's address and secret.
const AddressFile = "engine.json"

// Address is the content of the address file.
type Address struct {
	Address string `json:"address"` // host:port actually in use
	Secret  string `json:"secret"`
}

// Config says where the Engine keeps its data and which port it tries first.
type Config struct {
	Dir  string // data directory; holds the address file
	Port int    // preferred port; 0 means any free port
	// WaitHold is how long a wait for a Pong stays open before answering
	// with the Ping as it stands; 0 means DefaultWaitHold.
	WaitHold time.Duration
	// Retention is how long a Ping that is no longer waiting, and its
	// Resources, are kept; 0 means DefaultRetention.
	Retention time.Duration
	// SweepEvery is how often Pings past retention are removed; 0 means
	// DefaultSweepEvery.
	SweepEvery time.Duration
	// Harnesses answers GET /harnesses and POST /harnesses/{id}/repair; nil
	// reports none.
	Harnesses Harnesses
	// Env is the human's shell environment, used to find and run each
	// Harness's tool; nil means the Engine's own (see LoginEnv).
	Env []string
	// Runners say how a Session is opened where the human sees it, one per
	// Harness; Launch opens what they return (OpenLaunch when nil).
	Runners []Runner
	Launch  func(Launch) error
	// Now is the clock Pings are stamped with; nil means the real one.
	Now func() time.Time
	// DeliveryGrace is how long a sent Pong whose prompt was held waits for a
	// hold to come back; 0 means DefaultDeliveryGrace.
	DeliveryGrace time.Duration
	// Keychain keeps voice mode's API key; nil keeps none (tests never touch
	// the real Keychain).
	Keychain Keychain
	// Devices, when set, makes this Engine part of the human's set of
	// Devices: it listens on the network and connects to the set.
	// nil keeps it a single-Device Engine with no network listener.
	Devices *devices.Config
	// SessionCheckEvery is how often Sessions whose Harness process is gone
	// are ended; 0 means DefaultSessionCheckEvery.
	SessionCheckEvery time.Duration
	// SessionQuietFor is how long a Session with no Harness process of its
	// own may go unchanged before it is ended; 0 means DefaultSessionQuietFor.
	SessionQuietFor time.Duration
	// Diagnostics, when set, is the folder where every event is also
	// appended to a dated journal that retention never sweeps, so real use
	// can be studied later. Only the real Engine sets it; "" keeps none.
	Diagnostics string
}

// DiagnosticsFolder is the folder, inside the real data directory, that holds
// the diagnostics journal.
const DiagnosticsFolder = "diagnostics"

// DefaultWaitHold is how long a wait for a Pong stays open by default.
const DefaultWaitHold = 25 * time.Second

var (
	testDirOnce sync.Once
	testDir     string
	testDirErr  error
)

// DefaultDir is the data directory used when none is given: $PONG_HOME, or
// ~/Library/Application Support/Pong on the Mac and $XDG_DATA_HOME/pong
// (~/.local/share/pong) on Linux.
func DefaultDir() (string, error) {
	if d := os.Getenv("PONG_HOME"); d != "" {
		return d, nil
	}
	if testing.Testing() {
		// A test that names no data folder gets a scratch one, the same for
		// the whole test binary, so no test reaches the human's real data.
		testDirOnce.Do(func() { testDir, testDirErr = os.MkdirTemp("", "pong-test-") })
		return testDir, testDirErr
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "pong"), nil
		}
		return filepath.Join(home, ".local", "share", "pong"), nil
	}
	return filepath.Join(home, "Library", "Application Support", "Pong"), nil
}

// Engine is a running Engine.
type Engine struct {
	Addr    Address
	Devices *devices.Node // nil unless Config.Devices was set
	server  *http.Server
	db      *sql.DB
	pings   *pings
	done    chan error
}

// Start listens on the loopback address, writes the address file and serves
// the API until Close.
func Start(cfg Config) (*Engine, error) {
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, err
	}
	secret, err := loadOrCreateSecret(cfg.Dir)
	if err != nil {
		return nil, err
	}
	db, err := openStore(cfg.Dir)
	if err != nil {
		return nil, err
	}
	ps, err := newPings(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	ps.dir = cfg.Dir
	ps.diag = cfg.Diagnostics
	ps.loadSeq()
	ps.now = cfg.Now
	pr, err := newProjects(db, ps, cfg.Dir)
	if err != nil {
		db.Close()
		return nil, err
	}
	st, err := newSettings(db, ps)
	if err != nil {
		db.Close()
		return nil, err
	}
	hold := cfg.WaitHold
	if hold == 0 {
		hold = DefaultWaitHold
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)))
	if err != nil && cfg.Port != 0 {
		// The preferred port is taken: fall back to any free port.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	e := &Engine{Addr: Address{Address: ln.Addr().String(), Secret: secret}, db: db, pings: ps, done: make(chan error, 1)}
	if err := writeAddress(cfg.Dir, e.Addr); err != nil {
		ln.Close()
		db.Close()
		return nil, err
	}
	mux := routes()
	ps.routes(mux, hold)
	ps.allowedRoutes(mux)
	ps.mountMCP(mux, hold)

	pr.routes(mux)
	st.routes(mux)
	mux.HandleFunc("GET /directory", func(w http.ResponseWriter, r *http.Request) {
		d, err := directory(ps, pr, st)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, d)
	})
	voiceKeyRoutes(mux, cfg.Keychain)
	starter := newStarter(pr, ps, cfg.Dir, cfg.Env, cfg.Runners, cfg.Launch)
	starter.hold = hold
	if cfg.Devices != nil {
		starter.name = cfg.Devices.Name
	}
	if starter.name == "" {
		host, _ := os.Hostname()
		starter.name = strings.TrimSuffix(host, ".local")
	}
	if err := starter.surfaceTable(); err != nil {
		ln.Close()
		db.Close()
		return nil, err
	}
	starter.routes(mux)
	harnessRoutes(mux, cfg.Harnesses, starter.withSurface)
	reader, _ := cfg.Harnesses.(SkillReader)
	skills := newSkillNames(reader, pr)
	pr.sessionSeen = skills.sessionSeen
	if cfg.DeliveryGrace != 0 {
		ps.grace = cfg.DeliveryGrace
	}
	if err := ps.recover(); err != nil {
		ln.Close()
		db.Close()
		return nil, err
	}
	if cfg.Devices != nil {
		dc := *cfg.Devices
		dc.Dir = cfg.Dir
		node, err := devices.Start(dc)
		if err != nil {
			ln.Close()
			db.Close()
			return nil, fmt.Errorf("joining the set of Devices: %w", err)
		}
		e.Devices = node
		node.Routes(mux)
		ps.mu.Lock()
		ps.set = newAcross(node, ps)
		ps.set.thread = pr.thread
		ps.set.pr = pr
		starter.comp = newComputers(ps.set, starter, cfg.Harnesses, cfg.Runners)
		starter.comp.routes(mux)
		starter.comp.skills = skills
		skills.changed = func() { starter.comp.report("") }
		skills.remote = starter.comp.remoteSkills
		ps.mu.Unlock()
	}
	skills.routes(mux, func() string {
		if e.Devices == nil {
			return ""
		}
		return e.Devices.ID()
	})
	if cfg.Harnesses != nil && reader != nil {
		go func() {
			var ids []string
			for _, h := range cfg.Harnesses.List() {
				ids = append(ids, h.ID)
			}
			skills.readAll(ids)
		}()
	}
	e.server = &http.Server{Handler: guard(secret, mux)}
	retention, every := cfg.Retention, cfg.SweepEvery
	if retention == 0 {
		retention = DefaultRetention
	}
	if every == 0 {
		every = DefaultSweepEvery
	}
	go ps.sweepEvery(retention, every)
	check := cfg.SessionCheckEvery
	if check == 0 {
		check = DefaultSessionCheckEvery
	}
	quiet := cfg.SessionQuietFor
	if quiet == 0 {
		quiet = DefaultSessionQuietFor
	}
	go pr.endGoneEvery(check, quiet, ps.closing)
	go func() { e.done <- e.server.Serve(ln) }()
	return e, nil
}

// Close stops the Engine.
func (e *Engine) Close() error {
	close(e.pings.closing)
	if e.Devices != nil {
		e.Devices.Close()
		e.pings.set.close()
	}
	err := e.server.Shutdown(context.Background())
	if serr := <-e.done; !errors.Is(serr, http.ErrServerClosed) && err == nil {
		err = serr
	}
	if derr := e.db.Close(); err == nil {
		err = derr
	}
	return err
}

func routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": Version})
	})
	return mux
}

// guard refuses requests without the secret, and requests whose Host or
// Origin is not local, so web pages cannot reach the Engine.
func guard(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			writeError(w, http.StatusForbidden, "host is not local")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !localOrigin(o) {
			writeError(w, http.StatusForbidden, "origin is not local")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or wrong secret")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func localOrigin(origin string) bool {
	for _, scheme := range []string{"http://", "https://"} {
		if len(origin) > len(scheme) && origin[:len(scheme)] == scheme {
			return localHost(origin[len(scheme):])
		}
	}
	return false
}

func loadOrCreateSecret(dir string) (string, error) {
	var a Address
	if b, err := os.ReadFile(filepath.Join(dir, AddressFile)); err == nil && json.Unmarshal(b, &a) == nil && a.Secret != "" {
		return a.Secret, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeAddress(dir string, a Address) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, AddressFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, AddressFile))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
