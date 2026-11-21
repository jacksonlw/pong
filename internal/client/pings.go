package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jacksonlw/pong/internal/engine"
)

// NewPing is what a Session sends to create a Ping or Note.
type NewPing = engine.NewPing

// SendPong is the human's reply.
type SendPong = engine.SendPong

// CreatePing creates a Ping and returns it.
func (c *Client) CreatePing(p NewPing) (engine.Ping, error) {
	var out engine.Ping
	return out, c.do(c.HTTP, "POST", "/pings", p, &out)
}

// Ping reads one Ping.
func (c *Client) Ping(id string) (engine.Ping, error) {
	var out engine.Ping
	return out, c.do(c.HTTP, "GET", "/pings/"+url.PathEscape(id), nil, &out)
}

// Pings lists Pings in state, or all when state is empty.
func (c *Client) Pings(state string) ([]engine.Ping, error) {
	var out []engine.Ping
	return out, c.do(c.HTTP, "GET", "/pings?state="+url.QueryEscape(state), nil, &out)
}

// WaitOnce holds a request open until the Ping has a Pong or the Engine's
// hold runs out, and returns the Ping as it then stands.
func (c *Client) WaitOnce(id string) (engine.Ping, error) { return c.waitOnce(id, false) }

func (c *Client) waitOnce(id string, held bool) (engine.Ping, error) {
	var out engine.Ping
	hc := *c.HTTP
	hc.Timeout = 0
	q := ""
	if held {
		q = "?held=1"
	}
	return out, c.do(&hc, "GET", "/pings/"+url.PathEscape(id)+"/wait"+q, nil, &out)
}

// RestartWait is how long Wait keeps trying to reach an Engine that stopped
// answering, so a Pong sent before an Engine restart is delivered after it.
// It is well inside the Engine's DefaultDeliveryGrace.
var RestartWait = 20 * time.Second

// Wait repeats WaitOnce until the Ping is no longer waiting, holding the
// Ping's prompt meanwhile: the caller is a hook holding it (see the Engine's
// held). An Engine that stops answering is tried again for RestartWait.
func (c *Client) Wait(id string) (engine.Ping, error) {
	var lost time.Time
	for {
		p, err := c.waitOnce(id, true)
		var e *Error
		if err != nil && !errors.As(err, &e) {
			if lost.IsZero() {
				lost = time.Now()
			}
			if time.Since(lost) < RestartWait {
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}
		lost = time.Time{}
		if err != nil || p.State != engine.StateWaiting || p.IsNote {
			return p, err
		}
	}
}

// Pong sends a Pong to a Ping.
func (c *Client) Pong(id string, p SendPong) (engine.Ping, error) {
	var out engine.Ping
	return out, c.do(c.HTTP, "POST", "/pings/"+url.PathEscape(id)+"/pong", p, &out)
}

func (c *Client) end(id, action string) (engine.Ping, error) {
	var out engine.Ping
	return out, c.do(c.HTTP, "POST", "/pings/"+url.PathEscape(id)+"/"+action, nil, &out)
}

// SendAgain sends an undelivered or unconfirmed Pong again: into its own
// Session, or as a new Session continued from it.
func (c *Client) SendAgain(id string, newSession bool) (engine.Ping, error) {
	var out engine.Ping
	hc := *c.HTTP
	hc.Timeout = time.Minute // starting a Session runs its Harness's checks
	return out, c.do(&hc, "POST", "/pings/"+url.PathEscape(id)+"/send-again", engine.SendAgain{NewSession: newSession}, &out)
}

// Leave keeps an undelivered or unconfirmed Pong without sending it again.
func (c *Client) Leave(id string) (engine.Ping, error) { return c.end(id, "leave") }

// HandBack hands a waiting Ping back to its Harness.
func (c *Client) HandBack(id string) (engine.Ping, error) { return c.end(id, "hand-back") }

// Withdraw withdraws a waiting Ping on behalf of its agent.
func (c *Client) Withdraw(id string) (engine.Ping, error) { return c.end(id, "withdraw") }

// Dismiss dismisses a Note.
func (c *Client) Dismiss(id string) (engine.Ping, error) { return c.end(id, "dismiss") }

// AnsweredInHarness records that the human answered the Ping in its Harness.
func (c *Client) AnsweredInHarness(id string) (engine.Ping, error) {
	return c.end(id, "answered-in-harness")
}

// Error is an error answer from the Engine.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("engine: %s (%d)", e.Message, e.Status) }

// Do sends in as JSON to the Engine's local API and decodes the answer into out.
func (c *Client) Do(method, path string, in, out any) error {
	return c.do(c.HTTP, method, path, in, out)
}

func (c *Client) do(hc *http.Client, method, path string, in, out any) error {
	req, err := c.Request(method, path)
	if err != nil {
		return err
	}
	if raw, ok := in.(rawBody); ok {
		req.Body = io.NopCloser(raw.reader())
	} else if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		return &Error{Status: resp.StatusCode, Message: e.Error}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Resource reads a Resource's content and its content type.
func (c *Client) Resource(pingID, resourceID string) ([]byte, string, error) {
	req, err := c.Request("GET", "/pings/"+url.PathEscape(pingID)+"/resources/"+url.PathEscape(resourceID))
	if err != nil {
		return nil, "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&e)
		return nil, "", &Error{Status: resp.StatusCode, Message: e.Error}
	}
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header.Get("Content-Type"), err
}

// Draft shares a draft Pong for Ping id with every Device.
func (c *Client) Draft(id string, draft any) (engine.Ping, error) {
	var p engine.Ping
	return p, c.do(c.HTTP, http.MethodPut, "/pings/"+id+"/draft", draft, &p)
}

// WithdrawPong takes back a Pong not yet delivered; the Ping waits again.
func (c *Client) WithdrawPong(id string) (engine.Ping, error) { return c.end(id, "withdraw-pong") }

// AskAllowed asks the Engine about a tool call the human may have allowed in
// a Pong: q.Do is "check", "take" or "refused" (engine.CallQuery).
func (c *Client) AskAllowed(q engine.CallQuery) (bool, error) {
	var out struct {
		Allowed bool `json:"allowed"`
	}
	return out.Allowed, c.do(c.HTTP, "POST", "/allowed", q, &out)
}
