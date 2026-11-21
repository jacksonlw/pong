package engine

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jacksonlw/pong/integrations"
)

// MCPBackend is what Pong's MCP tools need from an Engine. The Engine itself
// serves it over HTTP; the command-line tool forwards it to the local API.
// Errors carry the Engine's fix-it message; ErrEngineDown means no Engine.
type MCPBackend interface {
	CreatePing(NewPing) (Ping, error)
	WaitOnce(id string) (Ping, error) // returns after a short wait
	Withdraw(id string) (Ping, error)
}

// ErrEngineDown is returned by an MCPBackend that cannot reach the Engine.
var ErrEngineDown = errors.New("Engine is not running")

// MCP serves Pong's tools to one agent over MCP's JSON-RPC.
type MCP struct {
	Backend MCPBackend
	Session string // used when a tool call names no session
	Harness string // used when a tool call names no harness
	Folder  string // used when a tool call names no folder
	// Quiet serves an agent nobody attends: no instruction and no tools, so it
	// works as it would without Pong.
	Quiet bool
}

const mcpProtocol = "2025-06-18"

// codexHarness is the Codex adapter's name for its Harness, the one Harness
// that names its Session in a tool call's _meta.threadId.
const codexHarness = "codex"

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handle answers one JSON-RPC message, or returns nil for a notification.
func (m MCP) Handle(msg []byte) []byte {
	var req rpcRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		return rpcReply(json.RawMessage("null"), nil, &rpcError{-32700, "parse error: " + err.Error()})
	}
	if req.ID == nil {
		return nil // notification, such as notifications/initialized
	}
	var result any
	var rerr *rpcError
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if v == "" {
			v = mcpProtocol
		}
		init := map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "pong", "version": Version},
		}
		if !m.Quiet {
			init["instructions"] = integrations.StandingInstruction
		}
		result = init
	case "ping":
		result = map[string]any{}
	case "tools/list":
		if m.Quiet {
			result = map[string]any{"tools": []any{}}
		} else {
			result = map[string]any{"tools": mcpTools}
		}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      struct {
				ThreadID string `json:"threadId"`
			} `json:"_meta"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rerr = &rpcError{-32602, "params: " + err.Error()}
			break
		}
		if p.Meta.ThreadID != "" {
			// Codex runs every Session's tools in one shared process, so the
			// process cannot say which Session called; Codex names it on each
			// call, with the id its hooks report.
			m.Session, m.Harness = p.Meta.ThreadID, codexHarness
		}
		r, ok := m.call(p.Name, p.Arguments)
		if !ok {
			rerr = &rpcError{-32602, "unknown tool " + p.Name}
			break
		}
		result = r
	default:
		rerr = &rpcError{-32601, "method not found: " + req.Method}
	}
	return rpcReply(req.ID, result, rerr)
}

func rpcReply(id json.RawMessage, result any, e *rpcError) []byte {
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		out["error"] = e
	} else {
		out["result"] = result
	}
	b, _ := json.Marshal(out)
	return b
}

// ServeStdio reads newline-delimited JSON-RPC messages from r and writes the
// replies to w, until r ends.
func (m MCP) ServeStdio(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if out := m.Handle([]byte(line)); out != nil {
			if _, err := w.Write(append(out, '\n')); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

// SessionHeader carries the Session's identifier on MCP over HTTP. Without it
// the Engine uses the MCP session it handed out at initialize.
const SessionHeader = "Pong-Session"

// mountMCP serves MCP Streamable HTTP at /mcp, behind the same guard as the
// rest of the API. Replies are plain JSON; the server sends no requests.
func (s *pings) mountMCP(mux *http.ServeMux, hold time.Duration) {
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sid := r.Header.Get("Mcp-Session-Id")
		if strings.Contains(string(body), `"initialize"`) && sid == "" {
			b := make([]byte, 8)
			rand.Read(b)
			sid = "mcp-" + hex.EncodeToString(b)
			w.Header().Set("Mcp-Session-Id", sid)
		}
		session := r.Header.Get(SessionHeader)
		if session == "" {
			session = sid
		}
		m := MCP{Backend: engineBackend{s, hold, r.Context().Done()}, Session: session}
		out := m.Handle(body)
		if out == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "this MCP server sends no requests: POST messages to /mcp")
	})
}

// engineBackend serves the tools from inside the Engine.
type engineBackend struct {
	s    *pings
	hold time.Duration
	done <-chan struct{}
}

func (b engineBackend) CreatePing(n NewPing) (Ping, error) {
	if msg := n.validate(); msg != "" {
		return Ping{}, errors.New(msg)
	}
	return b.s.createWithResources(n)
}

func (b engineBackend) WaitOnce(id string) (Ping, error) {
	return fixIt(b.s.wait(id, b.hold, b.done, false))
}

func (b engineBackend) Withdraw(id string) (Ping, error) {
	return fixIt(b.s.end(id, StateWithdrawn, func(Ping) error { return nil }, nil))
}

func fixIt(p Ping, err error) (Ping, error) {
	if errors.Is(err, errNotFound) {
		return p, fmt.Errorf("no such Ping: check the ping_id")
	}
	return p, err
}
