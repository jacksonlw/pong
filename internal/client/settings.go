package client

import "github.com/jacksonlw/pong/internal/engine"

// Settings reads the settings the Engine keeps for its interfaces.
func (c *Client) Settings() (engine.Settings, error) {
	var out engine.Settings
	return out, c.do(c.HTTP, "GET", "/settings", nil, &out)
}

// ChangeSettings changes the given settings; absent ones stay.
func (c *Client) ChangeSettings(ch engine.ChangeSettings) (engine.Settings, error) {
	var out engine.Settings
	return out, c.do(c.HTTP, "PATCH", "/settings", ch, &out)
}

// VoiceKey returns the voice model's API key from the Engine's Keychain.
func (c *Client) VoiceKey() (string, error) {
	var out struct {
		Key string `json:"key"`
	}
	return out.Key, c.do(c.HTTP, "GET", "/voice/key", nil, &out)
}

// SetVoiceKey stores the voice model's API key in the Engine's Keychain.
func (c *Client) SetVoiceKey(key string) error {
	var out struct{}
	return c.do(c.HTTP, "PUT", "/voice/key", map[string]string{"key": key}, &out)
}

// ClearVoiceKey removes the voice model's API key.
func (c *Client) ClearVoiceKey() error {
	var out struct{}
	return c.do(c.HTTP, "DELETE", "/voice/key", nil, &out)
}
