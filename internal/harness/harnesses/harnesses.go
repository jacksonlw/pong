// Package harnesses is every Harness adapter Pong has, in one list, for the
// commands that set up, report on and remove Pong in all of them at once.
package harnesses

import (
	"slices"

	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
	"github.com/jacksonlw/pong/internal/harness/codex"
	"github.com/jacksonlw/pong/internal/harness/generic"
)

// Entry is one adapter and the name used for it on the command line.
type Entry struct {
	ID string
	harness.Adapter
}

// All returns every adapter. engineDir is the Engine's data directory, which
// the Claude Code adapter asks whether Sessions have connected.
func All(engineDir string) []Entry { return all(engineDir, nil) }

// all is All with the human's shell environment, in which an adapter finds
// its Harness's command; nil means this process's own.
func all(engineDir string, env []string) []Entry {
	es := []Entry{
		{claudecode.HarnessName, claudecode.Adapter{Engine: engineDir, Env: env}},
		{"codex", codex.Adapter{}},
	}
	for _, h := range generic.Known {
		es = append(es, Entry{h.ID, h})
	}
	return es
}

// Find returns the adapter with this ID.
func Find(engineDir, id string) (Entry, bool) {
	for _, e := range All(engineDir) {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// Report is e's status in the Engine's shape.
func Report(e Entry, home string) engine.HarnessStatus {
	s := e.Status(home)
	return engine.HarnessStatus{ID: e.ID, Name: e.Name(), Installed: s.Detected, Connected: s.Connected,
		Version: s.Version, Warning: s.Warning, Note: s.Note}
}

// ForEngine serves the Engine's /harnesses endpoints for the human whose home
// folder is Home, installing the pong command at Pong. Env is the human's
// shell environment as the Engine captured it for Sessions: an Engine started
// by Pong.app has no claude on its own PATH.
type ForEngine struct {
	Home, Pong, EngineDir string
	Env                   []string
}

var _ engine.Harnesses = ForEngine{}

// List reports every Harness.
func (f ForEngine) List() []engine.HarnessStatus {
	var out []engine.HarnessStatus
	for _, e := range all(f.EngineDir, f.Env) {
		out = append(out, Report(e, f.Home))
	}
	return out
}

// Repair takes Pong out of the Harness and installs it again, which mends
// entries changed by hand.
func (f ForEngine) Repair(id string) (engine.HarnessStatus, error) {
	es := all(f.EngineDir, f.Env)
	i := slices.IndexFunc(es, func(e Entry) bool { return e.ID == id })
	if i < 0 {
		return engine.HarnessStatus{}, engine.ErrNoHarness
	}
	e := es[i]
	// Remove takes Pong out even when the Harness's command is gone, and
	// Install could not then put it back.
	if c, ok := e.Adapter.(interface{ CanInstall() error }); ok {
		if err := c.CanInstall(); err != nil {
			return engine.HarnessStatus{}, err
		}
	}
	if err := e.Remove(f.Home); err != nil {
		return engine.HarnessStatus{}, err
	}
	if err := e.Install(f.Home, f.Pong); err != nil {
		return engine.HarnessStatus{}, err
	}
	return Report(e, f.Home), nil
}
