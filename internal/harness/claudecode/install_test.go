package claudecode_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// These tests drive the real claude command, always with HOME and
// CLAUDE_CONFIG_DIR set to a temporary home folder.

func needClaude(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is not installed")
	}
}

func claudeIn(t *testing.T, home string, args ...string) {
	t.Helper()
	cmd := exec.Command("claude", args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"))
	cmd.Dir = home
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("claude %v: %v\n%s", args, err, out)
	}
}

// seededHome is a home folder with a realistic Claude Code configuration:
// another marketplace and plugin installed, and a hand-formatted
// settings.json with hooks and permissions.
func seededHome(t *testing.T) string {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	other := filepath.Join(t.TempDir(), "other")
	for name, body := range map[string]string{
		".claude-plugin/marketplace.json": `{"name": "other", "owner": {"name": "Someone"}, "plugins": [{"name": "tool", "source": "./tool"}]}`,
		"tool/.claude-plugin/plugin.json": `{"name": "tool", "version": "1.2.0"}`,
		"tool/skills/tool/SKILL.md":       "---\nname: tool\ndescription: A tool.\n---\nUse it.\n",
	} {
		p := filepath.Join(other, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	claudeIn(t, home, "plugin", "marketplace", "add", other)
	claudeIn(t, home, "plugin", "install", "tool@other")
	settings := fmt.Sprintf(`{
    "model": "opus",
  "permissions": {"allow": ["Bash(ls)", "Read"]},
  "hooks": {"Notification": [{"hooks": [{"type": "command", "command": "say done"}]}]},
  "extraKnownMarketplaces": {"other": {"source": {"source": "directory", "path": %q}}},
  "enabledPlugins": {"tool@other": true}
}
`, other)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(settings), 0o600)
	return home
}

// tree lists every file under home with a hash of its bytes, leaving out
// Claude Code's own state, which every claude command rewrites (.claude.json,
// its backups), and Pong's own data folder under Library.
func tree(t *testing.T, home string) string {
	t.Helper()
	var b strings.Builder
	filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(home, p)
		if rel == "Library" || rel == ".claude/backups" || rel == ".claude/.claude.json" || rel == ".claude.json" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, _ := d.Info()
		if d.IsDir() {
			fmt.Fprintf(&b, "%s/\n", rel)
			return nil
		}
		c, _ := os.ReadFile(p)
		fmt.Fprintf(&b, "%s %o %x\n", rel, fi.Mode().Perm(), sha256.Sum256(c))
		return nil
	})
	return b.String()
}

func TestInstallTwiceThenRemoveLeavesConfigurationAsFound(t *testing.T) {
	needClaude(t)
	home := seededHome(t)
	before := tree(t, home)
	a := claudecode.Adapter{Engine: t.TempDir()}
	pong := "/Applications/Pong Tools/pong"

	if err := a.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, ".claude/plugins/cache/pong/pong")
	versions, _ := os.ReadDir(cache)
	if len(versions) != 1 {
		t.Fatalf("plugin cache: %v", versions)
	}
	dir := filepath.Join(cache, versions[0].Name())
	hooks, _ := os.ReadFile(filepath.Join(dir, "hooks/hooks.json"))
	mcp, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	skill, _ := os.ReadFile(filepath.Join(dir, "skills/pong/SKILL.md"))
	if !bytes.Contains(hooks, []byte(`"command": "'/Applications/Pong Tools/pong' hook claude-code", "timeout": 604800`)) ||
		!bytes.Contains(mcp, []byte(`"command": "/Applications/Pong Tools/pong"`)) {
		t.Fatalf("hooks.json:\n%s\n.mcp.json:\n%s", hooks, mcp)
	}
	sk, _ := integrations.ClaudeCode.ReadFile("skill/pong/SKILL.md")
	if !bytes.Equal(skill, sk) {
		t.Fatal("the skill was not bundled")
	}
	if s := a.Status(home); !s.Connected || !strings.Contains(s.Note, "installed") {
		t.Fatalf("status after install: %+v", s)
	}

	once := tree(t, home)
	if err := a.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	if twice := tree(t, home); twice != once {
		t.Fatalf("second install changed files:\n%s\nwas\n%s", twice, once)
	}

	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if after := tree(t, home); after != before {
		t.Fatalf("after remove:\n%s\nbefore install:\n%s", after, before)
	}
	if s := a.Status(home); s.Connected {
		t.Fatalf("status after remove: %+v", s)
	}
}

