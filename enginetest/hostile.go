package enginetest

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/jacksonlw/pong/wire"
)

// HostilePeer stands on the network between Engines: Engines that find it
// connect to it, and it connects on to the real listener behind it, copying
// and recording every frame both ways. It can then send the recorded
// envelopes again, altered, or made up, to that listener.
var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

type HostilePeer struct {
	Addr   string
	target string
	ln     net.Listener

	mu       sync.Mutex
	Recorded [][]byte
	upstream []*websocket.Conn
	umu      sync.Mutex
}

// StartHostilePeer listens on loopback in front of target (host:port).
func StartHostilePeer(t testing.TB, target string) *HostilePeer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &HostilePeer{Addr: ln.Addr().String(), target: target, ln: ln}
	srv := &http.Server{Handler: http.HandlerFunc(p.serve)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return p
}

func (p *HostilePeer) serve(w http.ResponseWriter, r *http.Request) {
	down, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer down.Close()
	up, _, err := websocket.DefaultDialer.Dial("ws://"+p.target+"/wire", http.Header{"Origin": {"http://" + p.target}})
	if err != nil {
		return
	}
	defer up.Close()
	p.mu.Lock()
	p.upstream = append(p.upstream, up)
	p.mu.Unlock()
	pipe := func(from, to *websocket.Conn, lock *sync.Mutex) {
		for {
			_, b, err := from.ReadMessage()
			if err != nil {
				from.Close()
				to.Close()
				return
			}
			p.mu.Lock()
			p.Recorded = append(p.Recorded, b)
			p.mu.Unlock()
			if lock != nil {
				lock.Lock()
			}
			to.WriteMessage(websocket.TextMessage, b)
			if lock != nil {
				lock.Unlock()
			}
		}
	}
	go pipe(up, down, nil)
	pipe(down, up, &p.umu)
}

// Inject sends a frame to the listener behind it, as if an Engine had.
func (p *HostilePeer) Inject(frame []byte) {
	p.mu.Lock()
	ups := append([]*websocket.Conn{}, p.upstream...)
	p.mu.Unlock()
	p.umu.Lock()
	defer p.umu.Unlock()
	for _, u := range ups {
		u.WriteMessage(websocket.TextMessage, frame)
	}
}

// Envelopes returns every envelope it carried.
func (p *HostilePeer) Envelopes() []wire.Envelope {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []wire.Envelope
	for _, b := range p.Recorded {
		var f struct {
			Envelope *wire.Envelope `json:"envelope"`
		}
		if json.Unmarshal(b, &f) == nil && f.Envelope != nil {
			out = append(out, *f.Envelope)
		}
	}
	return out
}

// AllRecorded is every frame it carried, joined.
func (p *HostilePeer) AllRecorded() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []byte
	for _, b := range p.Recorded {
		out = append(out, b...)
	}
	return out
}
