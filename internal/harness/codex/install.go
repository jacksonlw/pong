package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/harness"
)

// Adapter installs Pong in Codex by editing ~/.codex/config.toml as text: it
// appends one block holding the MCP server entry and the hooks, and appends the standing instruction to ~/.codex/AGENTS.md.
// Removal takes out exactly the bytes added, so the human's formatting and
// comments are untouched. Formats from
// https://learn.chatgpt.com/docs/config-file/config-reference and
// https://learn.chatgpt.com/docs/hooks (2026-11-18).
type Adapter struct{}

var _ harness.Adapter = Adapter{}

const (
	configFile      = ".codex/config.toml"
	instructionFile = ".codex/AGENTS.md"
	// ToolTimeoutSec is the raised MCP tool time limit (Codex's default is 60).
	ToolTimeoutSec = 300
	// WaitTimeout is the hook timeout, in seconds, for hooks that wait on a
	// Ping: a week, far above Codex's 600-second default.
	WaitTimeout = 7 * 24 * 60 * 60
)

// trustNote says what the human must do once after install, inside a status note.
const trustNote = "in Codex, run /hooks and trust Pong's hooks"

// TrustStep is what the human must do once after install.
const TrustStep = "Codex runs Pong's hooks only once you trust them. In Codex, run /hooks and trust them."

func (Adapter) Name() string { return "Codex" }

// Detect reports whether Codex's configuration folder exists.
func (Adapter) Detect(home string) bool {
	fi, err := os.Stat(filepath.Join(home, ".codex"))
	return err == nil && fi.IsDir()
}

func recordPath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Pong", "harnesses", "codex.json")
}

// record is what Install added, so Remove takes out exactly those bytes.
type record struct {
	InstalledAt        time.Time `json:"installed_at"`
	Config             []string  `json:"config"` // texts added to config.toml
	ConfigCreated      bool      `json:"config_created"`
	Instruction        string    `json:"instruction"`
	InstructionCreated bool      `json:"instruction_created"`
}