func TestInstallIntoEmptyClaudeFolderAndRemove(t *testing.T) {
	needClaude(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	before := tree(t, home)
	a := claudecode.Adapter{Engine: t.TempDir()}
	if err := a.Install(home, "/bin/pong"); err != nil {
		t.Fatal(err)
	}
	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if after := tree(t, home); after != before {
		t.Fatalf("after remove:\n%s\nbefore:\n%s", after, before)
	}
}

func TestStatusSaysWhenNoSessionHasConnected(t *testing.T) {
	needClaude(t)
	_, dir := enginetest.Start(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	a := claudecode.Adapter{Engine: dir}
	if err := a.Install(home, "/bin/pong"); err != nil {
		t.Fatal(err)
	}
	s := a.Status(home)
	if !strings.Contains(s.Note, "/reload-plugins") || !strings.Contains(s.Note, "version ") {
		t.Fatalf("before any Session: %+v", s)
	}
	if v := a.Version(home); !slices.Contains(claudecode.TestedVersions, v) && !strings.Contains(s.Note, "warning") {
		t.Fatalf("version %s outside the tested ones gave no warning: %+v", v, s)
	}
	hook(t, dir, recorded(t, "plan-mode", "SessionStart"))
	if s := a.Status(home); !strings.Contains(s.Note, "installed and connected") || strings.Contains(s.Note, "/reload-plugins") {
		t.Fatalf("after a Session started: %+v", s)
	}
}

func TestNotDetectedWithoutClaudeFolder(t *testing.T) {
	a := claudecode.Adapter{}
	home := t.TempDir()
	if a.Detect(home) || a.Install(home, "/bin/pong") == nil {
		t.Fatal("installed with no ~/.claude")
	}
	if s := a.Status(home); s.Detected || s.Connected {
		t.Fatalf("%+v", s)
	}
}

// installedHome is a home folder that looks as Install leaves it, made by
// hand so no claude is needed: Pong's status line and plugin in the tracked
// files, the plugin cache, the marketplace and the record.
func installedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for name, body := range map[string]string{
		".claude/settings.json":                                                               `{"enabledPlugins": {"pong@pong": true}, "statusLine": {"type": "command", "command": "/bin/pong statusline claude-code"}}`,
		".claude/plugins/installed_plugins.json":                                              `{"version": 2, "plugins": {"pong@pong": [{"scope": "user"}]}}`,
		".claude/plugins/known_marketplaces.json":                                             `{"pong": {}}`,
		".claude/plugins/cache/pong/pong/1.0.0/.mcp.json":                                     `{}`,
		"Library/Application Support/Pong/claude-code-plugin/.claude-plugin/marketplace.json": `{"name": "pong"}`,
		"Library/Application Support/Pong/harnesses/claude-code.json": `{"installed_at": "2026-11-01T00:00:00Z",
			"files": {".claude/settings.json": {"content": "{\"statusLine\": {\"type\": \"command\", \"command\": \"mine\"}}"}},
			"status_line": {"type": "command", "command": "mine"}}`,
	} {
		p := filepath.Join(home, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	return home
}

// all is tree with Pong's own data folder (the record and the marketplace) included.
func all(t *testing.T, home string) string {
	t.Helper()
	return tree(t, home) + tree(t, filepath.Join(home, "Library"))
}

// Without a claude command, Install changes nothing.
func TestWithoutClaudeInstallChangesNothing(t *testing.T) {
	a := claudecode.Adapter{Engine: t.TempDir(), Env: []string{"PATH=" + t.TempDir()}}
	home := installedHome(t)
	before := all(t, home)
	if a.CanInstall() == nil {
		t.Fatal("CanInstall found a claude")
	}
	if err := a.Install(home, "/bin/pong"); err == nil {
		t.Fatal("Install succeeded with no claude")
	}
	if after := all(t, home); after != before {
		t.Fatalf("changed with no claude:\n%s\nwas\n%s", after, before)
	}

	fresh := t.TempDir()
	os.MkdirAll(filepath.Join(fresh, ".claude"), 0o755)
	if err := a.Install(fresh, "/bin/pong"); err == nil {
		t.Fatal("Install succeeded with no claude")
	}
	if _, err := os.Stat(filepath.Join(fresh, "Library")); err == nil {
		t.Fatal("Install with no claude left a record or a marketplace")
	}
}

// When claude is gone from the machine, Remove still takes Pong out: its
// status line, its entries in Claude Code's plugin files, the plugin cache,
// the marketplace and the record. What else those files hold stays.
func TestRemoveWithoutClaudeTakesPongOut(t *testing.T) {
	a := claudecode.Adapter{Engine: t.TempDir(), Env: []string{"PATH=" + t.TempDir()}}
	home := installedHome(t)
	// Another plugin, installed after Pong, so the file cannot go back as found.
	installed := filepath.Join(home, ".claude/plugins/installed_plugins.json")
	os.WriteFile(installed, []byte(`{"version": 2, "plugins": {"pong@pong": [{"scope": "user"}], "tool@other": [{"scope": "user"}]}}`), 0o644)
	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(home, name))
		return string(b)
	}
	if got := read(".claude/settings.json"); got != `{"statusLine": {"type": "command", "command": "mine"}}` {
		t.Fatalf("settings.json was not put back as found: %s", got)
	}
	if got := read(".claude/plugins/installed_plugins.json"); strings.Contains(got, "pong") || !strings.Contains(got, "tool@other") {
		t.Fatalf("installed_plugins.json: %s", got)
	}
	if got := read(".claude/plugins/known_marketplaces.json"); strings.Contains(got, "pong") {
		t.Fatalf("known_marketplaces.json: %s", got)
	}
	for _, p := range []string{".claude/plugins/cache/pong", "Library/Application Support/Pong/claude-code-plugin", "Library/Application Support/Pong/harnesses/claude-code.json"} {
		if _, err := os.Stat(filepath.Join(home, p)); err == nil {
			t.Fatalf("Remove left %s", p)
		}
	}
}

// The captured shell environment is where claude is found, not this process's PATH.
func TestRemoveChangesNothingWhenClaudeCannotSayWhatIsInstalled(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	a := claudecode.Adapter{Engine: t.TempDir(), Env: []string{"PATH=" + bin}} // the fake fails plugin list
	home := installedHome(t)
	before := all(t, home)
	err := a.Remove(home)
	if err == nil || !strings.Contains(err.Error(), "fakeclaude") {
		t.Fatalf("Remove did not run the claude of the given environment: %v", err)
	}
	if after := all(t, home); after != before {
		t.Fatalf("changed:\n%s\nwas\n%s", after, before)
	}
}

func TestFailedInstallLeavesNoRecord(t *testing.T) {
	// The fake lists no plugins and fails `plugin marketplace add`.
	a := claudecode.Adapter{Engine: t.TempDir(), Env: []string{"PATH=" + bin, "FAKE_CLAUDE_PLUGINS=[]"}}
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"statusLine": {"type": "command", "command": "mine"}}`), 0o600)
	before := tree(t, home)
	if err := a.Install(home, "/bin/pong"); err == nil {
		t.Fatal("Install succeeded though claude failed")
	}
	if after := tree(t, home); after != before {
		t.Fatalf("changed:\n%s\nwas\n%s", after, before)
	}
	pong := filepath.Join(home, "Library", "Application Support", "Pong")
	for _, p := range []string{"harnesses/claude-code.json", "claude-code-plugin"} {
		if _, err := os.Stat(filepath.Join(pong, p)); err == nil {
			t.Fatalf("the failed Install left %s", p)
		}
	}
}

// A Pong whose plugin changed brings Claude Code's installed plugin up to
// it: installing again updates the cached plugin, under a newer version.
func TestInstallAgainUpdatesAChangedPlugin(t *testing.T) {
	needClaude(t)
	home := seededHome(t)
	before := tree(t, home)
	a := claudecode.Adapter{Engine: t.TempDir()}
	if err := a.Install(home, "/old/pong"); err != nil {
		t.Fatal(err)
	}
	if err := a.Install(home, "/new/pong"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude/plugins/installed_plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ip struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
		} `json:"plugins"`
	}
	json.Unmarshal(b, &ip)
	cur := ip.Plugins[claudecode.Plugin]
	if len(cur) != 1 || cur[0].Version == "0.1.0" {
		t.Fatalf("installed: %+v; want one install at a newer version", cur)
	}
	hooks, _ := os.ReadFile(filepath.Join(cur[0].InstallPath, "hooks/hooks.json"))
	if !bytes.Contains(hooks, []byte(`"command": "/new/pong hook claude-code"`)) {
		t.Fatalf("the installed plugin still runs the old pong:\n%s", hooks)
	}
	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if after := tree(t, home); after != before {
		t.Fatalf("after remove:\n%s\nbefore install:\n%s", after, before)
	}
}
