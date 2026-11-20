package engine

import (
	"encoding/json"
	"errors"
	"net/http"
)

func (s *projects) sessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /sessions", func(w http.ResponseWriter, r *http.Request) {
		var in RegisterSession
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "body is not a Session: "+err.Error())
			return
		}
		if msg := in.validate(); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		sess, err := s.register(in)
		writeSession(w, sess, err)
	})
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		all, err := s.sessions()
		writeSession(w, all, err)
	})
	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		sess, err := s.session(q.Get("harness"), q.Get("id"))
		writeSession(w, sess, err)
	})
	mux.HandleFunc("GET /thread", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		t, err := s.thread(q.Get("harness"), q.Get("id"))
		if errors.Is(err, errNotFound) && s.pings.set != nil {
			// A Session on another computer: its Thread is fetched from it now.
			raw, ferr := s.pings.set.fetchThread(q.Get("harness"), q.Get("id"))
			var un errUnreachable
			switch {
			case errors.As(ferr, &un):
				writeError(w, http.StatusServiceUnavailable, ferr.Error()+": its Thread opens once it is back")
			case ferr != nil:
				writeSession(w, nil, ferr)
			default:
				var t map[string]any
				if json.Unmarshal(raw, &t) != nil || t == nil {
					writeError(w, http.StatusBadGateway, "the Thread arrived unreadable")
					return
				}
				t["remote"] = true // opened in its Harness only on its own computer
				writeJSON(w, http.StatusOK, t)
			}
			return
		}
		t.TakesText = err == nil && s.takesText != nil && s.takesText(t.Session)
		t.StopsTurns = err == nil && s.stopsTurns != nil && s.stopsTurns(t.Session)
		writeSession(w, t, err)
	})
}

func writeSession(w http.ResponseWriter, v any, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, "no such Session: check harness and id")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusOK, v)
	}
}
