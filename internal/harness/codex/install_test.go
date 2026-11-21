package codex_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/internal/harness/codex"
)

const pongPath = "/Applications/Pong.app/Contents/MacOS/pong"

func codexHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	for name, content := range files {
		os.WriteFile(filepath.Join(home, ".codex", name), []byte(content), 0o600)
	}
	return home
}

func read(t *testing.T, home, name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(home, ".codex", name))
	return string(b), err == nil
}

// The human's config.toml, comments and odd spacing included, and AGENTS.md
// come back byte for byte; files Pong created are deleted.
func TestInstallThenRemoveLeavesConfigurationAsFound(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"existing files": {
			"config.toml": "# my settings\nmodel = \"o3\"   # keep this\n\n[features]\nhooks = true\n\n[mcp_servers.fs]\ncommand = \"fs\"\n\n[[hooks.Stop]]\n[[hooks.Stop.hooks]]\ntype = \"command\"\ncommand = \"say done\"\n",
			"AGENTS.md":   "Be terse.\n",
		},
		"no trailing newline": {"config.toml": "model = \"o3\"", "AGENTS.md": "Be terse."},
		"no files":            {},
	} {
		t.Run(name, func(t *testing.T) {
			home := codexHome(t, files)
			if err := (codex.Adapter{}).Install(home, pongPath); err != nil {
				t.Fatal(err)
			}
			cfg, _ := read(t, home, "config.toml")
			ins, _ := read(t, home, "AGENTS.md")
			if !strings.HasPrefix(cfg, files["config.toml"]) || !strings.Contains(cfg, "[mcp_servers.pong]") ||
				!strings.Contains(cfg, "tool_timeout_sec = 300") || !strings.Contains(cfg, "[[hooks.PermissionRequest]]") ||
				// Pong's own tools run without Codex asking first: asking the human is what they are for.
				!strings.Contains(cfg, "default_tools_approval_mode = \"auto\"") ||
				!strings.Contains(cfg, `command = "`+pongPath+` hook codex"`) || !strings.Contains(ins, "Pong") {
				t.Fatalf("config.toml:\n%s\nAGENTS.md:\n%s", cfg, ins)
			}
			if err := (codex.Adapter{}).Install(home, pongPath); err != nil {
				t.Fatal(err)
			}
			if again, _ := read(t, home, "config.toml"); again != cfg {
				t.Fatal("installing again changed config.toml")
			}
			if err := (codex.Adapter{}).Remove(home); err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"config.toml", "AGENTS.md"} {
				got, exists := read(t, home, f)
				want, had := files[f]
				if exists != had || got != want {
					t.Errorf("%s after remove = %q (exists %v); want %q (exists %v)", f, got, exists, want, had)
				}
			}
		})
	}
}

// Settings the human adds after install survive removal.
func TestRemoveKeepsLaterEdits(t *testing.T) {
	home := codexHome(t, map[string]string{"config.toml": "model = \"o3\"\n"})
	codex.Adapter{}.Install(home, pongPath)
	cfg, _ := read(t, home, "config.toml")
	os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(cfg+"\n[mcp_servers.git]\ncommand = \"git-mcp\"\n"), 0o600)
	codex.Adapter{}.Remove(home)
	if got, _ := read(t, home, "config.toml"); got != "model = \"o3\"\n\n[mcp_servers.git]\ncommand = \"git-mcp\"\n" {
		t.Fatalf("config.toml = %q", got)
	}
}

func TestInstallRefusesAnExistingPongServer(t *testing.T) {
	cfg := "[mcp_servers.pong]\ncommand = \"other\"\n"
	home := codexHome(t, map[string]string{"config.toml": cfg})
	if err := (codex.Adapter{}).Install(home, pongPath); err == nil {
		t.Fatal("installed over an existing pong server")
	}
	if got, _ := read(t, home, "config.toml"); got != cfg {
		t.Fatalf("config.toml changed: %q", got)
	}
}

func TestNotInstalled(t *testing.T) {
	home := t.TempDir()
	a := codex.Adapter{}
	if a.Detect(home) || a.Status(home).Detected || a.Status(home).Note != "not installed" {
		t.Fatalf("Status = %+v", a.Status(home))
	}
	if err := a.Install(home, pongPath); err == nil {
		t.Fatal("installed with no ~/.codex")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		t.Fatal("Install created ~/.codex")
	}
}

// The real codex, when on the PATH, reads what Install wrote: the MCP entry
// with its raised tool time limit, and the three hooks, untrusted until the
// human trusts them in /hooks; Status says so. Runs offline, no sign-in.
func TestRealCodexReadsTheInstalledConfiguration(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex is not on the PATH")
	}
	home := codexHome(t, map[string]string{"config.toml": "# mine\nmodel = \"o3\"\n"})
	if err := (codex.Adapter{}).Install(home, pongPath); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("codex", "mcp", "get", "pong", "--json")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+filepath.Join(home, ".codex"))
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("codex mcp get: %v", err)
	}
	var m struct {
		Transport struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"transport"`
		ToolTimeoutSec float64 `json:"tool_timeout_sec"`
	}
	if json.Unmarshal(b, &m) != nil || m.Transport.Command != pongPath || len(m.Transport.Args) != 1 || m.ToolTimeoutSec != 300 {
		t.Fatalf("codex mcp get pong: %s", b)
	}
	if trusted, err := codex.HooksTrusted(home); err != nil || trusted {
		t.Fatalf("HooksTrusted = %v, %v; want false (newly added hooks are untrusted)", trusted, err)
	}
	if s := (codex.Adapter{}).Status(home); !s.Connected || !strings.Contains(s.Note, "/hooks") || !strings.Contains(s.Note, "no Codex version has been tested") {
		t.Fatalf("Status = %+v", s)
	}
}
