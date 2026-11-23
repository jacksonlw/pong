package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// Plugin is the plugin's id in Claude Code: the pong plugin from the pong
// marketplace.
const Plugin = "pong@pong"

// Adapter installs Pong in Claude Code as a plugin at user scope.
//
// Install drives Claude Code's own `claude plugin` commands with HOME and
// CLAUDE_CONFIG_DIR pointed at the given home folder, rather than writing
// Claude Code's plugin files itself: those files (installed_plugins.json, the
// plugin cache) are Claude Code's internal state and not documented. The
// commands reorder settings.json and leave empty objects and an orphaned cache
// behind, so Install keeps a copy of each configuration file it lets them
// change, and Remove puts those bytes back when nothing else has changed in
// the file since.
type Adapter struct {
	// Engine is the Engine's data folder, used by Status to see whether Claude
	// Code Sessions have reached it.
	Engine string
	// Env is the human's shell environment, in which claude is found and run
	// (the Engine's captured one, as for Sessions); nil means this process's own.
	Env []string
}

var _ harness.Adapter = Adapter{}

// tracked are the configuration files, relative to the home folder, that the
// plugin commands change.
var tracked = []string{
	".claude/settings.json",
	".claude/plugins/known_marketplaces.json",
	".claude/plugins/installed_plugins.json",
}

// made are the folders the plugin commands may create, deepest first.
var made = []string{
	".claude/plugins/cache/pong",
	".claude/plugins/cache",
	".claude/plugins/marketplaces",
	".claude/plugins",
}

func (Adapter) Name() string { return "Claude Code" }

// Detect reports whether Claude Code's configuration folder exists.
func (Adapter) Detect(home string) bool {
	fi, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil && fi.IsDir()
}

func pongDir(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Pong")
}

// MarketplaceDir is where Install writes the marketplace Claude Code installs from.
func MarketplaceDir(home string) string { return filepath.Join(pongDir(home), "claude-code-plugin") }

func recordPath(home string) string {
	return filepath.Join(pongDir(home), "harnesses", "claude-code.json")
}

// record is what Install found, so Remove can leave things as they were.
type record struct {
	InstalledAt time.Time        `json:"installed_at"`
	Files       map[string]*file `json:"files"`   // tracked files as found; nil when absent
	Folders     []string         `json:"folders"` // folders in made that did not exist
	// StatusLine is the human's user-level statusLine setting as found; nil
	// when they had none.
	StatusLine json.RawMessage `json:"status_line,omitempty"`
}

type file struct {
	Content string `json:"content"`
}

func load(home string) (record, bool) {
	var r record
	b, err := os.ReadFile(recordPath(home))
	return r, err == nil && json.Unmarshal(b, &r) == nil
}

// WriteMarketplace writes Pong's Claude Code marketplace into dir, with the
// plugin's hooks and MCP server launching pong and the skill bundled. With
// pong "pong" it is the marketplace as published, relying on pong being on
// the PATH.
func WriteMarketplace(dir, pong string) error { return writeMarketplace(dir, pong, "") }

// writeMarketplace is WriteMarketplace with the plugin's version set to
// version, when it is not "".
func writeMarketplace(dir, pong, version string) error {
	quoted, _ := json.Marshal(pong)
	shell, _ := json.Marshal(shellQuote(pong))
	put := func(name string, b []byte) error {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0o644)
	}
	err := fs.WalkDir(integrations.ClaudeCode, "claude-code", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := integrations.ClaudeCode.ReadFile(p)
		if err != nil {
			return err
		}
		b = bytes.ReplaceAll(b, []byte(`"command": "pong hook`), []byte(`"command": `+strings.TrimSuffix(string(shell), `"`)+` hook`))
		b = bytes.ReplaceAll(b, []byte(`"command": "pong"`), []byte(`"command": `+string(quoted)))
		if version != "" && strings.HasSuffix(p, "plugin/.claude-plugin/plugin.json") {
			b = versionRE.ReplaceAll(b, []byte(`"version": "`+version+`"`))
		}
		return put(strings.TrimPrefix(p, "claude-code/"), b)
	})
	if err != nil {
		return err
	}
	skill, err := integrations.ClaudeCode.ReadFile("skill/pong/SKILL.md")
	if err != nil {
		return err
	}
	return put(path.Join("plugin", "skills", "pong", "SKILL.md"), skill)
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// errNoClaude is why nothing was changed: the claude command cannot be found.
var errNoClaude = errors.New("there is no claude command on your PATH")

func (a Adapter) env() []string {
	if a.Env == nil {
		return os.Environ()
	}
	return a.Env
}

