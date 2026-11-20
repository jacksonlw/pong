package client

import (
	"bytes"
	"io"
	"net/http"
	"net/url"

	"github.com/jacksonlw/pong/internal/engine"
)

// Projects lists the Projects the Engine has recognised.
func (c *Client) Projects() ([]engine.Project, error) {
	var out []engine.Project
	return out, c.do(c.HTTP, "GET", "/projects", nil, &out)
}

// Project reads one Project.
func (c *Client) Project(id string) (engine.Project, error) {
	var out engine.Project
	return out, c.do(c.HTTP, "GET", "/projects/"+url.PathEscape(id), nil, &out)
}

// ChangeProject changes a Project's name or colour.
func (c *Client) ChangeProject(id string, ch engine.ChangeProject) (engine.Project, error) {
	var out engine.Project
	return out, c.do(c.HTTP, "PATCH", "/projects/"+url.PathEscape(id), ch, &out)
}

// SetLogo replaces a Project's logo with a PNG or JPEG image.
func (c *Client) SetLogo(id string, image []byte) (engine.Project, error) {
	var out engine.Project
	return out, c.do(c.HTTP, "PUT", "/projects/"+url.PathEscape(id)+"/logo", rawBody(image), &out)
}

// Logo fetches the image at a Project's logo path.
func (c *Client) Logo(path string) ([]byte, error) {
	req, err := c.Request("GET", path)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &Error{Status: resp.StatusCode, Message: "no logo"}
	}
	return io.ReadAll(resp.Body)
}

// RegisterSession registers a Session or changes one already known.
func (c *Client) RegisterSession(s engine.RegisterSession) (engine.Session, error) {
	var out engine.Session
	return out, c.do(c.HTTP, "PUT", "/sessions", s, &out)
}

// Session reads one Session.
func (c *Client) Session(harness, id string) (engine.Session, error) {
	var out engine.Session
	return out, c.do(c.HTTP, "GET", "/session?harness="+url.QueryEscape(harness)+"&id="+url.QueryEscape(id), nil, &out)
}

// Thread reads a Session's Thread.
func (c *Client) Thread(harness, id string) (engine.Thread, error) {
	var out engine.Thread
	return out, c.do(c.HTTP, "GET", "/thread?harness="+url.QueryEscape(harness)+"&id="+url.QueryEscape(id), nil, &out)
}

// rawBody is a request body sent as it is, not as JSON.
type rawBody []byte

func (b rawBody) reader() io.Reader { return bytes.NewReader(b) }

// Sessions reads every Session.
func (c *Client) Sessions() ([]engine.Session, error) {
	var out []engine.Session
	return out, c.do(c.HTTP, "GET", "/sessions", nil, &out)
}

// LinkProject makes Project id the same Project as to, which may be on another computer.
func (c *Client) LinkProject(id, to string) (engine.Project, error) {
	var p engine.Project
	return p, c.do(c.HTTP, http.MethodPost, "/projects/"+id+"/link", map[string]string{"to": to}, &p)
}

// ProjectFor returns the Project for a folder on this computer, recognising it if new.
func (c *Client) ProjectFor(folder string) (engine.Project, error) {
	var p engine.Project
	return p, c.do(c.HTTP, http.MethodGet, "/projects?folder="+url.QueryEscape(folder), nil, &p)
}
