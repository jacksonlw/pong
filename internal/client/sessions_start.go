package client

import (
	"net/http"
	"net/url"

	"github.com/jacksonlw/pong/internal/engine"
)

// StartSession starts a Session through the Engine.
func (c *Client) StartSession(s engine.StartSession) (engine.Session, error) {
	var out engine.Session
	return out, c.do(c.HTTP, "POST", "/sessions/start", s, &out)
}

// FollowUp sends text the human wrote to a Session.
func (c *Client) FollowUp(harness, id, prompt string) (engine.Session, error) {
	var out engine.Session
	return out, c.do(c.HTTP, "POST", "/session/follow-up?"+sessionQuery(harness, id), engine.FollowUp{Prompt: prompt}, &out)
}

// Stop asks for the turn a working Session is in to be stopped.
func (c *Client) Stop(harness, id string) (engine.Stop, error) {
	var out engine.Stop
	return out, c.do(c.HTTP, "POST", "/session/stop?"+sessionQuery(harness, id), nil, &out)
}

// TakeStop is for a Harness's hook before a tool call: it reports whether a
// stop was sent for the Session's turn, which the hook then ends.
func (c *Client) TakeStop(harness, id string) (bool, error) {
	var out engine.TakenStop
	return out.Stopped, c.do(c.HTTP, "POST", "/session/take-stop?"+sessionQuery(harness, id), nil, &out)
}

func sessionQuery(harness, id string) string {
	return "harness=" + url.QueryEscape(harness) + "&id=" + url.QueryEscape(id)
}

// Placement says which computer a new Session would go to, and why each can or cannot take it.
func (c *Client) Placement(project, harness string) (engine.Placement, error) {
	var p engine.Placement
	return p, c.do(c.HTTP, http.MethodGet, "/sessions/placement?project="+url.QueryEscape(project)+"&harness="+url.QueryEscape(harness), nil, &p)
}

// Computers returns every computer in the set with its state.
func (c *Client) Computers() ([]engine.ComputerState, error) {
	var out []engine.ComputerState
	return out, c.do(c.HTTP, http.MethodGet, "/computers", nil, &out)
}

// TakeText takes the text the human wrote in the Session's Thread that it
// has not had yet, as its hooks do.
func (c *Client) TakeText(harness, id string) ([]string, error) {
	var out engine.TakenText
	return out.Texts, c.do(c.HTTP, "POST", "/session/take-text?"+sessionQuery(harness, id), nil, &out)
}

// ListenText waits for text for an idle Session the human started, and
// returns it. It returns nothing once the Session is no longer idle, or the
// Engine cannot be reached.
func (c *Client) ListenText(harness, id string) ([]string, error) {
	hc := *c.HTTP
	hc.Timeout = 0
	for {
		var out engine.TakenText
		if err := c.do(&hc, "POST", "/session/take-text?listen=1&"+sessionQuery(harness, id), nil, &out); err != nil || !out.Listening {
			return out.Texts, err
		}
	}
}

// SetSurface sets where the human works in a Harness: Pong opens Sessions there.
func (c *Client) SetSurface(harness, surface string) error {
	var out map[string]string
	return c.do(c.HTTP, "PUT", "/harnesses/"+url.PathEscape(harness)+"/surface", map[string]string{"surface": surface}, &out)
}