// claude runs a `claude` command against the given home folder.
func (a Adapter) claude(home string, args ...string) ([]byte, error) {
	path, err := engine.LookPath(a.env(), "claude")
	if err != nil {
		return nil, errNoClaude
	}
	cmd := exec.Command(path, args...)
	cmd.Env = append(slices.DeleteFunc(slices.Clone(a.env()), func(kv string) bool {
		return strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=")
	}), "HOME="+home, "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"))
	cmd.Dir = home
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String() + "\n" + string(out))
		return out, fmt.Errorf("claude %s: %v: %s", strings.Join(args, " "), err, msg)
	}
	return out, nil
}

// installed reports whether Claude Code lists the plugin as enabled at user scope.
func (a Adapter) installed(home string) (bool, error) {
	out, err := a.claude(home, "plugin", "list", "--json")
	if err != nil {
		return false, err
	}
	var ps []struct {
		ID      string `json:"id"`
		Scope   string `json:"scope"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(out, &ps); err != nil {
		return false, fmt.Errorf("reading claude plugin list: %w", err)
	}
	for _, p := range ps {
		if p.ID == Plugin && p.Scope == "user" && p.Enabled {
			return true, nil
		}
	}
	return false, nil
}

// CanInstall says why Install could not start: claude cannot be found.
func (a Adapter) CanInstall() error {
	if _, err := engine.LookPath(a.env(), "claude"); err != nil {
		return errNoClaude
	}
	return nil
}

// Install adds the plugin at user scope. Installing again brings the
// installed plugin up to this Pong's: when its hooks, MCP server or skill
// differ, the marketplace is written again under a newer version and Claude
// Code updates the plugin (Sessions take it when they restart). Otherwise it
// changes nothing.
// It changes nothing either when claude cannot be found, and an Install that
// fails part way takes out what it put in, so no record of a half-made
// install is left for the next Install or Remove to trust.
func (a Adapter) Install(home, pong string) error {
	if !a.Detect(home) {
		return errors.New("Claude Code is not installed (no ~/.claude)")
	}
	if err := a.CanInstall(); err != nil {
		return err
	}
	r, had := load(home)
	if had {
		yes, err := a.installed(home)
		if err != nil {
			return err
		}
		if yes {
			return a.update(home, pong)
		}
		// The plugin went away outside Pong: install it again, keeping what
		// the first Install found, since the files now hold Pong's changes.
		r.InstalledAt = time.Now().UTC()
	} else {
		var err error
		if r, err = found(home); err != nil {
			return err
		}
	}
	mkt := MarketplaceDir(home)
	if err := os.RemoveAll(mkt); err != nil {
		return err
	}
	if err := WriteMarketplace(mkt, pong); err != nil {
		return err
	}
	rb, _ := json.MarshalIndent(r, "", "  ")
	if err := os.MkdirAll(filepath.Dir(recordPath(home)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(recordPath(home), rb, 0o644); err != nil {
		return err
	}
	err := func() error {
		if _, err := a.claude(home, "plugin", "marketplace", "add", mkt, "--scope", "user", "--json"); err != nil {
			return err
		}
		if _, err := a.claude(home, "plugin", "install", Plugin, "--scope", "user", "--json"); err != nil {
			return err
		}
		return setStatusLine(home, pong, r.StatusLine)
	}()
	if err != nil {
		// If Remove cannot run either, the record stays: it holds what was
		// found before Pong changed anything, so a later Remove can use it.
		a.Remove(home)
	}
	return err
}

// update brings an installed plugin up to this Pong's, as Install says.
func (a Adapter) update(home, pong string) error {
	mkt := MarketplaceDir(home)
	fresh, err := os.MkdirTemp("", "pong-plugin")
	if err != nil {
		return err
	}
	defer os.RemoveAll(fresh)
	if err := WriteMarketplace(fresh, pong); err != nil {
		return err
	}
	if same, _ := sameMarketplace(fresh, mkt); same {
		return nil
	}
	// Written beside the old one and swapped in, so a failure leaves a
	// marketplace in place.
	next := mkt + ".next"
	os.RemoveAll(next)
	if err := writeMarketplace(next, pong, newerVersion()); err != nil {
		os.RemoveAll(next)
		return err
	}
	if err := os.RemoveAll(mkt); err != nil {
		return err
	}
	if err := os.Rename(next, mkt); err != nil {
		return err
	}
	_, err = a.claude(home, "plugin", "marketplace", "update", "pong", "--json")
	if err == nil {
		_, err = a.claude(home, "plugin", "update", Plugin, "--json")
	}
	if err != nil {
		// Marked, so the next setup finds it differs and tries again.
		os.WriteFile(filepath.Join(mkt, updateFailed), []byte(err.Error()), 0o644)
	}
	return err
}

// updateFailed marks a marketplace Claude Code did not take up.
const updateFailed = ".pong-update-failed"

// newerVersion is the plugin's own major and minor version with a patch
// that only grows, so Claude Code takes each update as newer.
func newerVersion() string {
	b, _ := integrations.ClaudeCode.ReadFile("claude-code/plugin/.claude-plugin/plugin.json")
	var v struct {
		Version string `json:"version"`
	}
	json.Unmarshal(b, &v)
	parts := strings.SplitN(v.Version, ".", 3)
	if len(parts) < 2 {
		parts = []string{"0", "1"}
	}
	return fmt.Sprintf("%s.%s.%d", parts[0], parts[1], now().Unix())
}

// now is the clock update versions the plugin by. Tests replace it.
var now = time.Now

// sameMarketplace reports whether two marketplaces hold the same files,
// whatever version the plugin says it is.
func sameMarketplace(a, b string) (bool, error) {
	read := func(root string) (map[string]string, error) {
		m := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			m[rel] = versionRE.ReplaceAllString(string(body), `"version": ""`)
			return nil
		})
		return m, err
	}
	x, err := read(a)
	if err != nil {
		return false, err
	}
	y, err := read(b)
	if err != nil {
		return false, err
	}
	return maps.Equal(x, y), nil
}

var versionRE = regexp.MustCompile(`"version": *"[^"]*"`)

// found records the human's configuration as it is before Pong changes it.
func found(home string) (record, error) {
	r := record{InstalledAt: time.Now().UTC(), Files: map[string]*file{}}
	sl, err := currentStatusLine(home)
	if err != nil {
		return r, err
	}
	r.StatusLine = sl
	for _, t := range tracked {
		b, err := os.ReadFile(filepath.Join(home, t))
		switch {
		case err == nil:
			r.Files[t] = &file{Content: string(b)}
		case errors.Is(err, fs.ErrNotExist):
			r.Files[t] = nil
		default:
			return r, err
		}
	}
	for _, d := range made {
		if _, err := os.Stat(filepath.Join(home, d)); errors.Is(err, fs.ErrNotExist) {
			r.Folders = append(r.Folders, d)
		}
	}
	return r, nil
}

// Remove uninstalls the plugin and its marketplace, then puts back each
// configuration file as Install found it, unless something else has changed
// in it since. When claude fails, so Remove cannot tell whether the plugin is
// installed, it changes nothing: putting files back around a plugin Claude
// Code still lists would leave Pong half removed. When claude is gone from
// the machine no command can take the plugin out, so Remove takes Pong's
// entries out of Claude Code's files itself.
func (a Adapter) Remove(home string) error {
	r, ok := load(home)
	if !ok {
		return nil
	}
	yes, err := a.installed(home)
	gone := errors.Is(err, errNoClaude)
	if err != nil && !gone {
		return err
	}
	if yes {
		if _, err := a.claude(home, "plugin", "uninstall", Plugin, "--scope", "user", "--json"); err != nil {
			return err
		}
	}
	if gone {
		for _, t := range tracked {
			if err := takeOutPong(home, t); err != nil {
				return err
			}
		}
	} else {
		// Ignore the error: the marketplace may have been removed by hand.
		a.claude(home, "plugin", "marketplace", "remove", "pong")
	}
	if err := restoreStatusLine(home, r.StatusLine); err != nil {
		return err
	}

	for _, t := range tracked {
		was, ok := r.Files[t]
		if !ok {
			continue
		}
		p := filepath.Join(home, t)
		now, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if was == nil {
			if emptyish(now) {
				os.Remove(p)
			}
			continue
		}
		if sameButEmpties([]byte(was.Content), now) {
			if err := os.WriteFile(p, []byte(was.Content), 0o644); err != nil {
				return err
			}
		}
	}
	os.RemoveAll(filepath.Join(home, ".claude/plugins/cache/pong"))
	for _, d := range made {
		if slices.Contains(r.Folders, d) {
			os.Remove(filepath.Join(home, d)) // only when empty
		}
	}
	if err := os.RemoveAll(MarketplaceDir(home)); err != nil {
		return err
	}
	return os.Remove(recordPath(home))
}

// pongEntries are where each tracked file names Pong's plugin or marketplace:
// the top-level object holding the entry ("" for the file's own) and its key.
var pongEntries = map[string][][2]string{
	".claude/settings.json":                   {{"enabledPlugins", Plugin}, {"extraKnownMarketplaces", "pong"}},
	".claude/plugins/known_marketplaces.json": {{"", "pong"}},
	".claude/plugins/installed_plugins.json":  {{"plugins", Plugin}},
}

// takeOutPong rewrites the tracked file t without Pong's entries, as the
// plugin commands would have. A file that is missing, is not JSON or does not
// name Pong is left alone.
func takeOutPong(home, t string) error {
	p := filepath.Join(home, t)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil {
		return nil
	}
	changed := false
	for _, e := range pongEntries[t] {
		in := top
		if e[0] != "" {
			in = nil
			if json.Unmarshal(top[e[0]], &in) != nil {
				continue
			}
		}
		if _, ok := in[e[1]]; !ok {
			continue
		}
		delete(in, e[1])
		changed = true
		if e[0] != "" {
			top[e[0]], _ = json.Marshal(in)
		}
	}
	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(out, '\n'), 0o644)
}

