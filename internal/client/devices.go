package client

import (
	"net/http"

	"github.com/jacksonlw/pong/internal/devices"
)

// Devices returns the set as this Engine sees it.
func (c *Client) Devices() (devices.Set, error) {
	var s devices.Set
	return s, c.do(c.HTTP, http.MethodGet, "/devices", nil, &s)
}

// FoundDevices asks every computer found on the network who it is.
func (c *Client) FoundDevices() ([]devices.Found, error) {
	var f []devices.Found
	return f, c.do(c.HTTP, http.MethodGet, "/devices/found", nil, &f)
}

// Pair asks the computer at address to admit this one.
func (c *Client) Pair(address string) (devices.Pairing, error) {
	var p devices.Pairing
	return p, c.do(c.HTTP, http.MethodPost, "/devices/pair", map[string]string{"address": address}, &p)
}

// ConfirmPairing says, on this computer that asked to join, whether the
// computer asked shows the same code. It joins only after a match.
func (c *Client) ConfirmPairing(match bool) (devices.Set, error) {
	var s devices.Set
	return s, c.do(c.HTTP, http.MethodPost, "/devices/pair/confirm", map[string]bool{"match": match}, &s)
}

// ApproveDevice admits a Device that is asking to join.
func (c *Client) ApproveDevice(id string) (devices.Set, error) {
	var s devices.Set
	return s, c.do(c.HTTP, http.MethodPost, "/devices/"+id+"/approve", nil, &s)
}

// RevokeDevice takes a Device out of the set.
func (c *Client) RevokeDevice(id string) (devices.Set, error) {
	var s devices.Set
	return s, c.do(c.HTTP, http.MethodPost, "/devices/"+id+"/revoke", nil, &s)
}

// LeaveSet takes this computer out of its set through a Device that can be reached.
func (c *Client) LeaveSet() (int, error) {
	var out struct {
		Told int `json:"told"`
	}
	return out.Told, c.do(c.HTTP, http.MethodPost, "/devices/leave", nil, &out)
}
