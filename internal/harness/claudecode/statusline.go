package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// statusLineArgs follow the pong command in the status line command Install
// sets; a statusLine whose command ends so is Pong's own, never the human's.
const statusLineArgs = " statusline claude-code"

// StatusLineWait is the longest the status line waits for the Engine, counted
// from its start and overlapping the human's status line, so Pong never delays rendering noticeably.
var StatusLineWait = 150 * time.Millisecond

// StatusLine handles one status line run: it reads Claude Code's status line
// input, sends the figures it states for the Session to the Engine in dir,
// and runs the human's own status line command (the one Install found in
// home) with the same input, its output passed through unchanged. It returns
// that command's exit status, or 0 when the human had none and nothing is
// printed. The Engine call runs alongside and is given up once wait has passed
// since the start.
func StatusLine(dir, home string, stdin io.Reader, stdout, stderr io.Writer, wait time.Duration) int {
	in, _ := io.ReadAll(stdin)
	deadline := time.After(wait)
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		sendStatusFigures(dir, in, wait)
	}()
	code := 0
	if command := humanStatusLine(home); command != "" {
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Stdin = bytes.NewReader(in)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		var exit *exec.ExitError
		switch err := cmd.Run(); {
		case errors.As(err, &exit):
			code = exit.ExitCode()
		case err != nil:
			code = 1
		}
	}
	select {
	case <-sent:
	case <-deadline:
	}
	return code
}

// humanStatusLine is the command of the status line the human had when Pong
// was installed, or "" when they had none.
func humanStatusLine(home string) string {
	r, ok := load(home)
	if !ok || r.StatusLine == nil {
		return ""
	}
	var s struct {
		Command string `json:"command"`
	}
	json.Unmarshal(r.StatusLine, &s)
	return s.Command
}

type statusInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Model     *struct {
		ID string `json:"id"`
	} `json:"model"`
	Cost *struct {
		TotalCostUSD *float64 `json:"total_cost_usd"`
	} `json:"cost"`
	ContextWindow *struct {
		Size           *int64   `json:"context_window_size"`
		UsedPercentage *float64 `json:"used_percentage"`
		CurrentUsage   *struct {
			InputTokens              *int64 `json:"input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
}

// statusContext returns the figures the status line input states, or nil
// when it names no Session or states none.
func statusContext(in []byte) (string, string, *engine.Context) {
	var s statusInput
	if json.Unmarshal(in, &s) != nil || s.SessionID == "" {
		return "", "", nil
	}
	var c engine.Context
	if s.Model != nil {
		c.Model = s.Model.ID
	}
	if s.Cost != nil {
		c.CostUSD = s.Cost.TotalCostUSD
	}
	if w := s.ContextWindow; w != nil {
		c.Window = w.Size
		if p := w.UsedPercentage; p != nil && *p >= 0 && *p <= 100 {
			f := *p / 100
			c.UsedFraction = &f
		}
		if u := w.CurrentUsage; u != nil && u.InputTokens != nil && u.CacheCreationInputTokens != nil && u.CacheReadInputTokens != nil {
			n := *u.InputTokens + *u.CacheCreationInputTokens + *u.CacheReadInputTokens
			c.Tokens = &n
		}
	}
	if c == (engine.Context{}) {
		return "", "", nil
	}
	return s.SessionID, s.Cwd, &c
}

func sendStatusFigures(dir string, in []byte, timeout time.Duration) {
	id, cwd, ctx := statusContext(in)
	if ctx == nil {
		return
	}
	c, err := client.Load(dir)
	if err != nil {
		return
	}
	c.HTTP.Timeout = timeout
	c.RegisterSession(engine.RegisterSession{Harness: HarnessName, ID: id, Folder: cwd, Context: ctx})
}

// settingsPath is Claude Code's user settings file in home.
func settingsPath(home string) string { return filepath.Join(home, ".claude", "settings.json") }

// readSettings returns the user settings as raw values, empty when the file
// does not exist.
func readSettings(home string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(settingsPath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	return m, json.Unmarshal(b, &m)
}

func writeSettings(home string, m map[string]json.RawMessage) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(home), append(b, '\n'), 0o600)
}

func isPongStatusLine(raw json.RawMessage) bool {
	var s struct {
		Command string `json:"command"`
	}
	return json.Unmarshal(raw, &s) == nil && strings.HasSuffix(s.Command, statusLineArgs)
}

// currentStatusLine is the human's statusLine setting as found, or nil when
// there is none (Pong's own wrapper counts as none).
func currentStatusLine(home string) (json.RawMessage, error) {
	m, err := readSettings(home)
	if err != nil {
		return nil, err
	}
	raw, ok := m["statusLine"]
	if !ok || isPongStatusLine(raw) {
		return nil, nil
	}
	return raw, nil
}

// setStatusLine points the user-level statusLine at Pong's command, keeping
// the human's other statusLine keys (padding, refreshInterval).
func setStatusLine(home, pong string, was json.RawMessage) error {
	m, err := readSettings(home)
	if err != nil {
		return err
	}
	sl := map[string]any{}
	if was != nil {
		json.Unmarshal(was, &sl)
	}
	sl["type"] = "command"
	sl["command"] = shellQuote(pong) + statusLineArgs
	b, _ := json.Marshal(sl)
	m["statusLine"] = b
	return writeSettings(home, m)
}

// restoreStatusLine puts the human's statusLine back as found, unless it no
// longer is Pong's (someone changed it since).
func restoreStatusLine(home string, was json.RawMessage) error {
	m, err := readSettings(home)
	if err != nil {
		return err
	}
	now, ok := m["statusLine"]
	if !ok || !isPongStatusLine(now) {
		return nil
	}
	if was == nil {
		delete(m, "statusLine")
	} else {
		m["statusLine"] = was
	}
	return writeSettings(home, m)
}
