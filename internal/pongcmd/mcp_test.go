package pongcmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
)

// mcpConn sends one JSON-RPC request and returns the reply's result.
type mcpConn func(t *testing.T, method string, params any) map[string]any

var nextID = 0

func request(method string, params any) []byte {
	nextID++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nextID, "method": method, "params": params})
	return b
}

func result(t *testing.T, reply []byte) map[string]any {
	t.Helper()
	var r struct {
		Result map[string]any
		Error  any
	}
	if err := json.Unmarshal(reply, &r); err != nil || r.Error != nil {
		t.Fatalf("reply %s: %v", reply, err)
	}
	return r.Result
}

// overHTTP talks to the Engine's /mcp endpoint.
func overHTTP(c *client.Client, session string) mcpConn {
	return func(t *testing.T, method string, params any) map[string]any {
		t.Helper()
		req, _ := c.Request("POST", "/mcp")
		req.Body = io.NopCloser(bytes.NewReader(request(method, params)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(engine.SessionHeader, session)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return result(t, b)
	}
}

// overStdio runs `pong mcp` and talks to it over pipes.
func overStdio(t *testing.T, dir, session string) mcpConn {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		serveMCP([]string{"-dir", dir, "-session", session, "-folder", "/tmp/project"}, inR, outW, io.Discard)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	lines := bufio.NewReader(outR)
	return func(t *testing.T, method string, params any) map[string]any {
		t.Helper()
		inW.Write(append(request(method, params), '\n'))
		line, err := lines.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		return result(t, line)
	}
}

func call(t *testing.T, conn mcpConn, tool string, args any) (text string, structured map[string]any, isError bool) {
	t.Helper()
	r := conn(t, "tools/call", map[string]any{"name": tool, "arguments": args})
	text = r["content"].([]any)[0].(map[string]any)["text"].(string)
	structured, _ = r["structuredContent"].(map[string]any)
	isError, _ = r["isError"].(bool)
	return
}

func transports(t *testing.T, f func(t *testing.T, c *client.Client, conn mcpConn)) {
	t.Run("http", func(t *testing.T) {
		c, _ := enginetest.Start(t)
		f(t, c, overHTTP(c, "s1"))
	})
	t.Run("stdio", func(t *testing.T) {
		c, dir := enginetest.Start(t)
		f(t, c, overStdio(t, dir, "s1"))
	})
}

func TestMCPListsItsTools(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		init := conn(t, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
		if !strings.Contains(init["instructions"].(string), "send_ping") {
			t.Fatalf("initialize = %v", init)
		}
		var names []string
		for _, tool := range conn(t, "tools/list", nil)["tools"].([]any) {
			names = append(names, tool.(map[string]any)["name"].(string))
		}
		if strings.Join(names, ",") != "send_ping,wait_for_pong,send_note,send_thought,withdraw_ping" {
			t.Fatalf("tools = %v", names)
		}
	})
}

func TestMCPSendWaitPong(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		_, s, isErr := call(t, conn, "send_ping", map[string]any{
			"folder": "/tmp/project", "question": "Ship the fix to staging first?", "blocks_agent": true,
			"answers":     []map[string]any{{"label": "Straight to production"}, {"label": "Staging first", "leads_to": "a day's delay"}},
			"recommended": "Staging first",
		})
		if isErr || s["state"] != "waiting" || s["ping_id"] == "" {
			t.Fatalf("send = %v", s)
		}
		id := s["ping_id"].(string)
		p, _ := c.Ping(id)
		if p.Session != "s1" || p.Folder != "/tmp/project" || p.Answers[0].Label != "Staging first" {
			t.Fatalf("Ping = %+v", p)
		}
		text, s, _ := call(t, conn, "wait_for_pong", map[string]any{"ping_id": id})
		if s["state"] != "waiting" || !strings.Contains(text, "wait_for_pong") {
			t.Fatalf("wait = %v %q", s, text)
		}
		go func() {
			time.Sleep(enginetest.WaitHold / 5)
			c.Pong(id, client.SendPong{Answer: "Staging first", Text: "after lunch"})
		}()
		text, s, _ = call(t, conn, "wait_for_pong", map[string]any{"ping_id": id})
		if s["state"] != "answered" || !strings.Contains(text, "Staging first") || !strings.Contains(text, "after lunch") {
			t.Fatalf("wait = %v %q", s, text)
		}
		// Delivered when wait_for_pong returned it.
		if p, _ := c.Ping(id); p.Pong.Delivery != engine.DeliveryDelivered {
			t.Errorf("delivery = %q", p.Pong.Delivery)
		}
	})
}

func TestMCPSendReturnsPongArrivingWithinShortWait(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		go func() {
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if ps, _ := c.Pings(engine.StateWaiting); len(ps) == 1 {
					c.Pong(ps[0].ID, client.SendPong{Text: "go ahead"})
					return
				}
			}
		}()
		text, s, _ := call(t, conn, "send_ping", map[string]any{"question": "Delete the stale branches?", "approves": "action", "blocks_agent": true})
		if s["state"] != "answered" || !strings.Contains(text, "go ahead") {
			t.Fatalf("send = %v %q", s, text)
		}
	})
}

