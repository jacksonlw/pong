// Package client talks to a running Engine over its local API.
package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/jacksonlw/pong/internal/engine"
)

// Client holds what is needed to reach an Engine.
type Client struct {
	Address string // host:port
	Secret  string
	HTTP    *http.Client
}

// Load reads the address file in dir.
func Load(dir string) (*Client, error) {
	b, err := os.ReadFile(filepath.Join(dir, engine.AddressFile))
	if err != nil {
		return nil, err
	}
	var a engine.Address
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("reading address file: %w", err)
	}
	return New(a.Address, a.Secret), nil
}

// New returns a client for the Engine at address.
func New(address, secret string) *Client {
	return &Client{Address: address, Secret: secret, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// Request builds an authorised request to path.
func (c *Client) Request(method, path string) (*http.Request, error) {
	req, err := http.NewRequest(method, "http://"+c.Address+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	return req, nil
}

// Health is the Engine's answer to GET /health.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Health asks the Engine for its health and version.
func (c *Client) Health() (Health, error) {
	var h Health
	req, err := c.Request("GET", "/health")
	if err != nil {
		return h, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return h, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h, fmt.Errorf("engine answered %s", resp.Status)
	}
	return h, json.NewDecoder(resp.Body).Decode(&h)
}

// Harnesses reports Pong's integration in each Harness.
func (c *Client) Harnesses() ([]engine.HarnessStatus, error) {
	var out []engine.HarnessStatus
	hc := *c.HTTP
	hc.Timeout = time.Minute // each Harness's status may run its command
	return out, c.do(&hc, "GET", "/harnesses", nil, &out)
}

// RepairHarness installs Pong's integration in the Harness again.
func (c *Client) RepairHarness(id string) (engine.HarnessStatus, error) {
	var out engine.HarnessStatus
	hc := *c.HTTP
	hc.Timeout = time.Minute
	return out, c.do(&hc, "POST", "/harnesses/"+url.PathEscape(id)+"/repair", nil, &out)
}