func load(home string) (record, bool) {
	var r record
	b, err := os.ReadFile(recordPath(home))
	return r, err == nil && json.Unmarshal(b, &r) == nil
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Block is the TOML Install appends to config.toml for the pong command.
func Block(pong string) string {
	q := strconv.Quote // a TOML basic string accepts Go's quoting of plain paths
	hook := q(shellQuote(pong) + " hook codex")
	var b strings.Builder
	b.WriteString("\n# Pong: begin\n[mcp_servers.pong]\n")
	fmt.Fprintf(&b, "command = %s\nargs = [\"mcp\"]\ntool_timeout_sec = %d\n", q(pong), ToolTimeoutSec)
	// Pong's tools exist to ask the human, so Codex runs them without asking
	// the human first whether it may ask (checked against codex-cli 0.171.0).
	b.WriteString("default_tools_approval_mode = \"auto\"\n")
	for _, ev := range []struct {
		name    string
		timeout int
	}{{"PermissionRequest", WaitTimeout}, {"Stop", WaitTimeout}, {"UserPromptSubmit", 30}} {
		fmt.Fprintf(&b, "\n[[hooks.%s]]\n[[hooks.%s.hooks]]\ntype = \"command\"\ncommand = %s\ntimeout = %d\n", ev.name, ev.name, hook, ev.timeout)
	}
	return b.String()
}

// endMarker closed the block once. Codex appends the tables it writes itself
// (hooks.state, tui, projects) after the file's last table and before a
// trailing comment, so they landed inside the markers and Pong no longer
// found its own bytes. The block now ends with its last hook.
const endMarker = "# Pong: end\n"

// cut takes the last occurrence of an added text out of s. A text that ended
// with the old end marker is also found with Codex's tables between its hooks
// and the marker: the hooks part and the marker line come out, the tables stay.
func cut(s, added string) (string, bool) {
	if i := strings.LastIndex(s, added); i >= 0 {
		return s[:i] + s[i+len(added):], true
	}
	core, ok := strings.CutSuffix(added, endMarker)
	if !ok {
		return s, false
	}
	i := strings.LastIndex(s, core)
	if i < 0 {
		return s, false
	}
	rest := s[i+len(core):]
	if j := strings.Index(rest, "\n"+endMarker); j >= 0 {
		rest = rest[:j+1] + rest[j+1+len(endMarker):]
	} else if strings.HasPrefix(rest, endMarker) {
		rest = rest[len(endMarker):]
	}
	return s[:i] + rest, true
}

var pongServer = regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*mcp_servers[ \t]*\.[ \t]*"?pong"?[ \t]*\]`)

// addToConfig returns config.toml with Pong's block appended, and the text
// added. Hooks need no feature flag: codex-cli 0.170.0 lists the hooks
// feature as stable and on.
func addToConfig(cfg, pong string) (string, []string, error) {
	// A block Pong wrote before, whose record is gone, is Pong's to replace.
	if c, ok := cut(cfg, Block(pong)+endMarker); ok {
		cfg = c
	}
	if pongServer.MatchString(cfg) {
		return "", nil, errors.New(`already has an MCP server named "pong"`)
	}
	block := Block(pong)
	if cfg != "" && !strings.HasSuffix(cfg, "\n") {
		block = "\n" + block
	}
	return cfg + block, []string{block}, nil
}

// Install adds Pong's MCP server, hooks and standing instruction. Installing
// again changes nothing.
func (a Adapter) Install(home, pong string) error {
	if !a.Detect(home) {
		return errors.New("Codex is not installed (no ~/.codex)")
	}
	if r, ok := load(home); ok {
		// Installed already: bring its instruction up to this Pong's.
		done, err := harness.RefreshInstruction(filepath.Join(home, instructionFile), r.Instruction, instructionBlock())
		if err != nil || !done {
			return err
		}
		r.Instruction = instructionBlock()
		rb, _ := json.MarshalIndent(r, "", "  ")
		return os.WriteFile(recordPath(home), rb, 0o644)
	}
	cfgPath := filepath.Join(home, configFile)
	cfg, err := os.ReadFile(cfgPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	r := record{InstalledAt: time.Now().UTC(), ConfigCreated: err != nil, Instruction: instructionBlock()}
	newCfg, added, err := addToConfig(string(cfg), pong)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	r.Config = added
	insPath := filepath.Join(home, instructionFile)
	ins, err := os.ReadFile(insPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	r.InstructionCreated = err != nil

	rb, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(filepath.Dir(recordPath(home)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(recordPath(home), rb, 0o644); err != nil {
		return err
	}
	if err := writeKeepingMode(cfgPath, []byte(newCfg)); err != nil {
		return err
	}
	return writeKeepingMode(insPath, append(ins, r.Instruction...))
}

func instructionBlock() string {
	return "\n<!-- Pong: begin -->\n" + integrations.StandingInstruction + "<!-- Pong: end -->\n"
}

// Remove takes out what Install added, leaving the files as they were found.
func (a Adapter) Remove(home string) error {
	r, ok := load(home)
	if !ok {
		return nil
	}
	if err := takeOut(filepath.Join(home, configFile), r.Config, r.ConfigCreated); err != nil {
		return err
	}
	if err := takeOut(filepath.Join(home, instructionFile), []string{r.Instruction}, r.InstructionCreated); err != nil {
		return err
	}
	return os.Remove(recordPath(home))
}

// takeOut deletes each added text from the file (the last occurrence, as
// Install appended). A file Pong created is deleted when nothing else is left.
func takeOut(path string, added []string, created bool) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	s := string(b)
	for _, a := range added {
		s, _ = cut(s, a)
	}
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

// Version is the installed Codex's version, or "" when it cannot be run.
func Version() string {
	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// Status reports whether Pong is installed in Codex, whether its hooks are
// trusted, and Codex's version. Trust is read from Codex itself: its app
// server's hooks/list gives each hook's trustStatus (managed, untrusted,
// trusted or modified), checked against codex-cli 0.170.0.
func (a Adapter) Status(home string) harness.Status {
	s := harness.Status{Detected: a.Detect(home)}
	if !s.Detected {
		s.Note = "not installed"
		return s
	}
	var notes []string
	r, ok := load(home)
	if ok {
		c, _ := os.ReadFile(filepath.Join(home, configFile))
		i, _ := os.ReadFile(filepath.Join(home, instructionFile))
		s.Connected = strings.Contains(string(i), r.Instruction)
		for _, t := range r.Config {
			_, found := cut(string(c), t)
			s.Connected = s.Connected && found
		}
	}
	switch {
	case s.Connected:
		switch trusted, err := HooksTrusted(home); {
		case err != nil:
			notes = append(notes, "installed; cannot tell whether its hooks are trusted ("+err.Error()+"); "+trustNote)
			s.Warning = TrustStep
		case trusted:
			notes = append(notes, "installed; hooks trusted")
		default:
			notes = append(notes, "installed; hooks not trusted yet: "+trustNote)
			s.Warning = TrustStep
		}
	case ok:
		notes = append(notes, "Pong's entries were changed by hand; remove and install again")
	default:
		notes = append(notes, "Pong is not installed in it")
	}
	v := Version()
	s.Version = v
	switch {
	case v == "":
		notes = append(notes, "the codex command could not be run")
	case len(TestedVersions) == 0:
		notes = append(notes, "version "+v+"; warning: no Codex version has been tested with Pong")
		s.Warning = join(s.Warning, "No Codex version has been tested with Pong.")
	case !slices.Contains(TestedVersions, v):
		notes = append(notes, fmt.Sprintf("version %s; warning: Pong was tested with %s only", v, strings.Join(TestedVersions, ", ")))
		s.Warning = join(s.Warning, "Pong was tested with Codex "+strings.Join(TestedVersions, ", ")+" only.")
	default:
		notes = append(notes, "version "+v)
	}
	s.Note = strings.Join(notes, "; ")
	return s
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// HooksTrusted asks Codex's app server, run against home's ~/.codex, whether
// every one of Pong's hooks is trusted. It needs no sign-in.
func HooksTrusted(home string) (bool, error) {
	hooks, err := listHooks(home)
	if err != nil {
		return false, err
	}
	n := 0
	for _, h := range hooks {
		if strings.HasSuffix(h.Command, " hook codex") {
			n++
			if h.TrustStatus != "trusted" && h.TrustStatus != "managed" {
				return false, nil
			}
		}
	}
	if n == 0 {
		return false, errors.New("Codex lists none of Pong's hooks")
	}
	return true, nil
}

type listedHook struct {
	EventName   string `json:"eventName"`
	Command     string `json:"command"`
	TimeoutSec  int    `json:"timeoutSec"`
	TrustStatus string `json:"trustStatus"`
}

// listHooks runs `codex app-server` with CODEX_HOME at home's .codex and asks
// hooks/list for the user's hooks.
func listHooks(home string) ([]listedHook, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CODEX_HOME=")
	}), "CODEX_HOME="+filepath.Join(home, ".codex"))
	cmd.Dir = home
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { in.Close(); cmd.Process.Kill(); cmd.Wait() }()
	cwd, _ := json.Marshal(home)
	fmt.Fprintf(in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"pong","version":"0"}}}`+"\n"+
		`{"jsonrpc":"2.0","method":"initialized"}`+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"hooks/list","params":{"cwds":[%s]}}`+"\n", cwd)
	sc := bufio.NewScanner(out)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var m struct {
			ID     int `json:"id"`
			Result *struct {
				Data []struct {
					Hooks []listedHook `json:"hooks"`
				} `json:"data"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID != 2 {
			continue
		}
		if m.Error != nil || m.Result == nil {
			return nil, errors.New("codex app-server hooks/list failed")
		}
		var hs []listedHook
		for _, d := range m.Result.Data {
			hs = append(hs, d.Hooks...)
		}
		return hs, nil
	}
	return nil, errors.New("codex app-server gave no hooks/list answer")
}