func decode(b []byte) (map[string]any, bool) {
	var v map[string]any
	return v, json.Unmarshal(b, &v) == nil
}

func isEmpty(v any) bool {
	switch v := v.(type) {
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
}

// emptyish reports whether a file the plugin commands created holds nothing
// but empty objects (and installed_plugins.json's format version).
func emptyish(b []byte) bool {
	v, ok := decode(b)
	if !ok {
		return false
	}
	for k, x := range v {
		if !isEmpty(x) && k != "version" {
			return false
		}
	}
	return true
}

// sameButEmpties reports whether now holds what was held, apart from empty
// objects the plugin commands added at the top level.
func sameButEmpties(was, now []byte) bool {
	w, ok1 := decode(was)
	n, ok2 := decode(now)
	if !ok1 || !ok2 {
		return false
	}
	for k, x := range n {
		if _, had := w[k]; !had && isEmpty(x) {
			delete(n, k)
		}
	}
	return reflect.DeepEqual(w, n)
}

// Version is the installed Claude Code's version, or "" when it cannot be run.
func (a Adapter) Version(home string) string {
	out, err := a.claude(home, "--version")
	if err != nil {
		return ""
	}
	v, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	return v
}

// Status reports whether the plugin is installed, whether a Claude Code
// Session has reached the Engine since it was installed (connected), and
// Claude Code's version, warning when it is not one the adapter was tested
// against. Until a Session has connected it says that Sessions already running
// need /reload-plugins: Sessions started before the install never announced
// themselves, so the Engine cannot list them.
func (a Adapter) Status(home string) harness.Status {
	s := harness.Status{Detected: a.Detect(home)}
	if !s.Detected {
		s.Note = "not installed"
		return s
	}
	var notes, warnings []string
	version := a.Version(home)
	s.Version = version
	switch {
	case version == "":
		notes = append(notes, "the claude command could not be run")
	case !slices.Contains(TestedVersions, version):
		notes = append(notes, fmt.Sprintf("version %s; warning: Pong was tested with %s only", version, strings.Join(TestedVersions, ", ")))
		warnings = append(warnings, fmt.Sprintf("Pong was tested with Claude Code %s only.", strings.Join(TestedVersions, ", ")))
	default:
		notes = append(notes, "version "+version)
	}
	r, recorded := load(home)
	yes, err := a.installed(home)
	switch {
	case err != nil:
		notes = append([]string{"cannot tell whether Pong's plugin is installed"}, notes...)
	case !yes && recorded:
		notes = append([]string{"Pong's plugin was removed or disabled outside Pong; remove and install again"}, notes...)
	case !yes:
		notes = append([]string{"Pong is not installed in it"}, notes...)
	default:
		s.Connected = true
		switch seen, err := sessionSince(a.Engine, r.InstalledAt); {
		case err != nil:
			notes = append([]string{"installed; the Engine is not running, so whether Sessions reach it is unknown"}, notes...)
		case seen:
			notes = append([]string{"installed and connected"}, notes...)
		default:
			notes = append([]string{"installed; no Session has connected yet: " + ReloadStep}, notes...)
			warnings = append(warnings, "Sessions already running need /reload-plugins, or start a new Session.")
		}
	}
	s.Note = strings.Join(notes, "; ")
	s.Warning = strings.Join(warnings, " ")
	return s
}

// ReloadStep is what the human does so Claude Code Sessions already running
// pick up Pong's plugin.
const ReloadStep = "run /reload-plugins in Claude Code Sessions already running, or start a new one"

// sessionSince reports whether the Engine knows a Claude Code Session
// registered at or after t.
func sessionSince(dir string, t time.Time) (bool, error) {
	c, err := client.Load(dir)
	if err != nil {
		return false, err
	}
	ss, err := c.Sessions()
	if err != nil {
		return false, err
	}
	for _, s := range ss {
		if s.Harness == HarnessName && !s.UpdatedAt.Before(t) {
			return true, nil
		}
	}
	return false, nil
}
