// Package generic is the adapter for Harnesses without a full adapter: it adds
// Pong's MCP server and the standing instruction to the Harness's own files,
// and takes exactly those bytes out again on removal.
package generic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/harness"
)

// Harness is one Harness whose configuration locations Pong knows. Paths are
// relative to the home folder.
type Harness struct {
	ID          string // name used on the command line
	Title       string
	MCPConfig   string // JSON file holding the MCP servers
	ServersKey  string // top-level key of the servers object
	Instruction string // always-loaded instruction file
	TimeoutMS   int    // per-server tool time limit to set; 0 when the default is long enough or there is none
}

// Known is the first set of Harnesses, each checked against the vendor docs cited beside it.
var Known = []Harness{
	// https://geminicli.com/docs/tools/mcp-server/ (timeout defaults to 600,000 ms, enough)
	// https://geminicli.com/docs/cli/gemini-md/
	{ID: "gemini", Title: "Gemini CLI", MCPConfig: ".gemini/settings.json", ServersKey: "mcpServers",
		Instruction: ".gemini/GEMINI.md"},
	// https://ampcode.com/docs/customize/mcp (no tool time limit documented)
	// https://ampcode.com/docs/customize/agents-md
	{ID: "amp", Title: "Amp", MCPConfig: ".config/amp/settings.json", ServersKey: "amp.mcpServers",
		Instruction: ".config/amp/AGENTS.md"},
	// https://docs.factory.com/cli/configuration/mcp (timeout in ms, default not stated)
	// https://docs.factory.com/cli/configuration/agents-md
	{ID: "factory", Title: "Factory Droid", MCPConfig: ".factory/mcp.json", ServersKey: "mcpServers",
		Instruction: ".factory/AGENTS.md", TimeoutMS: 120000},
}

// Find returns the known Harness with this ID.
func Find(id string) (Harness, bool) {
	for _, h := range Known {
		if h.ID == id {
			return h, true
		}
	}
	return Harness{}, false
}

var _ harness.Adapter = Harness{}

// Limits is said of every Harness this adapter covers.
const Limits = "can send Pings; its own prompts are not captured and Sessions cannot be started in it"

func (h Harness) Name() string { return h.Title }

// Detect reports whether the Harness's configuration folder exists.
func (h Harness) Detect(home string) bool {
	fi, err := os.Stat(filepath.Join(home, filepath.Dir(h.MCPConfig)))
	return err == nil && fi.IsDir()
}

// Entry is the MCP server entry for pong, as JSON.
func Entry(pong string, timeoutMS int) string {
	b, _ := json.Marshal(pong)
	s := fmt.Sprintf(`{"command": %s, "args": ["mcp"]`, b)
	if timeoutMS > 0 {
		s += fmt.Sprintf(`, "timeout": %d`, timeoutMS)
	}
	return s + "}"
}

// InstructionBlock is what install appends to the instruction file.
func InstructionBlock() string {
	return "\n<!-- Pong: begin -->\n" + integrations.StandingInstruction + "<!-- Pong: end -->\n"
}

// record is what install added, kept under Pong's data folder so removal
// takes out exactly those bytes.
type record struct {
	Config             string `json:"config"`
	ConfigCreated      bool   `json:"config_created"`
	Instruction        string `json:"instruction"`
	InstructionCreated bool   `json:"instruction_created"`
}

func (h Harness) recordPath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Pong", "harnesses", h.ID+".json")
}

