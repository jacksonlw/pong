package wire

import (
	"encoding/json"
	"fmt"
)

// A Message is what one envelope carries. Its JSON form is an object with a
// "kind" field and the kind's own fields beside it.
type Message interface{ Kind() string }

// MaxResourcePart is the most bytes one resource message carries.
const MaxResourcePart = 192 * 1024

// Hello is sent on connecting.
type Hello struct {
	ChainLen int `json:"chain_len"`
	// Head is the hash of the sender's last record: two chains of one length
	// that differ are a fork, and are exchanged.
	Head []byte `json:"head,omitempty"`
	Want string `json:"want,omitempty"` // "snapshot" or empty
	// Bind is the nonce the sender greeted with on this connection, then the
	// one it was greeted with. A connection becomes the sender's only on a
	// hello that binds it, so an envelope taken off another connection
	// cannot make it the sender's.
	Bind []byte `json:"bind,omitempty"`
}

// Ack says which envelopes from the recipient were opened. A message waits in
// the sender's outbox until it is acknowledged, and is sent again until then.
type Ack struct {
	IDs []string `json:"ids"`
}

// ChainRecords carries records the recipient lacks.
type ChainRecords struct {
	Records Chain `json:"records"`
}

// Ping is a Ping as the local API describes it, sent on creation and on
// every change of state, with its computer, Project and Session.
type Ping struct {
	Computer string          `json:"computer"`        // Device identifier
	Event    string          `json:"event,omitempty"` // the local API's event kind, e.g. "ping.created"
	Ping     json.RawMessage `json:"ping"`
	Project  json.RawMessage `json:"project,omitempty"`
	Session  json.RawMessage `json:"session,omitempty"`
}

// Snapshot is every waiting Ping a computer holds, with recent Thread entries
// and the facts of its Sessions: those with a waiting Ping and the latest.
type Snapshot struct {
	Computer string            `json:"computer"`
	Pings    []Ping            `json:"pings"`
	Threads  json.RawMessage   `json:"threads,omitempty"`
	Sessions []json.RawMessage `json:"sessions,omitempty"`
}

// Session is one Session's facts as the local API describes a Session, sent
// by the computer running it when they change.
type Session struct {
	Computer string          `json:"computer"` // Device identifier
	Session  json.RawMessage `json:"session"`
}

// Pong is the human's answer to a Ping, as the local API takes it.
type Pong struct {
	PingID string          `json:"ping_id"`
	PongID string          `json:"pong_id,omitempty"` // chosen by the sender; the delivery answering it carries it
	Pong   json.RawMessage `json:"pong"`
	// Again, when set, acts on the Ping's undelivered Pong instead:
	// "same_session" or "new_session" sends it again, "leave" keeps it unsent.
	Again string `json:"again,omitempty"`
}

// Draft is a draft Pong in progress.
type Draft struct {
	PingID string          `json:"ping_id"`
	Draft  json.RawMessage `json:"draft"`
}

// Withdraw takes back a Pong still waiting to be delivered.
type Withdraw struct {
	PingID string `json:"ping_id"`
	PongID string `json:"pong_id,omitempty"`
}

// Dismiss dismisses a Ping.
type Dismiss struct {
	PingID string `json:"ping_id"`
}

// Delivery states.
const (
	Sent        = "sent"
	Delivered   = "delivered"
	Undelivered = "undelivered"
)

// Delivery is a Pong's state and, when it lost, the Pong that won.
type Delivery struct {
	PingID string          `json:"ping_id"`
	PongID string          `json:"pong_id,omitempty"`
	State  string          `json:"state"`
	Winner json.RawMessage `json:"winner,omitempty"`
	Reason string          `json:"reason,omitempty"` // why it was not applied, in words a human reads
}

// ResourceGet asks for a Resource's bytes from Offset.
type ResourceGet struct {
	PingID     string `json:"ping_id"`
	ResourceID string `json:"resource_id"`
	Offset     int64  `json:"offset"`
}

// Resource is one part of a Resource, at most MaxResourcePart bytes.
type Resource struct {
	PingID     string `json:"ping_id"`
	ResourceID string `json:"resource_id"`
	Offset     int64  `json:"offset"`
	Total      int64  `json:"total"`
	MIME       string `json:"mime,omitempty"`
	Title      string `json:"title,omitempty"`
	ResKind    string `json:"resource_kind,omitempty"` // the Resource's kind, as the local API names it
	SHA256     []byte `json:"sha256,omitempty"`        // of the whole Resource, to check it once reassembled
	Data       []byte `json:"data"`
	Error      string `json:"error,omitempty"`
}

// ThreadGet asks for a Session's Thread.
type ThreadGet struct {
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
}

// Thread is a Session's Thread as the local API returns it.
type Thread struct {
	Harness   string          `json:"harness"`
	SessionID string          `json:"session_id"`
	Thread    json.RawMessage `json:"thread"`
	Error     string          `json:"error,omitempty"`
}

