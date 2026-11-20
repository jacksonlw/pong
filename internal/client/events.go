package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jacksonlw/pong/internal/engine"
)

// Events opens the event stream and sends each event on the returned channel
// until stop is called or the stream ends, then closes it. since is the seq
// of the last event already seen (0 for all); a negative since means only
// events from now on.
func (c *Client) Events(since int64) (<-chan engine.Event, func(), error) {
	path := "/events"
	if since >= 0 {
		path += fmt.Sprintf("?since=%d", since)
	}
	req, err := c.Request("GET", path)
	if err != nil {
		return nil, nil, err
	}
	hc := *c.HTTP
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, nil, &Error{Status: resp.StatusCode, Message: "event stream refused"}
	}
	out := make(chan engine.Event)
	done := make(chan struct{})
	go func() {
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var e engine.Event
			if json.Unmarshal([]byte(data), &e) != nil {
				continue
			}
			select {
			case out <- e:
			case <-done:
				return
			}
		}
	}()
	stop := func() { close(done); resp.Body.Close() }
	return out, stop, nil
}
