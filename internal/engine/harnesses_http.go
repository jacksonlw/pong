package engine

import (
	"errors"
	"net/http"
)

// HarnessStatus is what the Engine reports about Pong's integration in one
// Harness.
type HarnessStatus struct {
	ID        string `json:"id"`        // name used on the command line, e.g. "claude-code"
	Name      string `json:"name"`      // e.g. "Claude Code"
	Installed bool   `json:"installed"` // the Harness is on this computer
	Connected bool   `json:"connected"` // Pong's integration is installed in it
	CanStart  bool   `json:"can_start"` // Pong can open Sessions in it (it has a Runner)
	// Surface is where the human works in it, one of Surfaces, as they chose
	// (PUT /harnesses/{id}/surface) or as their Sessions suggest. Only
	// on a Harness that shares a terminal Session with its app (AppSharer).
	Surface string `json:"surface,omitempty"`
	Version string `json:"version,omitempty"`
	Warning string `json:"warning,omitempty"` // untested version or a manual step outstanding
	Note    string `json:"note"`              // the whole status in a sentence or two
}

// Harnesses reports on and repairs Pong's integration in each Harness. The
// adapters live outside the Engine (they import its client), so the command
// that runs the Engine supplies them.
type Harnesses interface {
	List() []HarnessStatus
	// Repair installs Pong's integration in the Harness again. ErrNoHarness
	// when id names no Harness Pong knows.
	Repair(id string) (HarnessStatus, error)
}

// ErrNoHarness is returned by Harnesses.Repair for an unknown id.
var ErrNoHarness = errors.New("no such Harness")

func harnessRoutes(mux *http.ServeMux, h Harnesses, canStart func(HarnessStatus) HarnessStatus) {
	mux.HandleFunc("GET /harnesses", func(w http.ResponseWriter, r *http.Request) {
		list := []HarnessStatus{}
		if h != nil {
			for _, s := range h.List() {
				list = append(list, canStart(s))
			}
		}
		writeJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("POST /harnesses/{id}/repair", func(w http.ResponseWriter, r *http.Request) {
		if h == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": ErrNoHarness.Error()})
			return
		}
		s, err := h.Repair(r.PathValue("id"))
		switch {
		case errors.Is(err, ErrNoHarness):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusOK, canStart(s))
		}
	})
}