func (h Harness) load(home string) (record, bool) {
	var r record
	b, err := os.ReadFile(h.recordPath(home))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// Install adds Pong's MCP server and the standing instruction. Installing
// again changes nothing.
func (h Harness) Install(home, pong string) error {
	if !h.Detect(home) {
		return fmt.Errorf("%s is not installed (no %s)", h.Title, filepath.Dir(filepath.Join("~", h.MCPConfig)))
	}
	if _, ok := h.load(home); ok {
		return nil
	}
	cfgPath := filepath.Join(home, h.MCPConfig)
	cfg, cfgErr := os.ReadFile(cfgPath)
	if cfgErr != nil && !errors.Is(cfgErr, fs.ErrNotExist) {
		return cfgErr
	}
	r := record{ConfigCreated: cfgErr != nil}
	newCfg, added, err := addServer(cfg, h.ServersKey, Entry(pong, h.TimeoutMS))
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	r.Config = added

	insPath := filepath.Join(home, h.Instruction)
	ins, insErr := os.ReadFile(insPath)
	if insErr != nil && !errors.Is(insErr, fs.ErrNotExist) {
		return insErr
	}
	r.InstructionCreated = insErr != nil
	r.Instruction = InstructionBlock()

	rb, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(filepath.Dir(h.recordPath(home)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(h.recordPath(home), rb, 0o644); err != nil {
		return err
	}
	if err := writeKeepingMode(cfgPath, newCfg); err != nil {
		return err
	}
	return writeKeepingMode(insPath, append(ins, r.Instruction...))
}

// Remove takes out what Install added, leaving the files as they were found.
func (h Harness) Remove(home string) error {
	r, ok := h.load(home)
	if !ok {
		return nil
	}
	if err := takeOut(filepath.Join(home, h.MCPConfig), r.Config, r.ConfigCreated); err != nil {
		return err
	}
	if err := takeOut(filepath.Join(home, h.Instruction), r.Instruction, r.InstructionCreated); err != nil {
		return err
	}
	return os.Remove(h.recordPath(home))
}

// Status reports whether Pong is installed in the Harness.
func (h Harness) Status(home string) harness.Status {
	s := harness.Status{Detected: h.Detect(home)}
	r, ok := h.load(home)
	if ok {
		c, _ := os.ReadFile(filepath.Join(home, h.MCPConfig))
		i, _ := os.ReadFile(filepath.Join(home, h.Instruction))
		s.Connected = bytes.Contains(c, []byte(r.Config)) && bytes.Contains(i, []byte(r.Instruction))
	}
	switch {
	case !s.Detected:
		s.Note = "not installed"
	case s.Connected:
		s.Note = "connected: " + Limits
	case ok:
		s.Note = "Pong's entry or instruction was changed by hand; remove and install again"
	default:
		s.Note = "Pong is not installed in it"
	}
	return s
}

// takeOut deletes added from the file. A file Pong created is deleted when
// nothing else is left in it.
func takeOut(path, added string, created bool) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	s := string(b)
	i := strings.Index(s, added)
	if i < 0 {
		return nil // taken out by hand already
	}
	s = s[:i] + s[i+len(added):]
	if created && strings.TrimSpace(s) == "" {
		return os.Remove(path)
	}
	return writeKeepingMode(path, []byte(s))
}

func writeKeepingMode(path string, b []byte) error {
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, b, mode)
}

// addServer inserts a "pong" server into the servers object of a JSON file,
// touching nothing else, and returns the new bytes and the exact text added.
// An empty or missing file becomes a new object.
func addServer(cfg []byte, key, entry string) ([]byte, string, error) {
	if len(bytes.TrimSpace(cfg)) == 0 {
		added := fmt.Sprintf("{\n  %q: {\n    \"pong\": %s\n  }\n}\n", key, entry)
		return append(cfg, added...), added, nil
	}
	top, servers, serversEmpty, topEmpty, err := locate(cfg, key)
	if err != nil {
		return nil, "", err
	}
	var at int
	var added string
	if servers >= 0 {
		at = servers
		added = "\n    \"pong\": " + entry
		if !serversEmpty {
			added += ","
		}
	} else {
		at = top
		added = fmt.Sprintf("\n  %q: {\n    \"pong\": %s\n  }", key, entry)
		if !topEmpty {
			added += ","
		}
	}
	out := append(append(append([]byte{}, cfg[:at]...), added...), cfg[at:]...)
	return out, added, nil
}

// locate finds the offsets just after the top-level "{" and just after the
// servers object's "{" (-1 when absent), and whether each is empty. It
// refuses a file that already has a "pong" server.
func locate(cfg []byte, key string) (top, servers int, serversEmpty, topEmpty bool, err error) {
	d := json.NewDecoder(bytes.NewReader(cfg))
	if t, e := d.Token(); e != nil || t != json.Delim('{') {
		return 0, 0, false, false, errors.New("not a JSON object")
	}
	top, servers, topEmpty = int(d.InputOffset()), -1, !d.More()
	for d.More() {
		t, e := d.Token()
		if e != nil {
			return 0, 0, false, false, e
		}
		if t == key {
			if t, e := d.Token(); e != nil || t != json.Delim('{') {
				return 0, 0, false, false, fmt.Errorf("%q is not an object", key)
			}
			servers, serversEmpty = int(d.InputOffset()), !d.More()
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return 0, 0, false, false, e
				}
				if k == "pong" {
					return 0, 0, false, false, fmt.Errorf("already has a %q server", "pong")
				}
				var skip json.RawMessage
				if e := d.Decode(&skip); e != nil {
					return 0, 0, false, false, e
				}
			}
			if _, e := d.Token(); e != nil {
				return 0, 0, false, false, e
			}
			continue
		}
		var skip json.RawMessage
		if e := d.Decode(&skip); e != nil {
			return 0, 0, false, false, e
		}
	}
	if _, e := d.Token(); e != nil {
		return 0, 0, false, false, e
	}
	return top, servers, serversEmpty, topEmpty, nil
}