func TestMCPMalformedPingReturnsFixIt(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		text, _, isErr := call(t, conn, "send_ping", map[string]any{
			"question": "Use Postgres or SQLite?", "answers": []map[string]any{{"label": "Postgres"}, {"label": "SQLite"}},
			"captured": true, // an agent cannot opt out of the strict rules
		})
		if !isErr || !strings.Contains(text, "ask the question without listing its answers") {
			t.Fatalf("send = %v %q", isErr, text)
		}
		if ps, _ := c.Pings(""); len(ps) != 0 {
			t.Fatalf("malformed Ping was stored: %v", ps)
		}
	})
}

func TestMCPNoteAndWithdraw(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		_, s, isErr := call(t, conn, "send_note", map[string]any{"text": "Build passed on main."})
		if isErr || s["state"] != "waiting" {
			t.Fatalf("note = %v", s)
		}
		if n, _ := c.Ping(s["ping_id"].(string)); !n.IsNote || n.Question != "Build passed on main." {
			t.Fatalf("Note = %+v", n)
		}
		_, s, _ = call(t, conn, "send_ping", map[string]any{"question": "Which port should the dev server use?", "blocks_agent": true})
		id := s["ping_id"].(string)
		_, s, isErr = call(t, conn, "withdraw_ping", map[string]any{"ping_id": id})
		if isErr || s["state"] != "withdrawn" {
			t.Fatalf("withdraw = %v", s)
		}
		text, s, _ := call(t, conn, "wait_for_pong", map[string]any{"ping_id": id})
		if s["state"] != "withdrawn" || !strings.Contains(text, "no Pong") {
			t.Fatalf("wait after withdraw = %v %q", s, text)
		}
		text, _, isErr = call(t, conn, "wait_for_pong", map[string]any{"ping_id": "nope"})
		if !isErr || !strings.Contains(text, "no such Ping") {
			t.Fatalf("wait unknown = %v %q", isErr, text)
		}
	})
}

func TestMCPWithoutEngineTellsAgentToAskAsUsual(t *testing.T) {
	conn := overStdio(t, t.TempDir(), "s1")
	for tool, args := range map[string]any{
		"send_ping":     map[string]any{"question": "Ship it?"},
		"send_note":     map[string]any{"text": "Done."},
		"wait_for_pong": map[string]any{"ping_id": "x"},
		"withdraw_ping": map[string]any{"ping_id": "x"},
	} {
		start := time.Now()
		text, _, _ := call(t, conn, tool, args)
		if !strings.Contains(text, "ask your human in your usual way") && !strings.Contains(text, "Ask your human in your usual way") {
			t.Fatalf("%s = %q", tool, text)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("%s took %v", tool, time.Since(start))
		}
	}
}

// send_thought keeps a Thought and tells the agent to carry on; a malformed
// one comes back with what to fix.
func TestMCPSendThought(t *testing.T) {
	transports(t, func(t *testing.T, c *client.Client, conn mcpConn) {
		for _, tc := range []struct {
			name    string
			args    map[string]any
			text    string
			isError bool
		}{
			{"kept", map[string]any{"text": "Inbox drops the Session id on retry.", "summary": "Fixing the retry."}, "Thought kept. Carry on.", false},
			{"too long", map[string]any{"text": strings.Repeat("a", 141)}, "at most 140", true},
			{"empty", map[string]any{"text": " "}, "needs its sentence", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				text, s, isErr := call(t, conn, "send_thought", tc.args)
				if isErr != tc.isError || !strings.Contains(text, tc.text) {
					t.Fatalf("%q %v", text, isErr)
				}
				if tc.isError {
					return
				}
				p, _ := c.Ping(s["ping_id"].(string))
				if !p.Thought || p.State != engine.StateDismissed || p.Question != tc.args["text"] || p.Session != "s1" {
					t.Fatalf("kept %+v", p)
				}
				if sess, _ := c.Session("", "s1"); sess.Summary != "Fixing the retry." {
					t.Fatalf("summary %q", sess.Summary)
				}
			})
		}
	})
}

// When Pong cannot take a Thought, the agent carries on: nothing was being
// asked, so it is never told to ask in its usual way.
func TestMCPThoughtPongCannotTakeSaysCarryOn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		conn  func(t *testing.T) mcpConn
		state string
	}{
		{"no Engine", func(t *testing.T) mcpConn { return overStdio(t, t.TempDir(), "s1") }, "engine_not_running"},
		{"headless run", func(t *testing.T) mcpConn {
			was := attended.Harness
			attended.Harness = func() []string { return []string{"claude", "-p"} }
			t.Cleanup(func() { attended.Harness = was })
			_, dir := enginetest.Start(t)
			return overStdio(t, dir, "s1")
		}, "dropped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, s, isErr := call(t, tc.conn(t), "send_thought", map[string]any{"text": "Inbox drops the Session id on retry."})
			if isErr || !strings.HasSuffix(text, "Carry on.") || strings.Contains(strings.ToLower(text), "usual way") || s["state"] != tc.state {
				t.Fatalf("%q %v %v", text, s, isErr)
			}
		})
	}
}
