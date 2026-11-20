package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// fetchWait is how long one request for a part of a Resource, or a Thread,
// waits for another computer's answer; it is asked fetchTries times, since a
// connection replaced while the answer was on its way loses it.
const (
	fetchWait  = 3 * time.Second
	fetchTries = 3
)

// fetched is a Resource as another computer sent it.
type fetched struct {
	data              []byte
	mime, title, kind string
}

// partKey names one requested part of a Resource.
func partKey(pingID, rid string, offset int64) string {
	return pingID + "/" + rid + "@" + strconv.FormatInt(offset, 10)
}

// fetchResource asks the computer holding Ping id for one of its Resources,
// part by part, when the human opens it; never ahead of time.
func (a *across) fetchResource(id, rid string) (fetched, error) {
	a.mu.Lock()
	owner := a.owner[id]
	a.mu.Unlock()
	d, _, _ := a.node.Member(owner)
	var out fetched
	var sum []byte
	for offset := int64(0); ; {
		key := partKey(id, rid, offset)
		ch := make(chan wire.Resource, 1)
		a.mu.Lock()
		a.parts[key] = append(a.parts[key], ch)
		a.mu.Unlock()
		var part wire.Resource
		var ok bool
		for try := 0; try < fetchTries && !ok; try++ {
			if !a.node.Send(owner, &wire.ResourceGet{PingID: id, ResourceID: rid, Offset: offset}) {
				break
			}
			select {
			case part = <-ch:
				ok = true
			case <-time.After(fetchWait):
			}
		}
		a.mu.Lock()
		a.parts[key] = without(a.parts[key], ch)
		if len(a.parts[key]) == 0 {
			delete(a.parts, key)
		}
		a.mu.Unlock()
		if !ok {
			return out, errUnreachable{d.Name}
		}
		if part.Error != "" {
			return out, errResource{part.Error}
		}
		if offset == 0 {
			out.mime, out.title, out.kind, sum = part.MIME, part.Title, part.ResKind, part.SHA256
		}
		out.data = append(out.data, part.Data...)
		offset += int64(len(part.Data))
		if offset >= part.Total || len(part.Data) == 0 {
			if offset != part.Total {
				return out, errResource{"the Resource arrived incomplete"}
			}
			break
		}
	}
	if h := sha256.Sum256(out.data); !bytes.Equal(h[:], sum) {
		return out, errResource{"the Resource arrived damaged"}
	}
	return out, nil
}

// partSize is how much of a Resource one message carries: under the wire's
// 192 KiB, so that once base64-encoded and sealed it fits the relay's
// 256 KiB limit on an envelope's body.
const partSize = 144 * 1024

// errResource is a Resource another computer could not send whole.
type errResource struct{ msg string }

func (e errResource) Error() string { return e.msg }

func (a *across) onResource(from string, m wire.Message) {
	r := m.(*wire.Resource)
	a.mu.Lock()
	chs := append([]chan wire.Resource{}, a.parts[partKey(r.PingID, r.ResourceID, r.Offset)]...)
	owner := a.owner[r.PingID]
	a.mu.Unlock()
	if owner != from {
		return
	}
	for _, ch := range chs {
		select {
		case ch <- *r:
		default:
		}
	}
}

// without returns chs without ch.
func without[T any](chs []chan T, ch chan T) []chan T {
	out := chs[:0:0]
	for _, c := range chs {
		if c != ch {
			out = append(out, c)
		}
	}
	return out
}

// onResourceGet sends one part of one of this Engine's Resources, off the
// connection's read loop so other messages from that Device are not held up.
func (a *across) onResourceGet(from string, m wire.Message) {
	go a.sendPart(from, m.(*wire.ResourceGet))
}

func (a *across) sendPart(from string, g *wire.ResourceGet) {
	reply := wire.Resource{PingID: g.PingID, ResourceID: g.ResourceID, Offset: g.Offset}
	p, err := a.s.get(g.PingID)
	var res *Resource
	for i := range p.Resources {
		if p.Resources[i].ID == g.ResourceID {
			res = &p.Resources[i]
		}
	}
	var f *os.File
	switch {
	case err != nil:
		reply.Error = "this Ping is no longer kept"
	case res == nil:
		reply.Error = "this Ping has no such Resource"
	default:
		if f, err = os.Open(filepath.Join(a.s.resourceDir(), res.ID)); err != nil {
			reply.Error = "the Resource could not be read"
		}
	}
	if f != nil {
		defer f.Close()
		fi, _ := f.Stat()
		reply.Total = fi.Size()
		if g.Offset == 0 {
			h := sha256.New()
			io.Copy(h, f)
			reply.SHA256, reply.MIME, reply.Title, reply.ResKind = h.Sum(nil), res.ContentType, res.Title, res.Kind
		}
		if g.Offset < 0 || g.Offset > reply.Total {
			reply.Error = "no such part"
		} else {
			buf := make([]byte, min(int64(partSize), reply.Total-g.Offset))
			n, _ := f.ReadAt(buf, g.Offset)
			reply.Data = buf[:n]
		}
	}
	a.node.Send(from, &reply)
}

// fetchThread asks the computer running a Session for its Thread.
func (a *across) fetchThread(harness, id string) (json.RawMessage, error) {
	owner := a.sessionOwner(SessionRef{harness, id})
	if owner == "" {
		return nil, errNotFound
	}
	d, _, _ := a.node.Member(owner)
	key := owner + "/" + harness + "/" + id
	ch := make(chan wire.Thread, 1)
	a.mu.Lock()
	a.threads[key] = append(a.threads[key], ch)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.threads[key] = without(a.threads[key], ch)
		if len(a.threads[key]) == 0 {
			delete(a.threads, key)
		}
		a.mu.Unlock()
	}()
	for try := 0; try < fetchTries; try++ {
		if !a.node.Send(owner, &wire.ThreadGet{Harness: harness, SessionID: id}) {
			return nil, errUnreachable{d.Name}
		}
		select {
		case t := <-ch:
			if t.Error != "" {
				return nil, errors.New(t.Error)
			}
			if len(t.Thread) == 0 || t.Thread[0] != '{' {
				return nil, errors.New("the Thread arrived unreadable")
			}
			return t.Thread, nil
		case <-time.After(fetchWait):
		}
	}
	return nil, errUnreachable{d.Name}
}

func (a *across) onThread(from string, m wire.Message) {
	t := m.(*wire.Thread)
	a.mu.Lock()
	chs := append([]chan wire.Thread{}, a.threads[from+"/"+t.Harness+"/"+t.SessionID]...) // only from the computer asked
	a.mu.Unlock()
	for _, ch := range chs {
		select {
		case ch <- *t:
		default:
		}
	}
}

func (a *across) onThreadGet(from string, m wire.Message) {
	g := m.(*wire.ThreadGet)
	reply := wire.Thread{Harness: g.Harness, SessionID: g.SessionID}
	if a.thread == nil {
		reply.Error = "this computer keeps no Threads"
	} else if t, err := a.thread(g.Harness, g.SessionID); err != nil {
		reply.Error = "no such Session on this computer"
	} else {
		// Whether text written in the Thread reaches the Session is this
		// computer's to say: the Device asking may send it a follow-up.
		t.TakesText = a.pr != nil && a.pr.takesText != nil && a.pr.takesText(t.Session)
		t.StopsTurns = a.pr != nil && a.pr.stopsTurns != nil && a.pr.stopsTurns(t.Session)
		reply.Thread, _ = json.Marshal(t)
	}
	a.node.Send(from, &reply)
}