// Start asks a computer to start a Session.
type Start struct {
	RequestID string          `json:"request_id"`
	Start     json.RawMessage `json:"start"` // the local API's start body
}

// FollowUp sends a follow-up prompt to a Session.
type FollowUp struct {
	RequestID string          `json:"request_id"`
	Harness   string          `json:"harness"`
	SessionID string          `json:"session_id"`
	FollowUp  json.RawMessage `json:"follow_up"` // the local API's follow-up body
}

// Stop asks the computer that runs a Session to stop the turn it is in. It
// is answered with a StartResult whose Session carries the stop.
type Stop struct {
	RequestID string `json:"request_id"`
	Harness   string `json:"harness"`
	SessionID string `json:"session_id"`
}

// StartResult answers Start, FollowUp and Stop. A Session a Start opened in its
// Harness has no id yet: it registers itself once it starts.
type StartResult struct {
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Session   json.RawMessage `json:"session,omitempty"`
	Reasons   []string        `json:"reasons,omitempty"` // why the computer refused
}

// State is a computer's state report.
type State struct {
	State json.RawMessage `json:"state"`
}

// Project is a Project's metadata, copied to every Device; the last edit wins.
type Project struct {
	Project json.RawMessage `json:"project"`
}

// CloudGrant carries the Pong Cloud registration grant.
type CloudGrant struct {
	Grant   string `json:"grant"`
	Expires int64  `json:"expires"`          // Unix milliseconds
	Server  string `json:"server,omitempty"` // Pong's server, e.g. https://pong.fly
}

func (Hello) Kind() string        { return "hello" }
func (ChainRecords) Kind() string { return "chain" }
func (Ping) Kind() string         { return "ping" }
func (Snapshot) Kind() string     { return "snapshot" }
func (Session) Kind() string      { return "session" }
func (Pong) Kind() string         { return "pong" }
func (Draft) Kind() string        { return "draft" }
func (Withdraw) Kind() string     { return "withdraw" }
func (Dismiss) Kind() string      { return "dismiss" }
func (Delivery) Kind() string     { return "delivery" }
func (ResourceGet) Kind() string  { return "resource_get" }
func (Resource) Kind() string     { return "resource" }
func (ThreadGet) Kind() string    { return "thread_get" }
func (Thread) Kind() string       { return "thread" }
func (Start) Kind() string        { return "start" }
func (FollowUp) Kind() string     { return "follow_up" }
func (Stop) Kind() string         { return "stop" }
func (StartResult) Kind() string  { return "start_result" }
func (State) Kind() string        { return "state" }
func (Project) Kind() string      { return "project" }
func (CloudGrant) Kind() string   { return "cloud_grant" }
func (Ack) Kind() string          { return "ack" }

var kinds = map[string]func() Message{}

func init() {
	for _, f := range []func() Message{
		func() Message { return &Hello{} }, func() Message { return &ChainRecords{} }, func() Message { return &Ack{} },
		func() Message { return &Ping{} }, func() Message { return &Snapshot{} },
		func() Message { return &Pong{} }, func() Message { return &Draft{} },
		func() Message { return &Withdraw{} }, func() Message { return &Dismiss{} },
		func() Message { return &Delivery{} }, func() Message { return &ResourceGet{} },
		func() Message { return &Resource{} }, func() Message { return &ThreadGet{} },
		func() Message { return &Thread{} }, func() Message { return &Start{} },
		func() Message { return &FollowUp{} }, func() Message { return &Stop{} },
		func() Message { return &StartResult{} }, func() Message { return &State{} },
		func() Message { return &Project{} }, func() Message { return &CloudGrant{} },
		func() Message { return &Session{} },
	} {
		kinds[f().Kind()] = f
	}
}

// Kinds lists every message kind.
func Kinds() []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	return out
}

// Encode writes m as {"kind": ..., fields...}.
func Encode(m Message) ([]byte, error) {
	var data []byte
	switch r := m.(type) {
	case *Resource:
		data = r.Data
	case Resource:
		data = r.Data
	}
	if len(data) > MaxResourcePart {
		return nil, fmt.Errorf("wire: resource part of %d bytes is over %d", len(data), MaxResourcePart)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	kind, _ := json.Marshal(m.Kind())
	if string(b) == "{}" {
		return []byte(`{"kind":` + string(kind) + `}`), nil
	}
	return append([]byte(`{"kind":`+string(kind)+`,`), b[1:]...), nil
}

// Decode reads a message written by Encode. The value is a pointer to the
// kind's type, e.g. *Pong.
func Decode(b []byte) (Message, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, fmt.Errorf("wire: message: %w", err)
	}
	f, ok := kinds[head.Kind]
	if !ok {
		return nil, fmt.Errorf("wire: unknown message kind %q", head.Kind)
	}
	m := f()
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("wire: %s message: %w", head.Kind, err)
	}
	return m, nil
}
