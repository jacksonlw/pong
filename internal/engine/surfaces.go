package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Where the human works in each Harness (start_runner.go) is kept by the
// Engine, so a start from any interface opens the Session the same way.

func (s *starter) surfaceTable() error {
	_, err := s.pr.db.Exec(`CREATE TABLE IF NOT EXISTS surfaces (harness TEXT PRIMARY KEY, surface TEXT NOT NULL)`)
	return err
}

// surface is where the human works in harness: what they chose, else the
// app when any Session of theirs in the Harness ran in it, else a terminal.
// (Not their latest Session only: a Session Pong starts runs in a terminal
// first, and would turn the answer round.)
func (s *starter) surface(harness string) string {
	var chosen string
	if err := s.pr.db.QueryRow(`SELECT surface FROM surfaces WHERE harness = ?`, harness).Scan(&chosen); err == nil {
		return chosen
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Surfaces[0]
	}
	all, _ := s.pr.sessions()
	for _, x := range all {
		if x.Harness == harness && x.AppCommand != "" {
			return SurfaceApp
		}
	}
	return Surfaces[0]
}

// withSurface says whether Pong can open Sessions in the Harness and, where
// that makes a difference (AppSharer), where the human works in it.
func (s *starter) withSurface(h HarnessStatus) HarnessStatus {
	r := s.runners[h.ID]
	h.CanStart = r != nil
	if _, ok := r.(AppSharer); ok {
		h.Surface = s.surface(h.ID)
	}
	return h
}

func (s *starter) surfaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /harnesses/{id}/surface", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var in struct {
			Surface string `json:"surface"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || !oneOf(in.Surface, Surfaces) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("surface %q is not where a human works: use one of %v", in.Surface, Surfaces))
			return
		}
		if _, ok := s.runners[id].(AppSharer); !ok {
			writeError(w, http.StatusNotFound, ErrNoHarness.Error()+" whose Sessions Pong opens differently by where you work")
			return
		}
		if _, err := s.pr.db.Exec(`INSERT INTO surfaces (harness, surface) VALUES (?, ?)
			ON CONFLICT (harness) DO UPDATE SET surface = excluded.surface`, id, in.Surface); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"surface": in.Surface})
	})
}
