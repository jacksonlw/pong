package devices

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// Device is one Device in the set as the local API reports it.
type Device struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	Engine    bool   `json:"engine"`
	This      bool   `json:"this"`
	Reachable bool   `json:"reachable"`
}

// Asking is a Device waiting for approval.
type Asking struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Platform string    `json:"platform"`
	Code     string    `json:"code"`
	AskedAt  time.Time `json:"asked_at"`
}

// Pairing is this Device's own request to join, while it lasts.
type Pairing struct {
	Code   string `json:"code"`
	Target Device `json:"target"`
	Joined bool   `json:"joined"`
	// Confirm is set while asking through a carrier: the human must say on
	// this Device that the code matches before it joins.
	Confirm bool `json:"confirm,omitempty"`
}

// Set is GET /devices.
type Set struct {
	Mode    string   `json:"mode"`              // "local", or the carrier's name while it is active
	Why     string   `json:"why,omitempty"`     // why it is on Local while a carrier could take over
	Account bool     `json:"account,omitempty"` // a carrier holds an account for this Device
	This    string   `json:"this"`
	Devices []Device `json:"devices"`
	Waiting []Asking `json:"waiting"`
	Pairing *Pairing `json:"pairing,omitempty"`
}

// Set reports the set as this Device sees it.
func (n *Node) Set() Set {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := Set{Mode: "local", This: n.ID(), Devices: []Device{}, Waiting: []Asking{}}
	active := n.active()
	for id, d := range n.members {
		_, up := n.peers[id]
		up = up || active && n.carrier.Reachable(id)
		s.Devices = append(s.Devices, Device{ID: id, Name: d.Name, Platform: d.Platform, Engine: d.Engine, This: id == n.ID(), Reachable: up || id == n.ID()})
	}
	sort.Slice(s.Devices, func(i, j int) bool {
		if s.Devices[i].This != s.Devices[j].This {
			return s.Devices[i].This
		}
		return s.Devices[i].Name < s.Devices[j].Name
	})
	for id, a := range n.waiting {
		s.Waiting = append(s.Waiting, Asking{ID: id, Name: a.info.Name, Platform: a.info.Platform, Code: n.askingCode(a.req), AskedAt: a.at})
	}
	sort.Slice(s.Waiting, func(i, j int) bool { return s.Waiting[i].AskedAt.Before(s.Waiting[j].AskedAt) })
	if p := n.pairing; p != nil {
		s.Pairing = &Pairing{Code: p.code, Joined: p.joined, Target: deviceOf(p.target)}
	}
	if n.carrier != nil {
		n.carrier.Status(&s) // the Node's lock is held: it must not call back
	}
	return s
}

func deviceOf(d wire.DeviceInfo) Device {
	return Device{ID: d.ID, Name: d.Name, Platform: d.Platform, Engine: d.Engine}
}

// Routes mounts the Devices endpoints on the local API.
func (n *Node) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, n.Set())
	})
	mux.HandleFunc("GET /devices/found", func(w http.ResponseWriter, r *http.Request) {
		f := n.Find()
		if f == nil {
			f = []Found{}
		}
		writeJSON(w, http.StatusOK, f)
	})
	mux.HandleFunc("POST /devices/pair", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Address == "" {
			writeError(w, http.StatusBadRequest, "address is required")
			return
		}
		code, target, err := n.Pair(body.Address)
		switch {
		case errors.Is(err, ErrInASet):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			writeError(w, http.StatusBadGateway, err.Error())
		default:
			writeJSON(w, http.StatusOK, Pairing{Code: code, Target: deviceOf(target)})
		}
	})
	if n.carrier != nil {
		n.carrier.Routes(mux)
	}
	mux.HandleFunc("POST /devices/leave", func(w http.ResponseWriter, r *http.Request) {
		told, err := n.Leave()
		if errors.Is(err, ErrNoOneToTell) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"told": told})
	})
	mux.HandleFunc("POST /devices/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		switch err := n.Revoke(r.PathValue("id")); {
		case errors.Is(err, ErrNotMember):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrRevokeSelf):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeJSON(w, http.StatusOK, n.Set())
		}
	})
	mux.HandleFunc("POST /devices/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		switch err := n.Approve(r.PathValue("id")); {
		case errors.Is(err, ErrOtherSet):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrNotWaiting):
			writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
		default:
			writeJSON(w, http.StatusOK, n.Set())
		}
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
