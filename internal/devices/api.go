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

// Offer is one Device that answered this Device's request to join, with the
// code the two made together. That Device shows the same code.
type Offer struct {
	Device Device `json:"device"`
	Code   string `json:"code"`
}

// Pairing is this Device's own request to join, while it lasts.
type Pairing struct {
	// Offers are the Devices that answered: one on Local, and through a
	// carrier every Device of the set that is there to answer.
	Offers []Offer `json:"offers"`
	Joined bool    `json:"joined"`
	// Confirm is set until the human said on this Device that the code of
	// one offer matches. It joins no set before that.
	Confirm bool `json:"confirm,omitempty"`
}

// Fork is a chain that forks from this Device's in a way only the human can
// settle: the Devices that wrote each side from where they differ. Mutual is
// set when each side revokes the other's writers; their envelopes wait.
type Fork struct {
	Ours   []Device `json:"ours"`
	Theirs []Device `json:"theirs"`
	Mutual bool     `json:"mutual"`
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
	Fork    *Fork    `json:"fork,omitempty"`
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
		if code := a.answer.Code(); code != "" { // nothing to check or approve before there is a code
			s.Waiting = append(s.Waiting, Asking{ID: id, Name: a.info.Name, Platform: a.info.Platform, Code: code, AskedAt: a.at})
		}
	}
	sort.Slice(s.Waiting, func(i, j int) bool { return s.Waiting[i].AskedAt.Before(s.Waiting[j].AskedAt) })
	if p := n.pairing; p != nil {
		s.Pairing = &Pairing{Offers: []Offer{}, Joined: p.joined, Confirm: !p.confirmed}
		if code := p.code(); code != "" {
			s.Pairing.Offers = append(s.Pairing.Offers, Offer{Device: deviceOf(p.target), Code: code})
		}
	}
	if hf := n.fork; hf != nil {
		// Writers are named from either chain: one may be in the offered chain alone.
		known := map[string]wire.DeviceInfo{}
		for _, c := range []wire.Chain{n.chain, hf.offer} {
			for _, r := range c {
				if r.Kind != wire.Revoke {
					known[r.Device.ID] = r.Device
				}
			}
		}
		named := func(ids []string) []Device {
			out := []Device{}
			for _, id := range ids {
				d := deviceOf(known[id])
				d.ID, d.This = id, id == n.ID()
				out = append(out, d)
			}
			return out
		}
		s.Fork = &Fork{Ours: named(hf.fork.Ours), Theirs: named(hf.fork.Theirs), Mutual: hf.fork.Mutual}
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
			writeJSON(w, http.StatusOK, Pairing{Offers: []Offer{{Device: deviceOf(target), Code: code}}, Confirm: true})
		}
	})
	mux.HandleFunc("POST /devices/pair/confirm", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Match bool `json:"match"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		n.Confirm(body.Match)
		writeJSON(w, http.StatusOK, n.Set())
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
	mux.HandleFunc("POST /devices/fork", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Keep string `json:"keep"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		switch err := n.SettleFork(body.Keep); {
		case errors.Is(err, ErrNoFork):
			writeError(w, http.StatusNotFound, err.Error())
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
